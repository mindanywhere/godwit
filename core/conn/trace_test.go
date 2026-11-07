package conn

import (
	"errors"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/stretchr/testify/assert"
)

func TestMaskTrace(t *testing.T) {
	raw := "T7 UID FETCH 1:* (UID FLAGS BODY.PEEK[1]<0.3000>)\r\n" +
		"* 3 FETCH (UID 42 FLAGS (\\Seen $Personal) BODY[1]<0> {38}\r\n" +
		"Hi Alice,\r\ncall 13800138000 today\r\n" +
		"ok)\r\n" +
		"* (\"Grüße\")\n" +
		"T7 OK done\r\n" +
		"* 4 FET"
	want := "T7 UID FETCH 1:* (UID FLAGS BODY.PEEK[1]<0.3000>)\n" +
		"* 3 FETCH (UID 42 FLAGS (\\Seen .xxxxxxxx) BODY[1]<0> {38}\n" +
		"… 35 bytes in 2 lines of text\n" +
		"xx)\n" +
		"* (\"xx????x\") <LF>\n" +
		"T7 OK xxxx\n" +
		"* 4 xxx <no EOL>"
	assert.Equal(t, want, maskTrace([]byte(raw)))
}

func TestMaskTraceCutsLongLines(t *testing.T) {
	got := maskTrace([]byte("* 1 FETCH (" + strings.Repeat("A ", 400) + ")\r\n"))
	assert.Contains(t, got, "… (+")
	assert.Less(t, len(got), traceLineMax+30)
}

func TestTraceKeepsTheEnd(t *testing.T) {
	tr := &trace{}
	tr.Write([]byte("T1 LOGIN alice hunter2\r\n"))
	tr.arm()
	for range 3 * traceMax / 10 {
		tr.Write([]byte("0123456789"))
	}
	tr.Write([]byte("end"))
	b := tr.bytes()
	assert.Len(t, b, traceMax)
	assert.True(t, strings.HasSuffix(string(b), "end"))
	assert.NotContains(t, string(b), "hunter2")
}

func TestRedactedTraceKeepsLines(t *testing.T) {
	raw := "T3 SELECT \"INBOX\"\r\n" +
		"* 172 EXISTS\r\n" +
		"* 3 FETCH (UID 86420730005 FLAGS (\\Seen))\r\n" +
		"Hi Alice,\r\nsecret body\r\n" +
		"T5 LOGIN alice hunter2\r\n" +
		"T6 AUTHENTICATE PLAIN dGVzdAB0ZXN0ADEyMzQ=\r\n"
	want := "T3 SELECT \"INBOX\"\n" +
		"* 172 EXISTS\n" +
		"* 3 FETCH (UID 86420730005 FLAGS (\\Seen))\n" +
		"… 24 bytes in 2 lines of text\n" +
		"T5 LOGIN [redacted]\n" +
		"T6 AUTHENTICATE [redacted]"
	assert.Equal(t, want, redactedTrace([]byte(raw)))
}

func TestRedactedTraceLeavesUntaggedLogin(t *testing.T) {
	// Without a tag there is no command, so the line is not redacted.
	assert.Equal(t, "* LOGIN x", redactedLine("* LOGIN x"))
	assert.Equal(t, "T1 NOOP", redactedLine("T1 NOOP"))
}

func TestRedactedTraceCutsLongLines(t *testing.T) {
	got := redactedTrace([]byte("* 1 FETCH (" + strings.Repeat("A ", 600) + ")\r\n"))
	assert.Contains(t, got, "… (+")
	assert.Less(t, len(got), redactedLineMax+30)
}

func TestSetRawTraceWriter(t *testing.T) {
	var buf strings.Builder
	SetRawTraceWriter(&buf)
	defer SetRawTraceWriter(nil)
	tr := &trace{}
	tr.arm()
	tr.Write([]byte("T2 NOOP\r\n"))
	LogTrace(nil, nil) // no client: nothing anywhere
	assert.Empty(t, buf.String())

	c := &imapclient.Client{} // never registered: no trace to log
	LogTrace(c, errors.New("imapwire: expected atom, got \"(\""))
	assert.Empty(t, buf.String())
}
