package app

// The bulk export: a folder's mail as .eml files in a chosen directory, and
// the toast that says how many.

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/godwit-email/godwit/core/coretest"
	"github.com/godwit-email/godwit/internal/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportFolderToast(t *testing.T) {
	h, accs := mailHarness(t, nil, "alice@example.com")
	acc := accs[0]
	h.put(acc, "INBOX", coretest.Msg("bob@example.com", "alice@example.com", "mail 1", "body", at(h.clock.Now(), 1)))
	h.put(acc, "INBOX", coretest.Msg("carol@example.com", "alice@example.com", "mail 2", "body", at(h.clock.Now(), 2)))
	h.syncNow(acc)
	h.waitMail("two mails", func() bool { return len(h.items()) == 2 })

	dir := t.TempDir()
	h.plat.mu.Lock()
	h.plat.dir = dir
	h.plat.mu.Unlock()

	f := h.folderOf(acc, "inbox")
	h.app.mail.exportFolder(f)

	// The toast lives one frame: View adds it to the window and clears
	// a.toasts, so look between the drain and the frame.
	deadline := time.Now().Add(10 * time.Second)
	for {
		h.drain()
		found := false
		for _, ts := range h.app.toasts {
			if ts.Title == i18n.Tf("mail_export_done", 2, dir) {
				found = true
			}
		}
		h.tt.Frame()
		if found {
			break
		}
		require.Less(t, time.Now(), deadline, "no export toast; dir still %v", func() any {
			names, err := os.ReadDir(dir)
			return map[bool]string{err == nil: fmt.Sprint(len(names))}[err == nil] + " files"
		}())
		time.Sleep(2 * time.Millisecond)
	}
	names, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, names, 2, "both mails are .eml files")
	for _, de := range names {
		assert.True(t, strings.HasSuffix(de.Name(), ".eml"), de.Name())
	}
}
