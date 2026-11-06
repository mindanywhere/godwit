//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// guardClosed refuses while Godwit holds the data dir: an exclusive flock on
// the lock file fails only while the app runs.
func guardClosed(dd string) error {
	p := filepath.Join(dd, "godwit.lock")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return nil // never started here: nothing holds it
	}
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("Godwit is running; close it before -offline import")
	}
	unix.Flock(int(f.Fd()), unix.LOCK_UN)
	return nil
}
