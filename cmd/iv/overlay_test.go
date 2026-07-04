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

func TestOverlayed(t *testing.T) {
	v := &view{showInfo: true, infoCache: map[string][]string{}}
	v.args = []info{{Name: "cam.jpg"}}
	v.infoCache["cam.jpg"] = []string{"TestCam Model123", "f/5.6", "ISO 800"}

	px := image.Pt(overlayMargin+1, 200-overlayMargin-1)

	img := solid(400, 200, 200)
	out := v.overlayed(img)
	if out.RGBAAt(px.X, px.Y).R == 200 {
		t.Error("overlay did not draw on the returned image")
	}
	if img.RGBAAt(px.X, px.Y).R != 200 {
		t.Error("overlayed mutated its input buffer (cached original corrupted)")
	}

	// disabled, image returned unchanged
	v.showInfo = false
	clean := solid(400, 200, 200)
	if v.overlayed(clean) != clean {
		t.Error("overlayed changed image while disabled")
	}

	// no EXIF, placeholder still draws so the toggle stays visible
	w2 := &view{showInfo: true, infoCache: map[string][]string{}}
	w2.args = []info{{Name: "/no/such/plain.png", Format: "PNG"}}
	none := solid(400, 200, 200)
	out2 := w2.overlayed(none)
	if out2.RGBAAt(px.X, px.Y).R == 200 {
		t.Error("no-EXIF placeholder overlay did not draw")
	}
	if none.RGBAAt(px.X, px.Y).R != 200 {
		t.Error("no-EXIF overlayed mutated its input buffer")
	}
}
