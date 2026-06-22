//go:build arm64 && !noasm

package swizzle

//go:noescape
func swizzleBGRA(p []byte) int
