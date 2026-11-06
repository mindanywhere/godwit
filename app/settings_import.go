package app

// The 导入 page: .eml backups brought in as mail of a local folder, which
// only Godwit holds. It runs importeml.Drive a batch at a time, as
// `godwit import-eml` does, and shares its journal, so a run started here
// resumes one started there, and the other way around.

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
	"github.com/godwit-email/godwit/importeml"
	"github.com/godwit-email/godwit/internal/i18n"
	"github.com/godwit-email/godwit/internal/kit"
	"github.com/godwit-email/godwit/internal/theme"
)

// importView is the 导入 page's form and its run, while the page lives.
type importView struct {
	a *App

	// the form
	acc    int64  // the account; 0: the first
	folder string // the local folder's name
	seen   bool   // mark the imported mail read
	dir    string // the chosen backup folder

	scanning bool
	scanned  bool     // dir was read, paths are its files
	paths    []string // the .eml files, sorted
	skipped  int      // the journal's share of paths

	// the run
	running    bool
	stopping   bool
	stopFlag   atomic.Bool // between batches: the run ends there, journal kept
	rev        uint64      // per run: a stopped run's late updates are dropped
	done       int         // files processed: imported, duplicate or failed
	total      int         // all files
	imported   int
	duplicates int
	failed     int
	failures   []importeml.ImportResult

	finished bool
	stopped  bool
	failOpen bool
	err      error
}

func newImportView(a *App) *importView {
	// The folder's name is not translated: it is data the user may keep.
	return &importView{a: a, folder: "Imported Mail", seen: true}
}

func (v *importView) view(c *ui.Context) {
	t, a := theme.Of(c), v.a
	pageHeading(c, i18n.T("settings.import"), i18n.T("settings.import.sub"))
	if len(a.accounts) == 0 {
		kit.Text(c, i18n.T("settings.import.no_account"), t.Text.Sm).TextColor(t.Muted)
		return
	}
	if !v.accountKnown() {
		v.acc = a.accounts[0].ID
	}
	settingsSection(c, i18n.T("settings.import.target"), i18n.T("settings.import.local"),
		func() {
			settingRow(c, i18n.T("settings.import.account"), "", func() {
				opts := make([]kit.Option[int64], len(a.accounts))
				for i := range a.accounts {
					opts[i] = kit.Option[int64]{Value: a.accounts[i].ID, Label: accountName(a.accounts[i])}
				}
				settingSelect(c, i18n.T("settings.import.account"), &v.acc, opts).Disabled(v.running)
			})
		},
		func() {
			settingRow(c, i18n.T("settings.import.folder"), i18n.T("settings.import.folder.help"), func() {
				kit.Input(c, &v.folder, kit.InputOpts{Placeholder: "Imported Mail"}).
					Width(theme.SettingsInputWidth).Radius(t.Radius.Lg).Label(i18n.T("settings.import.folder")).Disabled(v.running)
			})
		},
		func() { kit.SwitchRow(c, &v.seen, i18n.T("settings.import.seen"), "") },
	)
	settingsSection(c, i18n.T("settings.import.source"), "",
		func() {
			settingRow(c, i18n.T("settings.import.source"), "", func() {
				settingButton(c, i18n.T("settings.import.choose"), kit.Outline, v.running || v.scanning).OnClick(v.choose)
			})
		},
		func() { settingBlock(c, func() { v.status(c) }) },
	)
}

// accountKnown reports whether acc is one of the accounts still.
func (v *importView) accountKnown() bool {
	for _, acc := range v.a.accounts {
		if acc.ID == v.acc {
			return true
		}
	}
	return false
}

// syncing is whether a sync holds an account now: the import would wait on
// its lock, so the page says so instead of starting it.
func (v *importView) syncing() bool { return len(v.a.syncing) > 0 }

// canStart reports whether the form can import now.
func (v *importView) canStart() bool {
	return !v.running && !v.scanning && v.scanned && len(v.paths) > 0 &&
		strings.TrimSpace(v.folder) != "" && !v.syncing()
}

