package codec

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"testing"
)

func pngEntry(t *testing.T, osType string, w, h int) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}

	entry := make([]byte, 8+buf.Len())
	copy(entry[0:4], osType)
	binary.BigEndian.PutUint32(entry[4:8], uint32(8+buf.Len()))
	copy(entry[8:], buf.Bytes())

	return entry
}

func makeICNS(entries ...[]byte) []byte {
	var body bytes.Buffer
	for _, e := range entries {
		body.Write(e)
	}

	out := make([]byte, 8+body.Len())
	copy(out[0:4], "icns")
	binary.BigEndian.PutUint32(out[4:8], uint32(8+body.Len()))
	copy(out[8:], body.Bytes())

	return out
}

func TestICNSDecodeLargest(t *testing.T) {
	data := makeICNS(
		pngEntry(t, "ic07", 128, 128),
		pngEntry(t, "ic09", 512, 512),
		[]byte("TOC \x00\x00\x00\x08"), // non-image entry, must be skipped
	)

	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal("DecodeConfig:", err)
	}
	if format != "icns" {
		t.Errorf("format %q, want icns", format)
	}
	if cfg.Width != 512 || cfg.Height != 512 {
		t.Errorf("config %dx%d, want 512x512 (largest)", cfg.Width, cfg.Height)
	}

	m, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal("Decode:", err)
	}
	if b := m.Bounds(); b.Dx() != 512 || b.Dy() != 512 {
		t.Errorf("decoded %v, want 512x512", b)
	}
}

func TestUnpackICNS(t *testing.T) {
	// literal run of 2 (0x01), then repeat run of 3 (0x80)
	src := []byte{0x01, 0xAA, 0xBB, 0x80, 0xCC}
	dst := make([]byte, 5)
	unpackICNS(src, dst)
	if want := []byte{0xAA, 0xBB, 0xCC, 0xCC, 0xCC}; !bytes.Equal(dst, want) {
		t.Errorf("got %v, want %v", dst, want)
	}
}

func TestDecodeRLE(t *testing.T) {
	// 2x2 image: planes R,G,B as 12 literal bytes; mask fully opaque
	data := append([]byte{0x0b}, 1, 2, 3, 4, 10, 20, 30, 40, 100, 110, 120, 130)
	mask := []byte{0xff, 0xff, 0xff, 0xff}
	img := decodeRLE(data, 2, mask)
	if r, g, b, a := img.At(0, 0).RGBA(); r>>8 != 1 || g>>8 != 10 || b>>8 != 100 || a>>8 != 0xff {
		t.Errorf("px(0,0) = %d,%d,%d,%d; want 1,10,100,255", r>>8, g>>8, b>>8, a>>8)
	}
}
