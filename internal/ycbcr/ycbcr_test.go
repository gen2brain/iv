package ycbcr

import (
	"bytes"
	"image/color"
	"testing"
)

func reference(dst, y, cb, cr []byte, half, bgra bool) {
	for i := range y {
		c := i
		if half {
			c = i / 2
		}
		put(dst[4*i:], y[i], cb[c], cr[c], bgra)
	}
}

func check(t *testing.T, name string, conv func(dst, y, cb, cr []byte, bgra bool), half bool) {
	t.Helper()

	y := make([]byte, 256)
	for i := range y {
		y[i] = byte(i)
	}
	cb := make([]byte, 256)
	cr := make([]byte, 256)
	got := make([]byte, 4*256)
	want := make([]byte, 4*256)

	for _, bgra := range []bool{false, true} {
		for b := 0; b < 256; b++ {
			for r := 0; r < 256; r++ {
				for i := range cb {
					cb[i], cr[i] = byte(b), byte(r)
				}
				conv(got, y, cb, cr, bgra)
				reference(want, y, cb, cr, half, bgra)
				if !bytes.Equal(got, want) {
					t.Fatalf("%s bgra=%v cb=%d cr=%d: mismatch", name, bgra, b, r)
				}
			}
		}
	}
}

func TestRowExhaustive(t *testing.T) {
	for _, k := range impls {
		if !k.ok {
			t.Logf("%s: not supported by this CPU", k.name)
			continue
		}

		check(t, k.name, func(dst, y, cb, cr []byte, bgra bool) {
			n := run(k, dst, y, cb, cr, len(y), bgra, false)
			for i := n; i < len(y); i++ {
				put(dst[4*i:], y[i], cb[i], cr[i], bgra)
			}
		}, false)
		check(t, k.name+"/half", func(dst, y, cb, cr []byte, bgra bool) {
			n := run(k, dst, y, cb, cr, len(y), bgra, true)
			for i := n; i < len(y); i++ {
				put(dst[4*i:], y[i], cb[i/2], cr[i/2], bgra)
			}
		}, true)
	}
}

func TestRowLengths(t *testing.T) {
	for n := 0; n <= 70; n++ {
		y := make([]byte, n)
		cb := make([]byte, n)
		cr := make([]byte, n)
		for i := range y {
			y[i], cb[i], cr[i] = byte(i*37), byte(i*91+5), byte(255-i*53)
		}

		for _, bgra := range []bool{false, true} {
			got := make([]byte, 4*n)
			want := make([]byte, 4*n)

			Row(got, y, cb, cr, bgra)
			reference(want, y, cb, cr, false, bgra)
			if !bytes.Equal(got, want) {
				t.Fatalf("Row n=%d bgra=%v: mismatch", n, bgra)
			}

			Row2(got, y, cb[:(n+1)/2], cr[:(n+1)/2], bgra)
			reference(want, y, cb, cr, true, bgra)
			if !bytes.Equal(got, want) {
				t.Fatalf("Row2 n=%d bgra=%v: mismatch", n, bgra)
			}
		}
	}
}

func TestPutMatchesColor(t *testing.T) {
	p := make([]byte, 4)
	put(p, 200, 30, 220, false)
	r, g, b := color.YCbCrToRGB(200, 30, 220)
	if p[0] != r || p[1] != g || p[2] != b || p[3] != 0xff {
		t.Fatalf("put = %v, want %d %d %d 255", p, r, g, b)
	}
}

func BenchmarkRow2(b *testing.B) {
	const w = 1920
	y := make([]byte, w)
	cb := make([]byte, w/2)
	cr := make([]byte, w/2)
	dst := make([]byte, 4*w)
	b.SetBytes(4 * w * 1080)

	for i := 0; i < b.N; i++ {
		for r := 0; r < 1080; r++ {
			Row2(dst, y, cb, cr, true)
		}
	}
}

func BenchmarkDrawEquivalent(b *testing.B) {
	const w = 1920
	y := make([]byte, w)
	cb := make([]byte, w/2)
	cr := make([]byte, w/2)
	dst := make([]byte, 4*w)
	b.SetBytes(4 * w * 1080)

	for i := 0; i < b.N; i++ {
		for r := 0; r < 1080; r++ {
			for x := 0; x < w; x++ {
				put(dst[4*x:], y[x], cb[x/2], cr[x/2], true)
			}
		}
	}
}
