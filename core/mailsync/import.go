package mailsync

// Local folders and .eml imports: mail that exists only in Godwit, with no
// folder of this name on the server. A local folder never syncs, is never
// deleted as stale (folders.go), and its messages carry no server location,
// which sync already treats as a message awaiting a move's claim.

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
	"github.com/godwit-email/godwit/core/mail"
	"github.com/godwit-email/godwit/core/store"
)

// Import statuses of a file.
const (
	Imported   = "imported"
	Duplicate  = "duplicate"
	ImportFail = "failed"
)

// ImportResult is one file's outcome.
type ImportResult struct {
	File   string `json:"file"`
	Status string `json:"status"` // imported, duplicate, failed
	UID    int64  `json:"uid,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// ImportReport sums a run.
type ImportReport struct {
	Imported   int            `json:"imported"`
	Duplicates int            `json:"duplicates"`
	Failed     int            `json:"failed"`
	Results    []ImportResult `json:"results"`
}

// EnsureLocalFolder returns the id of the account's local folder of name,
// creating it when missing. An error when a server folder carries the name:
// a later sync would claim the rows in it.
func EnsureLocalFolder(ctx context.Context, db *store.DB, accountID int64, name string) (int64, error) {
	rows, err := db.Query(ctx, "SELECT id, local FROM folders WHERE account_id = ? AND name = ?", accountID, name)
	if err != nil {
		return 0, err
	}
	if len(rows.Values) > 0 {
		if store.Int(rows.Values[0][1]) == 0 {
			return 0, fmt.Errorf("a server folder named %q exists; pick another folder name", name)
		}
		return store.Int(rows.Values[0][0]), nil
	}
	res, err := db.Transaction(ctx, []sqlite.Statement{{
		SQL:  "INSERT INTO folders(account_id, name, role, local, role_since) VALUES (?, ?, '', 1, unixepoch())",
		Args: []any{accountID, name},
	}})
	if err != nil {
		return 0, err
	}
	return res[0].LastInsertID, nil
}

// ImportEml inserts each path as a local message of folderID: parsed with
// mail.Parse, threaded by planThread like synced mail, labeled by the same
// fingerprint, and its attachments written into the account's attachment
// cache (their only copy — a local message has no server to fetch from).
// A file whose fingerprint the folder already holds counts as a duplicate.
func ImportEml(ctx context.Context, db *store.DB, cacheDir string, accountID, folderID int64, email string, seen bool, paths []string) (ImportReport, error) {
	acc := &account{id: accountID, email: email}
	var rep ImportReport
	for _, p := range paths {
		if ctx.Err() != nil {
			return rep, ctx.Err()
		}
		r := importOne(ctx, db, cacheDir, acc, folderID, seen, p)
		rep.Results = append(rep.Results, r)
		switch r.Status {
		case Imported:
			rep.Imported++
		case Duplicate:
			rep.Duplicates++
		default:
			rep.Failed++
		}
	}
	return rep, nil
}

func importOne(ctx context.Context, db *store.DB, cacheDir string, acc *account, folderID int64, seen bool, path string) ImportResult {
	res := ImportResult{File: path, Status: ImportFail}
	raw, err := os.ReadFile(path)
	if err != nil {
		res.Reason = err.Error()
		return res
	}
	parsed, err := mail.Parse(raw)
	if err != nil {
		res.Reason = "parse: " + err.Error()
		return res
	}
	size := int64(len(raw))
	fp := labelFP(parsed, size)
	if duplicate(ctx, db, folderID, fp, parsed.MessageID, size) {
		res.Status = Duplicate
		return res
	}
	plan, err := planThread(ctx, db, acc, parsed)
	if err != nil {
		res.Reason = "thread: " + err.Error()
		return res
	}
	flags := ""
	if seen {
		flags = `\Seen`
	}
	ids, _ := mail.ThreadKeys(parsed)
	stmts := append(plan.pre, sqlite.Statement{
		SQL: `INSERT INTO messages(account_id, folder_id, server_folder_id, server_uidvalidity, server_uid, thread_id,
			message_id, in_reply_to, refs, subject, from_addr, to_addrs, cc_addrs, reply_to, date, flags, size,
			snippet, body_text, body_html, body_truncated, has_attachments, label_fp, list_unsubscribe, list_unsubscribe_post, list_id, auth_results, security_status, security_reason, security_scanned_at, security_verify_dkim, rule_sent, entered_at)
			VALUES (?, ?, NULL, NULL, NULL, ` + plan.expr + `, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, 0, 0, unixepoch())`,
		Args: append(append([]any{acc.id, folderID}, plan.args...),
			nullable(parsed.MessageID), nullable(parsed.InReplyTo), nullable(strings.Join(parsed.References, " ")), parsed.Subject, parsed.From,
			jsonList(parsed.To), jsonList(parsed.Cc), nullable(parsed.ReplyTo), parsed.Date.Unix(), flags, size,
			mail.Snippet(parsed.Text), parsed.Text, nullable(parsed.HTML), parsed.Truncated, len(parsed.Attachments) > 0, nullable(fp),
			parsed.ListUnsubscribe, parsed.ListUnsubscribePost, parsed.ListID, nullable(parsed.AuthenticationResults)),
	})
	for _, id := range ids { // last_insert_rowid() is still the message here
		stmts = append(stmts, sqlite.Statement{
			SQL: `INSERT INTO thread_refs(account_id, message_id_hdr, thread_id) VALUES (?, ?, (SELECT thread_id FROM messages WHERE rowid = last_insert_rowid()))
				ON CONFLICT(account_id, message_id_hdr) DO UPDATE SET thread_id = excluded.thread_id`,
			Args: []any{acc.id, id},
		})
	}
	if plan.target != 0 {
		stmts = append(stmts, sqlite.Statement{
			SQL:  "UPDATE threads SET last_date = (SELECT max(date) FROM messages WHERE thread_id = threads.id) WHERE id = ?",
			Args: []any{plan.target},
		})
	}
	tres, err := db.Transaction(ctx, stmts)
	if err != nil {
		res.Reason = err.Error()
		return res
	}
	msgID := tres[len(plan.pre)].LastInsertID
	res.UID = msgID

	if len(parsed.Attachments) > 0 {
		var atts []sqlite.Statement
		for _, a := range parsed.Attachments {
			atts = append(atts, sqlite.Statement{
				SQL:  "INSERT INTO attachments(message_id, part, encoding, charset, filename, content_type, size, content_id, inline) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
				Args: []any{msgID, a.Part, a.Encoding, a.Charset, a.Filename, a.ContentType, a.Size, nullable(a.ContentID), a.Inline},
			})
		}
		ares, err := db.Transaction(ctx, atts)
		if err != nil {
			res.Reason = "attachments: " + err.Error()
			return res
		}
		var cache []sqlite.Statement
		for i, a := range parsed.Attachments {
			// A part at the parse cap is a leading slice, not the file.
			if len(a.Data) == 0 || a.Size >= mail.MaxAttachmentBytes {
				continue
			}
			p := filepath.Join(cacheDir, fmt.Sprint(ares[i].LastInsertID))
			if err := store.WriteFileAtomic(p, a.Data); err != nil {
				res.Reason = "cache: " + err.Error()
				return res
			}
			cache = append(cache, sqlite.Statement{
				SQL:  "UPDATE attachments SET cache_path = ?, last_used = ? WHERE id = ?",
				Args: []any{p, time.Now().Unix(), ares[i].LastInsertID},
			})
		}
		if len(cache) > 0 {
			if _, err := db.Transaction(ctx, cache); err != nil {
				res.Reason = "cache: " + err.Error()
				return res
			}
		}
	}
	if parsed.Damaged {
		res.Reason = strings.Join(parsed.Warnings, "; ")
	}
	res.Status = Imported
	return res
}

// duplicate reports whether the folder already holds the message, by its
// label fingerprint when it has one (covers mail without a Message-ID), else
// by Message-ID and size.
func duplicate(ctx context.Context, db *store.DB, folderID int64, fp, messageID string, size int64) bool {
	query, args := "", []any{}
	switch {
	case fp != "":
		query, args = "SELECT 1 FROM messages WHERE folder_id = ? AND label_fp = ? LIMIT 1", []any{folderID, fp}
	case messageID != "":
		query, args = "SELECT 1 FROM messages WHERE folder_id = ? AND message_id = ? AND size = ? LIMIT 1", []any{folderID, messageID, size}
	default:
		return false
	}
	rows, err := db.Query(ctx, query, args...)
	return err == nil && len(rows.Values) > 0
}

// WalkEml lists the .eml files of dir (recursively), sorted by name: the
// date prefixes of an export keep them chronological.
func WalkEml(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(d.Name()), ".eml") {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}
