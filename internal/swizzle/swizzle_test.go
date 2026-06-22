package swizzle

import (
	"bytes"
	"testing"
)

func reference(p []byte) []byte {
	out := append([]byte(nil), p...)
	for i := 0; i+3 < len(out); i += 4 {
		out[i], out[i+2] = out[i+2], out[i]
	}

	return out
}

func TestBGRA(t *testing.T) {
	lengths := []int{0, 4, 8, 12, 16, 32, 60, 64, 68, 124, 128, 132, 256, 4096, 4100}

	for _, n := range lengths {
		p := make([]byte, n)
		for i := range p {
			p[i] = byte(i*7 + i/4)
		}

		want := reference(p)

		got := append([]byte(nil), p...)
		if err := BGRA(got); err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}

		if !bytes.Equal(got, want) {
			t.Fatalf("n=%d: mismatch\n got=%v\nwant=%v", n, got, want)
		}
	}

	if err := BGRA([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error for length not multiple of 4")
	}
}

func BenchmarkBGRA(b *testing.B) {
	p := make([]byte, 1920*1080*4)
	b.SetBytes(int64(len(p)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = BGRA(p)
	}
}
