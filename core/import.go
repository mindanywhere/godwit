package core

import (
	"context"

	"github.com/godwit-email/godwit/core/mailsync"
	"github.com/godwit-email/godwit/core/store"
)

// ImportEml imports .eml files as local mail of the account's local folder
// name (folders.local, created when missing): mail that exists only in
// Godwit, never synced and never sent. Local only — no egress, no
// notifications. Threading and the labels' fingerprint follow the same rules
// sync uses; attachments are cached, their only copy.
func (s *Service) ImportEml(ctx context.Context, accountID int64, folder string, seen bool, paths []string) (mailsync.ImportReport, error) {
	ctx, end, err := s.begin(ctx, accountID)
	if err != nil {
		return mailsync.ImportReport{}, err
	}
	defer end()
	unlock, err := s.lockAccount(ctx, accountID) // no sync, send or fetch while rows appear
	if err != nil {
		return mailsync.ImportReport{}, err
	}
	defer unlock()
	rows, err := s.db.Query(ctx, "SELECT email FROM accounts WHERE id = ?", accountID)
	if err != nil {
		return mailsync.ImportReport{}, err
	}
	if len(rows.Values) == 0 {
		return mailsync.ImportReport{}, ErrAccountNotFound
	}
	folderID, err := mailsync.EnsureLocalFolder(ctx, s.db, accountID, folder)
	if err != nil {
		return mailsync.ImportReport{}, err
	}
	return mailsync.ImportEml(ctx, s.db, s.cacheDir(accountID), accountID, folderID,
		store.Str(rows.Values[0][0]), seen, paths)
}
