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

func TestCopyBGRA(t *testing.T) {
	lengths := []int{0, 4, 8, 12, 16, 32, 60, 64, 68, 124, 128, 132, 256, 4096, 4100}

	for _, n := range lengths {
		for _, off := range []int{0, 1, 3} {
			buf := make([]byte, n+off)
			src := buf[off:]
			for i := range src {
				src[i] = byte(i*7 + i/4)
			}

			orig := append([]byte(nil), src...)
			want := reference(src)

			dst := make([]byte, n+off+4)[off+4:]
			for i := range dst {
				dst[i] = 0xaa
			}

			if err := CopyBGRA(dst, src); err != nil {
				t.Fatalf("n=%d off=%d: %v", n, off, err)
			}

			if !bytes.Equal(dst, want) {
				t.Fatalf("n=%d off=%d: mismatch\n got=%v\nwant=%v", n, off, dst, want)
			}

			if !bytes.Equal(src, orig) {
				t.Fatalf("n=%d off=%d: src modified", n, off)
			}
		}
	}

	if err := CopyBGRA(make([]byte, 3), []byte{1, 2, 3}); err != ErrSliceNot32Bit {
		t.Fatalf("got %v, want %v", err, ErrSliceNot32Bit)
	}

	if err := CopyBGRA(make([]byte, 4), make([]byte, 8)); err != ErrLengthMismatch {
		t.Fatalf("got %v, want %v", err, ErrLengthMismatch)
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

func BenchmarkCopyBGRA(b *testing.B) {
	src := make([]byte, 1920*1080*4)
	dst := make([]byte, len(src))
	b.SetBytes(int64(len(src)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = CopyBGRA(dst, src)
	}
}
