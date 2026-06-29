//go:build windows

package main

import (
	"os"
	"path/filepath"
)

// singleSocketPath returns the AF_UNIX socket path under the per-user temp dir (Windows 10 1803+).
func singleSocketPath() string {
	return filepath.Join(os.TempDir(), "iv.sock")
}
