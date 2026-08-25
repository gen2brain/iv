package main

import (
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/gen2brain/pngn"
)

func TestFileInfoPNG(t *testing.T) {
	frame := func(v uint8) *image.RGBA {
		m := image.NewRGBA(image.Rect(0, 0, 8, 8))
		for i := range m.Pix {
			m.Pix[i] = v
		}

		return m
	}

	dir := t.TempDir()

	still := filepath.Join(dir, "still.png")
	f, err := os.Create(still)
	if err != nil {
		t.Fatal(err)
	}

	if err := pngn.Encode(f, frame(0x40)); err != nil {
		t.Fatal(err)
	}

	f.Close()

	moving := filepath.Join(dir, "moving.png")
	f, err = os.Create(moving)
	if err != nil {
		t.Fatal(err)
	}

	anim := &pngn.PNG{
		Image: []image.Image{frame(0x40), frame(0x80)},
		Delay: []float64{0.1, 0.1},
	}

	if err := pngn.EncodeAll(f, anim); err != nil {
		t.Fatal(err)
	}

	f.Close()

	for _, c := range []struct {
		path   string
		format string
		anim   bool
	}{
		{still, "PNG", false},
		{moving, "APNG", true},
	} {
		got, err := fileInfo(c.path)
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}

		if got.Format != c.format || got.IsAnimation != c.anim {
			t.Errorf("%s: (%q, %v), want (%s, %v)", filepath.Base(c.path), got.Format, got.IsAnimation, c.format, c.anim)
		}
	}
}
