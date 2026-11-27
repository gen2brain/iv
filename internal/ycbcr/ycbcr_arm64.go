//go:build arm64 && !noasm

package ycbcr

//go:noescape
func rgbaNEON(dst, y, cb, cr *byte, n int) int

//go:noescape
func bgraNEON(dst, y, cb, cr *byte, n int) int

//go:noescape
func rgba2NEON(dst, y, cb, cr *byte, n int) int

//go:noescape
func bgra2NEON(dst, y, cb, cr *byte, n int) int

var impls = []impl{
	{"neon", true, rgbaNEON, bgraNEON, rgba2NEON, bgra2NEON},
}
