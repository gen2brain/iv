package main

import (
	"encoding/binary"
	"testing"
)

func TestRefinePNG(t *testing.T) {
	sig := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}

	chunk := func(typ string, n int) []byte {
		b := make([]byte, 8+n+4)
		binary.BigEndian.PutUint32(b[:4], uint32(n))
		copy(b[4:8], typ)
		return b
	}

	static := append(append(append([]byte{}, sig...), chunk("IHDR", 13)...), chunk("IDAT", 0)...)
	if f, anim := refinePNG(static); f != "PNG" || anim {
		t.Fatalf("static png: (%q, %v), want (PNG, false)", f, anim)
	}

	apng := append(append(append([]byte{}, sig...), chunk("IHDR", 13)...), chunk("acTL", 8)...)
	if f, anim := refinePNG(apng); f != "APNG" || !anim {
		t.Fatalf("apng: (%q, %v), want (APNG, true)", f, anim)
	}
}