// status is the chosen folder's files, and the run's progress or its result.
func (v *importView) status(c *ui.Context) {
	t, th := theme.Of(c), c.Theme()
	if v.dir != "" {
		kit.Text(c, v.dir, t.Text.Sm).TextColor(t.Muted).SingleLine()
	}
	switch {
	case v.scanning:
		kit.Text(c, i18n.T("settings.import.scanning"), t.Text.Sm).TextColor(t.Muted)
		return
	case !v.scanned:
		return
	case len(v.paths) == 0:
		kit.Text(c, i18n.T("settings.import.no_files"), t.Text.Sm).TextColor(t.Muted)
		return
	}
	kit.Text(c, i18n.Tf("settings.import.files", len(v.paths), v.skipped), t.Text.Sm).TextColor(t.Muted)
	if v.err != nil {
		kit.Text(c, humanize(v.err), t.Text.Sm).TextColor(t.Danger)
	}
	if v.running {
		done := v.done
		ui.Row(c).Gap(th.Space(6)).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).Gap(th.Space(1.5)).Children(func() {
				kit.Text(c, i18n.Tf("settings.import.running", done, v.total), t.Text.Sm)
				ui.Row(c).FillWidth().Height(th.Space(2)).Radius(t.Radius.Full).Background(t.Border).AlignItems(ui.Stretch).Clip().
					Label(i18n.T("settings.import.progress")).Children(func() {
					ui.Box(c).Grow(float32(done)).Background(t.Primary)
					ui.Box(c).Grow(float32(v.total - done))
				})
			})
			settingButton(c, i18n.T("settings.import.stop"), kit.Outline, v.stopping).OnClick(v.stop)
		})
		kit.Text(c, i18n.Tf("settings.import.counts", v.imported, v.duplicates, v.failed), t.Text.Sm).TextColor(t.Muted)
	} else {
		if v.finished {
			if v.stopped {
				kit.Text(c, i18n.T("settings.import.stopped"), t.Text.Sm).TextColor(t.Muted)
			} else {
				kit.Text(c, i18n.Tf("settings.import.done", v.imported, v.duplicates, v.failed), t.Text.Sm)
			}
		}
		ui.Row(c).Gap(th.Space(3)).AlignItems(ui.Center).Children(func() {
			if v.syncing() {
				kit.Text(c, i18n.T("settings.import.busy"), t.Text.Sm).TextColor(t.Muted).Grow(1).Shrink(1)
			} else {
				ui.Spacer(c)
			}
			settingButton(c, i18n.T("settings.import.start"), kit.Default, !v.canStart()).OnClick(v.start)
		})
	}
	if len(v.failures) > 0 {
		kit.Collapsible(c, &v.failOpen, func() {
			kit.Text(c, i18n.Tf("settings.import.failed", len(v.failures)), t.Text.Sm)
		}, func() {
			for _, f := range v.failures {
				ui.Column(c).FillWidth().Gap(th.Space(0.5)).Padding(th.Space(1), th.Space(2)).Children(func() {
					kit.Text(c, f.File, t.Text.Xs).Font(theme.Mono).SingleLine()
					kit.Text(c, f.Reason, t.Text.Xs).TextColor(t.Muted)
				})
			}
		})
	}
}

// choose asks for the backup folder off the UI thread and reads its .eml
// files and the journal there.
func (v *importView) choose() {
	if v.running || v.scanning {
		return
	}
	a, plat := v.a, v.a.plat
	v.scanning = true
	var dir string
	var paths []string
	var skipped int
	a.do(func(context.Context) error {
		var err error
		if dir, err = plat.OpenDirectory(); err != nil || dir == "" {
			return err
		}
		if paths, err = importeml.WalkEml(dir); err != nil {
			return err
		}
		j, err := importeml.LoadJournal(journalPath(plat))
		if err != nil {
			return err
		}
		skipped = len(paths) - len(j.Pending(paths))
		return nil
	}, func(err error) {
		v.scanning = false
		if err != nil {
			a.toastErr(err)
			return
		}
		if dir == "" { // cancelled
			return
		}
		v.dir, v.paths, v.skipped, v.scanned = dir, paths, skipped, true
		v.finished, v.stopped, v.err, v.failures = false, false, nil, nil
		v.done, v.total, v.imported, v.duplicates, v.failed = 0, 0, 0, 0, 0
	})
}

// start runs the import in batches on a goroutine: each batch's report comes
// back on the UI thread.
func (v *importView) start() {
	a := v.a
	if !v.canStart() {
		return
	}
	folder := strings.TrimSpace(v.folder)
	if folder == "" {
		return
	}
	v.rev++
	rev := v.rev
	v.running, v.stopping = true, false
	v.finished, v.stopped, v.err = false, false, nil
	v.done, v.total = v.skipped, len(v.paths)
	v.imported, v.duplicates, v.failed = 0, 0, 0
	v.failures = nil
	v.stopFlag.Store(false)
	acc, seen := v.acc, v.seen
	run := importeml.Run{
		StatePath: journalPath(a.plat),
		Paths:     v.paths,
		Batch:     200,
		Call: func(chunk []string) (importeml.ImportReport, error) {
			return a.svc.ImportEml(a.ctx, acc, folder, seen, chunk)
		},
		Stop: v.stopFlag.Load,
		Progress: func(pr importeml.Progress) {
			a.onUI(func() {
				if rev == v.rev {
					v.apply(pr)
				}
			})
		},
	}
	go func() {
		stopped, err := importeml.Drive(a.ctx, run)
		a.onUI(func() {
			if rev != v.rev {
				return
			}
			v.running, v.stopping = false, false
			v.finished, v.stopped, v.err = true, stopped, err
			v.skipped += v.imported + v.duplicates // what the journal now holds
			if err != nil {
				a.toastErr(err)
			}
			a.mail.reload() // the folder shows at once
		})
	}()
}

// apply is a batch's progress: the counts, and its failures.
func (v *importView) apply(pr importeml.Progress) {
	v.done, v.total = pr.Done, pr.Total
	v.imported, v.duplicates, v.failed = pr.Imported, pr.Duplicates, pr.Failed
	for _, r := range pr.Batch.Results {
		if r.Status == importeml.ImportFail {
			v.failures = append(v.failures, r)
		}
	}
}

// stop ends the run at the batch it is in: it writes its journal first.
func (v *importView) stop() {
	if !v.running || v.stopping {
		return
	}
	v.stopping = true
	v.stopFlag.Store(true)
}

// journalPath is the import journal's file, by the data directory (the
// CLI's, so both share it).
func journalPath(plat Platform) string {
	dir, _ := plat.DataDir()
	if dir == "" {
		return "import-eml-state.json"
	}
	return filepath.Join(dir, "import-eml-state.json")
}
