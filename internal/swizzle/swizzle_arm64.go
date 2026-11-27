//go:build arm64 && !noasm

package swizzle

//go:noescape
func swizzleBGRA(dst, src []byte) int
