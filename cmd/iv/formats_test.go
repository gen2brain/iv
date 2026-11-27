package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"testing"

	"github.com/gen2brain/vpx/webp"
)

func TestWebPLossyColor(t *testing.T) {
	want := color.RGBA{R: 200, G: 40, B: 60, A: 255}
	src := image.NewRGBA(image.Rect(0, 0, 32, 32))
	draw.Draw(src, src.Bounds(), &image.Uniform{C: want}, image.Point{}, draw.Src)

	var buf bytes.Buffer
	if err := webp.Encode(&buf, src, webp.EncodeOptions{Quality: 100}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "c.webp")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	img, err := decodeImage("WEBP", bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}

	frames, _, err := decodeAll(info{Name: path, Format: "WEBP"})
	if err != nil || len(frames) == 0 {
		t.Fatalf("decodeAll: %v, %d frames", err, len(frames))
	}

	for name, m := range map[string]image.Image{"decodeImage": img, "decodeAll": frames[0]} {
		r, g, b, _ := m.At(16, 16).RGBA()
		got := [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
		for i, w := range [3]int{int(want.R), int(want.G), int(want.B)} {
			if d := got[i] - w; d < -4 || d > 4 {
				t.Fatalf("%s: color %v, want %v", name, got, want)
			}
		}
	}
}
