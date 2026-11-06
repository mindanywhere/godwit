package main

// import-eml imports .eml files into a local folder (folders.local): mail
// that exists only in Godwit, never synced and never sent. Two ways to run:
//
//   - default: against the running Godwit through its Local API;
//   - -offline: straight into a closed data dir's store (no Local API), the
//     app must not be running.
//
// Both go a batch at a time with a journal beside the data directory, so an
// interrupted run resumes where it stopped. Nothing is uploaded anywhere.

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/godwit-email/godwit/core"
	"github.com/godwit-email/godwit/importeml"
	"github.com/godwit-email/godwit/rpc"
)

func importEml(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("godwit import-eml", stderr)
	dataDir := fs.String("data-dir", "", "")
	dir := fs.String("dir", "", "the folder with the .eml files")
	folder := fs.String("folder", "126邮箱备份", "the local folder to import into")
	seen := fs.Bool("seen", true, "mark the imported mail as read")
	batch := fs.Int("batch", 200, "files per call")
	offline := fs.Bool("offline", false, "import straight into the data dir; close Godwit first")
	account := fs.String("account", "", "the account's e-mail when Godwit has several")
	if code, ok := parse(fs, args); !ok {
		return code
	}
	if *dir == "" || fs.NArg() > 0 {
		fs.Usage()
		return 2
	}
	paths, err := importeml.WalkEml(*dir)
	if err != nil {
		return printErr(stderr, err)
	}
	if len(paths) == 0 {
		fmt.Fprintf(stderr, "godwit import-eml: no .eml files under %s\n", *dir)
		return 1
	}
	dd, err := dataDirOr(*dataDir)
	if err != nil {
		return printErr(stderr, err)
	}
	statePath := filepath.Join(dd, "import-eml-state.json")
	journal, err := importeml.LoadJournal(statePath)
	if err != nil {
		return printErr(stderr, err)
	}

	var call func(chunk []string) (importeml.ImportReport, error)
	if *offline {
		call, err = importOffline(ctx, dd, *folder, *seen, *account)
	} else {
		call, err = importSocket(ctx, dd, *folder, *seen, *account)
	}
	if err != nil {
		return printErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%d files, %d already imported, importing into %q\n",
		len(paths), len(paths)-len(journal.Pending(paths)), *folder)

	stopped, err := importeml.Drive(ctx, importeml.Run{
		StatePath: statePath,
		Paths:     paths,
		Batch:     *batch,
		Call:      call,
		Progress: func(pr importeml.Progress) {
			if pr.Batch.Results == nil {
				return // the run's start, which the line above told
			}
			for _, r := range pr.Batch.Results {
				if r.Status != importeml.Imported && r.Status != importeml.Duplicate {
					fmt.Fprintf(stderr, "  failed: %s: %s\n", r.File, r.Reason)
				}
			}
			fmt.Fprintf(stdout, "[%d/%d] imported %d, duplicates %d, failed %d\n",
				pr.Done, pr.Total, pr.Batch.Imported, pr.Batch.Duplicates, pr.Batch.Failed)
		},
	})
	if err != nil {
		return printErr(stderr, err)
	}
	if stopped {
		fmt.Fprintln(stderr, "stopped; the journal keeps what ended, so a later run goes on from there")
		return 1
	}
	fmt.Fprintln(stdout, "done: the folder shows after Godwit's next list refresh")
	return 0
}

// importOffline imports straight into the data dir's store. The store's
// migrations run here, on a closed database, before the app next starts.
func importOffline(ctx context.Context, dd, folder string, seen bool, email string,
) (func([]string) (importeml.ImportReport, error), error) {
	if err := guardClosed(dd); err != nil {
		return nil, err
	}
	return importeml.Offline(ctx, dd, folder, seen, email)
}

// importSocket imports through the running Godwit's Local API.
func importSocket(ctx context.Context, dd, folder string, seen bool, email string,
) (func([]string) (importeml.ImportReport, error), error) {
	c, err := rpc.Dial(dd, rpc.RoleFull, "godwit-import", nil)
	if err != nil {
		return nil, err
	}
	acc, err := pickAccount(ctx, c, email)
	if err != nil {
		c.Close()
		return nil, err
	}
	return func(chunk []string) (importeml.ImportReport, error) {
		var rep importeml.ImportReport
		if err := c.Call(ctx, "ImportEml", &rep, acc.ID, folder, seen, chunk); err != nil {
			return rep, err
		}
		return rep, nil
	}, nil
}

// pickAccount resolves the import's account: the one -account names, or the
// only account there is.
func pickAccount(ctx context.Context, c *rpc.Client, email string) (core.Account, error) {
	var accs []core.Account
	if err := c.Call(ctx, "ListAccounts", &accs); err != nil {
		return core.Account{}, err
	}
	if email != "" {
		for _, a := range accs {
			if strings.EqualFold(a.Email, email) {
				return a, nil
			}
		}
		return core.Account{}, fmt.Errorf("no account %s", email)
	}
	if len(accs) == 1 {
		return accs[0], nil
	}
	return core.Account{}, fmt.Errorf("several accounts; name one with -account (%d found)", len(accs))
}
