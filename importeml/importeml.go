// Package importeml is the .eml import's batch driver: the journal that lets
// an interrupted run resume, and the offline entry into a closed data dir.
// The app's 导入 page and `godwit import-eml` share it, so a run started in
// one goes on in the other.
//
// The UI and the CLI may not reach core/mailsync or core/store themselves
// (app/boundary_test.go): this package is how they import at all.
package importeml

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/godwit-email/godwit/core/mailsync"
	"github.com/godwit-email/godwit/core/store"
)

// The files' outcomes, as mailsync tells them.
const (
	Imported   = mailsync.Imported
	Duplicate  = mailsync.Duplicate
	ImportFail = mailsync.ImportFail
)

// ImportReport and ImportResult are mailsync's, which Service.ImportEml
// returns.
type (
	ImportReport = mailsync.ImportReport
	ImportResult = mailsync.ImportResult
)

// WalkEml lists the .eml files of dir, recursively, sorted by name: the date
// prefixes of an export keep them chronological.
func WalkEml(dir string) ([]string, error) { return mailsync.WalkEml(dir) }

// Journal is the journal file's content: the files whose import ended. A
// failed file is absent, so the next run tries it again.
type Journal struct {
	Done map[string]bool `json:"done"`
}

// LoadJournal reads the journal at path: an empty one when the file is not
// there yet.
func LoadJournal(path string) (*Journal, error) {
	j := &Journal{Done: map[string]bool{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return j, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, j); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if j.Done == nil {
		j.Done = map[string]bool{}
	}
	return j, nil
}

// Save writes the journal at path, replacing the file at once: a crash keeps
// the earlier one whole.
func (j *Journal) Save(path string) error {
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Pending is paths without the files the journal holds, in order.
func (j *Journal) Pending(paths []string) []string {
	return slices.DeleteFunc(slices.Clone(paths), func(p string) bool { return j.Done[p] })
}

// Progress is a run's state: before the first batch (Batch zero) and after
// each one. Done and Total are what a progress bar shows; the cumulative
// counts add the batches' up.
type Progress struct {
	Done       int // files processed: imported, duplicate or failed
	Total      int // the run's files
	Imported   int
	Duplicates int
	Failed     int
	Batch      ImportReport // the batch just imported; zero before the first
}

// Run is what Drive runs.
type Run struct {
	StatePath string // the journal's file
	Paths     []string
	Batch     int // files per call; 0 is 200
	Call      func([]string) (ImportReport, error)
	Stop      func() bool    // between batches: true ends the run, as ctx does, journal kept
	Progress  func(Progress) // nil for none
}

// Drive imports the run's paths a batch at a time, journaling the files
// whose import ended and reporting after each batch. Stop and ctx end the
// run between batches (reported as stopped, the journal keeping what ended);
// a batch's error ends it with that error.
func Drive(ctx context.Context, r Run) (stopped bool, err error) {
	j, err := LoadJournal(r.StatePath)
	if err != nil {
		return false, err
	}
	pending := j.Pending(r.Paths)
	pr := Progress{Done: len(r.Paths) - len(pending), Total: len(r.Paths)}
	if r.Progress != nil {
		r.Progress(pr)
	}
	batch := r.Batch
	if batch <= 0 {
		batch = 200
	}
	for start := 0; start < len(pending); start += batch {
		if ctx.Err() != nil || (r.Stop != nil && r.Stop()) {
			return true, nil
		}
		end := min(start+batch, len(pending))
		rep, err := r.Call(pending[start:end])
		if err != nil {
			return false, err
		}
		for _, res := range rep.Results {
			if res.Status == Imported || res.Status == Duplicate {
				j.Done[res.File] = true
			}
		}
		if err := j.Save(r.StatePath); err != nil {
			return false, err
		}
		pr.Done += len(rep.Results)
		pr.Imported += rep.Imported
		pr.Duplicates += rep.Duplicates
		pr.Failed += rep.Failed
		pr.Batch = rep
		if r.Progress != nil {
			r.Progress(pr)
		}
	}
	return false, nil
}

// Offline imports straight into the data dir's store, for a Godwit that is
// not running: the store's migrations run here, on the closed database.
// account is the account's e-mail when Godwit has several.
func Offline(ctx context.Context, dataDir, folder string, seen bool, account string) (func([]string) (ImportReport, error), error) {
	db, err := store.Open(ctx, filepath.Join(dataDir, "godwit.db"))
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, "SELECT id, email FROM accounts ORDER BY id")
	if err != nil {
		db.Close()
		return nil, err
	}
	if len(rows.Values) == 0 {
		db.Close()
		return nil, fmt.Errorf("no account in %s", dataDir)
	}
	accountID, accEmail := store.Int(rows.Values[0][0]), store.Str(rows.Values[0][1])
	if account != "" && !strings.EqualFold(account, accEmail) {
		db.Close()
		return nil, fmt.Errorf("no account %s (found %s)", account, accEmail)
	}
	folderID, err := mailsync.EnsureLocalFolder(ctx, db, accountID, folder)
	if err != nil {
		db.Close()
		return nil, err
	}
	cacheDir := filepath.Join(dataDir, "cache", strconv.FormatInt(accountID, 10))
	return func(chunk []string) (ImportReport, error) {
		return mailsync.ImportEml(ctx, db, cacheDir, accountID, folderID, accEmail, seen, chunk)
	}, nil
}
