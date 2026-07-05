package codec

import (
	"bytes"
	"image"
	"testing"
)

const xpmSample = `/* XPM */
static char *t[] = {
/* width height ncolors cpp */
"4 2 3 1",
"  c None",
". c #ff0000",
"X c white",
"  .X",
"X.  ",
};
`

func TestXPMDecode(t *testing.T) {
	m, format, err := image.Decode(bytes.NewReader([]byte(xpmSample)))
	if err != nil {
		t.Fatal("Decode:", err)
	}
	if format != "xpm" {
		t.Errorf("format %q, want xpm", format)
	}

	b := m.Bounds()
	if b.Dx() != 4 || b.Dy() != 2 {
		t.Fatalf("bounds %v, want 4x2", b)
	}

	if _, _, _, a := m.At(0, 0).RGBA(); a != 0 {
		t.Errorf("px(0,0) alpha %d, want 0 (None)", a>>8)
	}
	if r, g, b, a := m.At(2, 0).RGBA(); r>>8 != 0xff || g>>8 != 0 || b>>8 != 0 || a>>8 != 0xff {
		t.Errorf("px(2,0) = %d,%d,%d,%d; want 255,0,0,255", r>>8, g>>8, b>>8, a>>8)
	}
	if r, g, b, a := m.At(3, 0).RGBA(); r>>8 != 0xff || g>>8 != 0xff || b>>8 != 0xff || a>>8 != 0xff {
		t.Errorf("px(3,0) = %d,%d,%d,%d; want white", r>>8, g>>8, b>>8, a>>8)
	}
	if r, g, b, a := m.At(0, 1).RGBA(); r>>8 != 0xff || g>>8 != 0xff || b>>8 != 0xff || a>>8 != 0xff {
		t.Errorf("px(0,1) = %d,%d,%d,%d; want white", r>>8, g>>8, b>>8, a>>8)
	}
}

func TestXPMConfig(t *testing.T) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader([]byte(xpmSample)))
	if err != nil {
		t.Fatal("DecodeConfig:", err)
	}
	if format != "xpm" {
		t.Errorf("format %q, want xpm", format)
	}
	if cfg.Width != 4 || cfg.Height != 2 {
		t.Errorf("config %dx%d, want 4x2", cfg.Width, cfg.Height)
	}
}

func TestXPMHex(t *testing.T) {
	cases := map[string][3]uint8{
		"f00":          {0xff, 0, 0},
		"00ff00":       {0, 0xff, 0},
		"000000001000": {0, 0, 0x10},
	}
	for h, want := range cases {
		c := xpmHex(h)
		if c.R != want[0] || c.G != want[1] || c.B != want[2] {
			t.Errorf("xpmHex(%q) = %d,%d,%d; want %v", h, c.R, c.G, c.B, want)
		}
	}
}

func TestXPMGrayName(t *testing.T) {
	if c, ok := xpmGray("gray50"); !ok || c.R != 128 {
		t.Errorf("gray50 = %v ok=%v; want R=128", c, ok)
	}
	if _, ok := xpmGray("gray"); ok {
		t.Errorf("plain gray should not resolve via xpmGray")
	}
}

func TestXPMMultiCPP(t *testing.T) {
	src := `/* XPM */
static char *t[] = {
"2 1 2 2",
"   c #010203",
".. c #0a0b0c",
"  ..",
};
`
	m, _, err := image.Decode(bytes.NewReader([]byte(src)))
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, _ := m.At(0, 0).RGBA(); r>>8 != 1 || g>>8 != 2 || b>>8 != 3 {
		t.Errorf("px(0,0) = %d,%d,%d; want 1,2,3", r>>8, g>>8, b>>8)
	}
	if r, g, b, _ := m.At(1, 0).RGBA(); r>>8 != 0x0a || g>>8 != 0x0b || b>>8 != 0x0c {
		t.Errorf("px(1,0) = %d,%d,%d; want 10,11,12", r>>8, g>>8, b>>8)
	}
}
