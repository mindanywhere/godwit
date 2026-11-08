package core

// ExportEml: a folder's or a selection's mail as .eml files, the server's
// original when it serves it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportEml(t *testing.T) {
	e := newSyncEnv(t)
	e.put(t, "INBOX", eml("a@x", "Bob <bob@example.com>", "alice@example.com", "Weekly: good news", day, "hello"))
	e.put(t, "INBOX", eml("b@x", "bob@example.com", "alice@example.com", "Weekly: good news", day, "again"))
	e.sync(t)
	ids, err := e.s.FolderMessageIDs(bg, e.folderID(t, "INBOX"))
	require.NoError(t, err)
	require.Len(t, ids, 2)

	dir := t.TempDir()
	n, err := e.s.ExportEml(bg, dir, ids)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	names, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, names, 2, "same subject: the ids keep the names apart")
	for i, de := range names {
		if !strings.HasPrefix(de.Name(), "Weekly") || !strings.HasSuffix(de.Name(), ".eml") {
			t.Errorf("file %d named %q: want the subject and the .eml", i, de.Name())
		}
		raw, err := os.ReadFile(filepath.Join(dir, de.Name()))
		require.NoError(t, err)
		assert.Contains(t, string(raw), "Subject: Weekly: good news")
		assert.Contains(t, string(raw), "\r\n\r\n", "a full message, headers from body apart")
	}

	n, err = e.s.ExportEml(bg, dir, []int64{ids[0], 999999})
	require.NoError(t, err)
	assert.Equal(t, 1, n, "a message that is gone is skipped")
}
