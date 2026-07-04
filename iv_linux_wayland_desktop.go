//go:build linux

package iv

import (
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
)

// installDesktopIcon writes an <appID>.desktop and PNG, once if absent, for app_id icon matching on CSD compositors lacking xdg-toplevel-icon.
func (v *viewWayland) installDesktopIcon(img image.Image) {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		dataHome = filepath.Join(home, ".local", "share")
	}

	appsDir := filepath.Join(dataHome, "applications")
	desktopPath := filepath.Join(appsDir, v.appID+".desktop")
	if _, err := os.Stat(desktopPath); err == nil {
		return
	}

	iconsDir := filepath.Join(dataHome, "icons")
	if os.MkdirAll(appsDir, 0o755) != nil || os.MkdirAll(iconsDir, 0o755) != nil {
		return
	}

	iconPath := filepath.Join(iconsDir, v.appID+".png")
	if err := writeIconPNG(iconPath, img); err != nil {
		return
	}

	exe, err := os.Executable()
	if err != nil {
		exe = v.appID
	}

	entry := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=%s\nExec=%s\nIcon=%s\nStartupWMClass=%s\nNoDisplay=true\n",
		v.appID, exe, iconPath, v.appID)
	_ = os.WriteFile(desktopPath, []byte(entry), 0o644)
}

func writeIconPNG(path string, img image.Image) error {
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return png.Encode(f, out)
}
