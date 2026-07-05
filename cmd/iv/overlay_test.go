package main

import (
	"image"
	"testing"
)

func TestOverlayWindowSized(t *testing.T) {
	v := &view{
		width:     400,
		height:    300,
		showInfo:  true,
		args:      []info{{Name: "x"}},
		infoCache: map[string][]string{"x": {"ISO 100", "f/2.8"}},
	}
	v.opts.BackgroundColor = "#000000"

	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+3] = 0xff, 0xff // red
	}

	out := v.overlayed(img)

	if out.Bounds().Dx() != 400 || out.Bounds().Dy() != 300 {
		t.Fatalf("canvas %v, want 400x300", out.Bounds())
	}
	if r, _, _, _ := out.At(200, 150).RGBA(); r>>8 != 0xff {
		t.Errorf("center pixel not image color (got r=%d)", r>>8)
	}
	if r, g, b, _ := out.At(1, 1).RGBA(); r|g|b != 0 {
		t.Errorf("top-left corner not background (got %d,%d,%d)", r>>8, g>>8, b>>8)
	}
}

func TestOverlayDisabledPassthrough(t *testing.T) {
	v := &view{width: 400, height: 300, showInfo: false, args: []info{{Name: "x"}}}
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	if out := v.overlayed(img); out != img {
		t.Error("overlay disabled should return the image unchanged")
	}
}
