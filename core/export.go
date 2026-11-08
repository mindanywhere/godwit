package core

// Exporting mail as .eml: a selection's or a folder's originals, written to a
// directory the user chose. The raw message when the server serves it, else
// what is stored built into one, its HTML body only — the same fallback the
// reader's save has.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	gomail "github.com/godwit-email/godwit/core/mail"
	"github.com/godwit-email/godwit/core/store"
)

// ExportEml writes each message of ids to dir, named after its subject and
// id, and returns how many files it wrote. GetRawMessage has the original
// when the server serves it; what does not load becomes a simplified copy
// built from what is stored, its HTML body only (attachments dropped); a
// message that cannot be had at all is skipped.
func (s *Service) ExportEml(ctx context.Context, dir string, ids []int64) (int, error) {
	n := 0
	for _, id := range ids {
		raw, subject, err := s.rawOrStored(ctx, id)
		if err != nil {
			continue
		}
		name := FileName(subject, "email") + "-" + strconv.FormatInt(id, 10) + ".eml"
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// rawOrStored is a message's bytes and subject: the server's original, or a
// copy built from what is stored.
func (s *Service) rawOrStored(ctx context.Context, id int64) ([]byte, string, error) {
	rows, err := s.db.Query(ctx, "SELECT subject, from_addr, to_addrs, cc_addrs, date, body_html FROM messages WHERE id = ?", id)
	if err != nil {
		return nil, "", err
	}
	if len(rows.Values) == 0 {
		return nil, "", fmt.Errorf("message %d not found", id)
	}
	subject := store.Str(rows.Values[0][0])
	if raw, err := s.GetRawMessage(ctx, id); err == nil {
		return raw, subject, nil
	}
	raw, _, err := gomail.Build(gomail.Outgoing{
		From:    store.Str(rows.Values[0][1]),
		To:      jsonList(store.Str(rows.Values[0][2])),
		Cc:      jsonList(store.Str(rows.Values[0][3])),
		Subject: subject,
		HTML:    store.Str(rows.Values[0][5]),
	}, time.Unix(store.Int(rows.Values[0][4]), 0))
	return raw, subject, err
}

// FolderMessageIDs is every not-purged message of the folder, oldest first.
func (s *Service) FolderMessageIDs(ctx context.Context, folderID int64) ([]int64, error) {
	rows, err := s.db.Query(ctx, "SELECT id FROM messages m WHERE m.folder_id = ? AND "+notPurgedCond+" ORDER BY m.date, m.id", folderID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(rows.Values))
	for _, r := range rows.Values {
		ids = append(ids, store.Int(r[0]))
	}
	return ids, nil
}
