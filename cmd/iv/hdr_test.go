package main

import (
	"bytes"
	"image"
	"testing"

	"github.com/mdouchement/hdr"
	"github.com/mdouchement/hdr/codec/rgbe"
	"github.com/mdouchement/hdr/hdrcolor"
)

func makeHDR(w, h int) *bytes.Buffer {
	m := hdr.NewRGB(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := float64(x) / 4.0
			m.SetRGB(x, y, hdrcolor.RGB{R: v, G: v * 0.5, B: 8 - v})
		}
	}
	var buf bytes.Buffer
	rgbe.Encode(&buf, m)
	return &buf
}

func TestHDRDecodeToneMaps(t *testing.T) {
	img, err := decodeImage("HDR", makeHDR(32, 24))
	if err != nil {
		t.Fatal(err)
	}
	if _, isHDR := img.(hdr.Image); isHDR {
		t.Fatal("result is still HDR; tone-mapping did not run")
	}
	if b := img.Bounds(); b.Dx() != 32 || b.Dy() != 24 {
		t.Fatalf("bounds %v, want 32x24", b)
	}
}

func TestHDRDecodeConfig(t *testing.T) {
	cfg, format, err := image.DecodeConfig(makeHDR(48, 16))
	if err != nil {
		t.Fatal(err)
	}
	if format != "hdr" {
		t.Errorf("format %q, want hdr", format)
	}
	if cfg.Width != 48 || cfg.Height != 16 {
		t.Errorf("config %dx%d, want 48x16", cfg.Width, cfg.Height)
	}
}
