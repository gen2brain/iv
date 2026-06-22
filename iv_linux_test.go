//go:build linux

package iv

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/pbnjay/pixfont"
)

func TestElideTitle(t *testing.T) {
	short := "a.jpg"
	if got := elideTitle(short, 10000); got != short {
		t.Errorf("short title changed: %q", got)
	}

	long := "/home/user/pictures/some-very-long-filename.jpg"
	for _, mw := range []int{400, 200, 120} {
		got := elideTitle(long, mw)

		if pixfont.MeasureString(got) > mw {
			t.Errorf("maxWidth=%d: %q measures %d > max", mw, got, pixfont.MeasureString(got))
		}
		if !strings.Contains(got, "...") {
			t.Errorf("maxWidth=%d: %q missing ellipsis", mw, got)
		}
		if !strings.HasPrefix(got, "/") || !strings.HasSuffix(got, ".jpg") {
			t.Errorf("maxWidth=%d: %q dropped start/end", mw, got)
		}
	}

	if got := elideTitle(long, 5); got != "" {
		t.Errorf("tiny width should yield empty, got %q", got)
	}
}

func TestScaleIconDown(t *testing.T) {
	small := image.NewRGBA(image.Rect(0, 0, 64, 64))
	if got := scaleIconDown(small, maxIconX11); got != small {
		t.Error("icon within max should be returned as-is")
	}

	big := image.NewRGBA(image.Rect(0, 0, 512, 256))
	got := scaleIconDown(big, maxIconX11)
	if got.Bounds().Dx() != maxIconX11 || got.Bounds().Dy() != maxIconX11/2 {
		t.Errorf("scaled to %dx%d, want %dx%d", got.Bounds().Dx(), got.Bounds().Dy(), maxIconX11, maxIconX11/2)
	}
}

func TestWLCanvas(t *testing.T) {
	data := make([]byte, 2*4)

	bgra := &wlCanvas{data: data, stride: 2 * 4, w: 2, h: 1, useXBGR: false}
	bgra.Set(0, 0, color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff})
	if data[0] != 0x33 || data[1] != 0x22 || data[2] != 0x11 || data[3] != 0xff {
		t.Errorf("BGRA order wrong: %v", data[:4])
	}

	rgba := &wlCanvas{data: data, stride: 2 * 4, w: 2, h: 1, useXBGR: true}
	rgba.Set(1, 0, color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff})
	if data[4] != 0x11 || data[5] != 0x22 || data[6] != 0x33 || data[7] != 0xff {
		t.Errorf("RGBA order wrong: %v", data[4:8])
	}

	bgra.Set(-1, 0, color.RGBA{R: 1, G: 2, B: 3, A: 4})
	bgra.Set(5, 5, color.RGBA{R: 1, G: 2, B: 3, A: 4})
}
