//go:build amd64 && !noasm

package swizzle

// haveSSSE3 reports whether the CPU supports SSSE3 (i.e. PSHUFB).
func haveSSSE3() bool

//go:noescape
func swizzleBGRASSSE3(p []byte) int

var useSSSE3 = haveSSSE3()

func swizzleBGRA(p []byte) int {
	if useSSSE3 {
		return swizzleBGRASSSE3(p)
	}

	return 0
}
