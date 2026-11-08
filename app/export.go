package app

// Bulk mail as .eml: a folder's or a selection's messages exported to a
// directory the user chose, and a folder of .eml brought into a local
// folder of the sidebar — the import page's run, from where the mail is.

import (
	"context"
	"path/filepath"

	"github.com/godwit-email/godwit/core"
	"github.com/godwit-email/godwit/importeml"
	"github.com/godwit-email/godwit/internal/i18n"
)

// exportMail asks for a directory, then writes the messages there: a thread
// row stands for its every message. The directory dialog blocks, so the run
// is off the UI thread; the count lands as a toast.
func (p *mailPage) exportMail(items []mailItem) {
	p.exportIDs(func(ctx context.Context) ([]int64, error) {
		return messageIDs(ctx, p.a, items)
	})
}

// exportFolder writes every message of the folder.
func (p *mailPage) exportFolder(f core.Folder) {
	p.exportIDs(func(ctx context.Context) ([]int64, error) {
		return p.a.svc.FolderMessageIDs(ctx, f.ID)
	})
}

// exportIDs runs one export: choose a directory, then write the ids' .eml
// files there.
func (p *mailPage) exportIDs(list func(ctx context.Context) ([]int64, error)) {
	a := p.a
	var dir string
	var n int
	a.do(func(ctx context.Context) error {
		var err error
		if dir, err = a.plat.OpenDirectory(); err != nil || dir == "" {
			return err
		}
		ids, err := list(ctx)
		if err != nil {
			return err
		}
		n, err = a.svc.ExportEml(ctx, dir, ids)
		return err
	}, func(err error) {
		switch {
		case err != nil:
			a.toastErr(err)
		case dir != "":
			msg := i18n.Tf("mail_export_done", n, dir)
			a.toast(msg)
			a.notifyDone("export", msg)
		}
	})
}

// notifyDone tells of a finished run off the window too: a folder's export
// takes a while, and the file manager is where the user looks. Off the UI
// thread: a platform's notification calls may wait on their own.
func (a *App) notifyDone(id, body string) {
	plat := a.plat
	a.do(func(context.Context) error {
		if !plat.NotificationsSupported() {
			return nil
		}
		if err := plat.Notify(Notification{ID: id, Title: "Godwit", Body: body}); err != nil {
			a.logf("notify: %s: %v", id, err)
		}
		return nil
	}, nil)
}

// importFolder brings the chosen directory's .eml files into the local
// folder f: the import page's run, from the folder the mail is going to.
func (p *mailPage) importFolder(f core.Folder) {
	a := p.a
	a.do(func(ctx context.Context) error {
		dir, err := a.plat.OpenDirectory()
		if err != nil || dir == "" {
			return err
		}
		paths, err := importeml.WalkEml(dir)
		if err != nil {
			return err
		}
		if len(paths) == 0 {
			a.toast(i18n.T("settings_import_no_files"))
			return nil
		}
		rep, err := a.svc.ImportEml(ctx, f.AccountID, f.Name, true, paths)
		if err == nil {
			a.notifyDone("import", i18n.Tf("mail_import_done", rep.Imported, rep.Duplicates, filepath.Base(dir)))
		}
		return err
	}, func(err error) {
		if err != nil {
			a.toastErr(err)
			return
		}
		p.reload()
	})
}

// messageIDs is the export's targets: a message row is itself, a thread row
// its every message.
func messageIDs(ctx context.Context, a *App, items []mailItem) ([]int64, error) {
	var ids []int64
	for _, it := range items {
		if !it.Thread {
			ids = append(ids, it.ID)
			continue
		}
		t, err := a.svc.GetThread(ctx, it.ID)
		if err != nil {
			return nil, err
		}
		for _, m := range t.Messages {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}
