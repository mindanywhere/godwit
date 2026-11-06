package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/godwit-email/godwit/core"
	"github.com/godwit-email/godwit/core/coretest"
)

// writeEml writes n plain messages into dir, named as a date-prefixed export
// is, sorted by name.
func writeEml(t *testing.T, dir string, n int) {
	t.Helper()
	for i := range n {
		raw := fmt.Sprintf("From: sender%d@example.test\r\nTo: alice@example.com\r\n"+
			"Subject: message %d\r\nDate: Mon, 02 Jan 2006 15:%02d:05 -0700\r\n"+
			"Message-ID: <m%d@example.test>\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nhello %d\r\n",
			i, i, i, i, i)
		name := filepath.Join(dir, fmt.Sprintf("2006-01-02 15%02d00 message %d.eml", i, i))
		if err := os.WriteFile(name, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// importHarness is withAccount without automatic syncs, which would hold the
// account's lock under the import.
func importHarness(t *testing.T) (*harness, core.Account) {
	t.Helper()
	var acc core.Account
	h := newHarnessWith(t, []coretest.Option{coretest.WithoutAutoSync()}, func(e *coretest.Env) {
		acc = e.AddAccount(t, "alice@example.com")
	})
	h.until("initial load", func() bool { return h.app.loaded })
	return h, acc
}

func TestImportPageImportsEml(t *testing.T) {
	h, acc := importHarness(t)
	dir := t.TempDir()
	writeEml(t, dir, 2)
	h.plat.dir = dir

	p := h.openSettings("import")
	h.see("备份目录")
	h.click("选择目录…")
	h.called("OpenDirectory")
	h.see("共 2 个文件，0 个已导入过（将跳过）")

	h.click("开始导入")
	h.until("the import ends", func() bool { return p.imp.finished && !p.imp.running })
	h.see("导入完成：新增 2，重复 0，失败 0")

	folders, err := h.env.Svc.ListFolders(context.Background(), acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	var folder core.Folder
	for _, f := range folders {
		if f.Name == "Imported Mail" {
			folder = f
		}
	}
	if folder.ID == 0 {
		t.Fatalf("no local folder: %+v", folders)
	}
	page, err := h.env.Svc.ListMessages(context.Background(), core.View{FolderID: folder.ID}, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("the folder holds %d messages", len(page.Items))
	}

	// A second scan sees the journal: the files are skipped, not imported twice.
	h.click("选择目录…")
	h.see("共 2 个文件，2 个已导入过（将跳过）")
}

func TestImportPageEmptyFolder(t *testing.T) {
	h, _ := importHarness(t)
	h.plat.dir = t.TempDir()
	h.openSettings("import")
	h.click("选择目录…")
	h.see("目录里没有 .eml 文件")
}

// A sync holds the account's lock: the page says so and does not start.
func TestImportPageWaitsForSync(t *testing.T) {
	h, acc := importHarness(t)
	dir := t.TempDir()
	writeEml(t, dir, 1)
	h.plat.dir = dir

	p := h.openSettings("import")
	h.click("选择目录…")
	h.see("共 1 个文件，0 个已导入过（将跳过）")
	h.app.syncing[acc.ID] = true
	h.tt.Frame()
	h.see("同步进行中，请稍后再试")
	h.click("开始导入")
	if p.imp.running {
		t.Fatal("the import started while a sync held the account")
	}
	delete(h.app.syncing, acc.ID)
}

// stop ends the run at the batch it is in.
func TestImportStopMarksStopping(t *testing.T) {
	v := &importView{running: true}
	v.stop()
	if !v.stopping || !v.stopFlag.Load() {
		t.Fatalf("stop: stopping %v, flag %v", v.stopping, v.stopFlag.Load())
	}
	v.running = false
	v.stopping = false
	v.stop()
	if v.stopping {
		t.Fatal("a stop outside a run changed the view")
	}
}
