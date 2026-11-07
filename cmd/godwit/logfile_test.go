package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogWriterRotates(t *testing.T) {
	dir := t.TempDir()
	l := newLogWriter(dir)
	defer l.Close()
	chunk := make([]byte, logMaxBytes/2) // two chunks fit; the third rotates
	for i := range chunk {
		chunk[i] = byte('a' + i%26)
	}
	for range 3 { // 1.5 logs: godwit.log holds the rest
		if _, err := l.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := os.Stat(l.path); got == nil || got.Size() != logMaxBytes/2 {
		t.Fatalf("godwit.log = %v", got)
	}
	if _, err := os.Stat(l.path + ".1"); err != nil {
		t.Fatalf("no godwit.log.1: %v", err)
	}
	for range logKeep + 1 { // one more write per rotation: only .1 … .logKeep survive
		if _, err := l.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= logKeep; i++ {
		if _, err := os.Stat(l.path + "." + string(rune('0'+i))); err != nil {
			t.Fatalf("no godwit.log.%d: %v", i, err)
		}
	}
	if _, err := os.Stat(l.path + "." + string(rune('0'+logKeep+1))); err == nil {
		t.Fatalf("godwit.log.%d survived", logKeep+1)
	}
}

func TestLogWriterReopensAfterFailedOpen(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil { // a file where a directory must be
		t.Fatal(err)
	}
	l := newLogWriter(filepath.Join(blocker, "logs"))
	if _, err := l.Write([]byte("x")); err == nil {
		t.Fatal("expected an error: the logs directory cannot be made")
	}
	l.path = filepath.Join(dir, "logs", "godwit.log")
	if _, err := l.Write([]byte("again")); err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
	b, err := os.ReadFile(l.path)
	if err != nil || string(b) != "again" {
		t.Fatalf("read back %q, %v", b, err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := os.Remove(l.path); err != nil {
		t.Fatalf("remove after close: %v", err)
	}
}
