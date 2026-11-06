package main

// guardClosed refuses while Godwit holds the data dir: the app keeps the
// lock file open, so an exclusive open of it fails only while Godwit runs.

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func guardClosed(dd string) error {
	p := filepath.Join(dd, "godwit.lock")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return nil // never started here: nothing holds it
	}
	h, err := windows.CreateFile(windows.StringToUTF16Ptr(p),
		windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return fmt.Errorf("Godwit is running; close it before -offline import")
	}
	windows.CloseHandle(h)
	return nil
}
