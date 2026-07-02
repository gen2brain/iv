//go:build !minimal

package main

import (
	"image"
	"testing"
)

func TestShutter(t *testing.T) {
	cases := map[float64]string{0.004: "1/250s", 0.5: "1/2s", 1: "1.0s", 2: "2.0s"}
	for in, want := range cases {
		if got := shutter(in); got != want {
			t.Errorf("shutter(%v) = %q, want %q", in, got, want)
		}
	}
}

func solid(w, h int, val byte) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = val
	}
	return img
}

func TestDrawOverlay(t *testing.T) {
	v := &view{showInfo: true, infoCache: map[string][]string{}}
	v.args = []info{{Name: "cam.jpg"}}
	v.infoCache["cam.jpg"] = []string{"TestCam Model123", "f/5.6", "ISO 800"}

	img := solid(400, 200, 200)
	v.drawOverlay(img)
	if img.RGBAAt(overlayMargin+1, img.Bounds().Max.Y-overlayMargin-1).R == 200 {
		t.Error("overlay did not draw (bottom-left background unchanged)")
	}

	// disabled, image unchanged
	v.showInfo = false
	clean := solid(400, 200, 200)
	if v.drawOverlay(clean) != clean {
		t.Error("drawOverlay changed image while disabled")
	}

	// no EXIF, placeholder still draws so the toggle stays visible
	w2 := &view{showInfo: true, infoCache: map[string][]string{}}
	w2.args = []info{{Name: "/no/such/plain.png", Format: "PNG"}}
	none := solid(400, 200, 200)
	w2.drawOverlay(none)
	if none.RGBAAt(overlayMargin+1, none.Bounds().Max.Y-overlayMargin-1).R == 200 {
		t.Error("no-EXIF placeholder overlay did not draw")
	}
}
