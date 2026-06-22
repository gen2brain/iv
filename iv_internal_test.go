package iv

import (
	"image"
	"image/color"
	"testing"
)

func TestParseHexColor(t *testing.T) {
	cases := []struct {
		in   string
		want color.RGBA
		err  bool
	}{
		{in: "#FFFFFF", want: color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}},
		{in: "#000000", want: color.RGBA{A: 0xff}},
		{in: "#4A90D9", want: color.RGBA{R: 0x4a, G: 0x90, B: 0xd9, A: 0xff}},
		{"zzz", color.RGBA{}, true},
	}

	for _, c := range cases {
		got, err := parseHexColor(c.in)
		if c.err {
			if err == nil {
				t.Errorf("%s: expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %v, want %v", c.in, got, c.want)
		}
	}
}

func TestImageToRGBA(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	if got := imageToRGBA(src); got != src {
		t.Error("contiguous origin RGBA should be returned as-is")
	}

	gray := image.NewGray(image.Rect(0, 0, 2, 2))
	if got := imageToRGBA(gray); got.Bounds() != image.Rect(0, 0, 2, 2) {
		t.Errorf("non-RGBA conversion bounds %v, want 0,0,2,2", got.Bounds())
	}
}

func TestImageToRGBASubImage(t *testing.T) {
	parent := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			parent.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 7, A: 0xff})
		}
	}

	sub := parent.SubImage(image.Rect(3, 4, 7, 9)).(*image.RGBA)
	got := imageToRGBA(sub)

	if got.Bounds() != image.Rect(0, 0, 4, 5) {
		t.Fatalf("bounds %v, want 0,0,4,5", got.Bounds())
	}
	if got.Stride != 4*4 {
		t.Errorf("stride %d, want 16 (contiguous)", got.Stride)
	}

	r, g, b, _ := got.At(0, 0).RGBA()
	if uint8(r>>8) != 3 || uint8(g>>8) != 4 || uint8(b>>8) != 7 {
		t.Errorf("pixel (0,0) = %d,%d,%d, want 3,4,7 (parent 3,4)", r>>8, g>>8, b>>8)
	}
}
