package mailsync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/godwit-email/godwit/core/store"
	"github.com/stretchr/testify/require"
)

const importParent = "Message-ID: <a@x>\r\nDate: Mon, 02 Jan 2006 15:04:05 +0000\r\n" +
	"From: Ann <ann@x>\r\nTo: Bob <bob@x>\r\nSubject: with file\r\n" +
	"Content-Type: multipart/mixed; boundary=b\r\n\r\n" +
	"--b\r\nContent-Type: text/plain\r\n\r\nhi\r\n" +
	"--b\r\nContent-Type: application/pdf; name=\"a.pdf\"\r\nContent-Disposition: attachment; filename=\"a.pdf\"\r\n" +
	"Content-Transfer-Encoding: base64\r\n\r\nJVBERi0x\r\n--b--\r\n"

const importReply = "Message-ID: <b@x>\r\nIn-Reply-To: <a@x>\r\nDate: Mon, 02 Jan 2006 16:04:05 +0000\r\n" +
	"From: Bob <bob@x>\r\nTo: Ann <ann@x>\r\nSubject: Re: with file\r\n\r\nok\r\n"

func importTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "mail.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	_, err = db.Execute(t.Context(), "INSERT INTO accounts(id,email,imap_host) VALUES (1,'me@customer.test','imap.receiver.test')")
	require.NoError(t, err)
	return db
}

// An import lands in a local folder with no server location, threaded like
// synced mail, its attachment cached as the only copy; importing again finds
// duplicates, and a server folder's name cannot be taken.
func TestImportEml(t *testing.T) {
	db := importTestDB(t)
	dir := t.TempDir()
	for name, raw := range map[string]string{"2006-01-02 1504 parent.eml": importParent, "2006-01-02 1604 reply.eml": importReply} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(raw), 0o600))
	}
	folderID, err := EnsureLocalFolder(t.Context(), db, 1, "126邮箱备份")
	require.NoError(t, err)
	rows, err := db.Query(t.Context(), "SELECT local, role FROM folders WHERE id = ?", folderID)
	require.NoError(t, err)
	require.Equal(t, []any{int64(1), ""}, rows.Values[0], "a local folder, no role")

	rep, err := ImportEml(t.Context(), db, t.TempDir(), 1, folderID, "me@customer.test", true,
		[]string{filepath.Join(dir, "2006-01-02 1504 parent.eml"), filepath.Join(dir, "2006-01-02 1604 reply.eml")})
	require.NoError(t, err)
	require.Equal(t, 2, rep.Imported)
	require.Equal(t, 0, rep.Failed)

	rows, err = db.Query(t.Context(), `SELECT message_id, server_folder_id, flags, label_fp, size, thread_id FROM messages ORDER BY id`)
	require.NoError(t, err)
	require.Len(t, rows.Values, 2)
	for _, r := range rows.Values {
		require.Nil(t, r[1], "no server location")
		require.Contains(t, store.Str(r[2]), `\Seen`)
		require.NotNil(t, r[3], "the label fingerprint is set")
	}
	require.Equal(t, "a@x", store.Str(rows.Values[0][0]))
	require.Equal(t, "b@x", store.Str(rows.Values[1][0]))
	require.Equal(t, store.Int(rows.Values[0][5]), store.Int(rows.Values[1][5]), "the reply joins the parent's conversation")

	rows, err = db.Query(t.Context(), "SELECT a.filename, a.cache_path FROM attachments a")
	require.NoError(t, err)
	require.Len(t, rows.Values, 1)
	require.Equal(t, "a.pdf", store.Str(rows.Values[0][0]))
	cachePath := store.Str(rows.Values[0][1])
	data, err := os.ReadFile(cachePath)
	require.NoError(t, err)
	require.Equal(t, "%PDF-1", string(data), "the transfer encoding is undone")

	refs, err := db.Query(t.Context(), "SELECT count(*) FROM thread_refs")
	require.NoError(t, err)
	require.Equal(t, int64(2), store.Int(refs.Values[0][0]))

	again, err := ImportEml(t.Context(), db, t.TempDir(), 1, folderID, "me@customer.test", true,
		[]string{filepath.Join(dir, "2006-01-02 1504 parent.eml"), filepath.Join(dir, "2006-01-02 1604 reply.eml")})
	require.NoError(t, err)
	require.Equal(t, 2, again.Duplicates)
	require.Equal(t, 0, again.Imported)
	count, err := db.Query(t.Context(), "SELECT count(*) FROM messages")
	require.NoError(t, err)
	require.Equal(t, int64(2), store.Int(count.Values[0][0]), "nothing imported twice")
}

func TestEnsureLocalFolderNameTaken(t *testing.T) {
	db := importTestDB(t)
	_, err := db.Execute(t.Context(), "INSERT INTO folders(id,account_id,name,role,uidvalidity) VALUES (1,1,'INBOX','inbox',1)")
	require.NoError(t, err)
	_, err = EnsureLocalFolder(t.Context(), db, 1, "INBOX")
	require.Error(t, err, "a server folder's name cannot become a local one")
	id, err := EnsureLocalFolder(t.Context(), db, 1, "126邮箱备份")
	require.NoError(t, err)
	again, err := EnsureLocalFolder(t.Context(), db, 1, "126邮箱备份")
	require.NoError(t, err)
	require.Equal(t, id, again, "the folder is reused, not created twice")
}
