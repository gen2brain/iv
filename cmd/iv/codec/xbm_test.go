package codec

import (
	"bytes"
	"image"
	"testing"
)

func TestXBMDecode(t *testing.T) {
	src := "#define t_width 8\n#define t_height 2\nstatic unsigned char t_bits[] = { 0x0f, 0xf0 };\n"

	cfg, format, err := image.DecodeConfig(bytes.NewReader([]byte(src)))
	if err != nil {
		t.Fatal("DecodeConfig:", err)
	}
	if format != "xbm" || cfg.Width != 8 || cfg.Height != 2 {
		t.Fatalf("config: format=%q %dx%d, want xbm 8x2", format, cfg.Width, cfg.Height)
	}

	m, _, err := image.Decode(bytes.NewReader([]byte(src)))
	if err != nil {
		t.Fatal("Decode:", err)
	}
	// row0 0x0f: x0..3 set (black), x4..7 clear (white); row1 0xf0: inverse
	check := func(x, y int, wantBlack bool) {
		r, _, _, _ := m.At(x, y).RGBA()
		black := r>>8 == 0
		if black != wantBlack {
			t.Errorf("px(%d,%d) black=%v, want %v", x, y, black, wantBlack)
		}
	}
	check(0, 0, true)
	check(4, 0, false)
	check(0, 1, false)
	check(4, 1, true)
}
