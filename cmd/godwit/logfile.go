package main

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// logMaxBytes is how big godwit.log may get before it rotates.
const logMaxBytes = 5 << 20 // 5 MiB

// logKeep is how many rotated files (godwit.log.1 …) survive.
const logKeep = 3

// logWriter appends to <dataDir>/logs/godwit.log, rotating it into
// godwit.log.1 … godwit.log.<logKeep> when it passes logMaxBytes: the
// journal of syncs, failures and the like, so a past problem can be
// analysed. Rotation happens on Write under mu; a failed open leaves w
// nil and the next Write retries, so a read-only directory degrades to
// no file logging rather than failing the app.
type logWriter struct {
	mu   sync.Mutex
	path string
	w    *os.File
	size int64
}

func newLogWriter(dataDir string) *logWriter {
	return &logWriter{path: filepath.Join(dataDir, "logs", "godwit.log")}
}

func (l *logWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.w == nil {
		if err := l.open(); err != nil {
			return 0, err
		}
	}
	if l.size+int64(len(p)) > logMaxBytes {
		l.rotate()
		if err := l.open(); err != nil {
			return 0, err
		}
	}
	n, err := l.w.Write(p)
	l.size += int64(n)
	return n, err
}

func (l *logWriter) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.w == nil {
		return nil
	}
	err := l.w.Close()
	l.w = nil
	return err
}

func (l *logWriter) open() error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	l.w, l.size = f, info.Size()
	return nil
}

// rotate closes the log and renames it to .1, shifting the older ones on
// and dropping the last; the caller reopens. Best effort: a rename that
// fails leaves the older file where it is.
func (l *logWriter) rotate() {
	l.w.Close()
	l.w = nil
	for i := logKeep; i > 1; i-- {
		os.Rename(l.path+"."+strconv.Itoa(i-1), l.path+"."+strconv.Itoa(i))
	}
	os.Rename(l.path, l.path+".1")
}
