//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

func singleSocketPath() string {
	if xdg.RuntimeDir == "" {
		return filepath.Join(os.TempDir(), fmt.Sprintf("iv-%d.sock", os.Getuid()))
	}

	return filepath.Join(xdg.RuntimeDir, "iv.sock")
}
