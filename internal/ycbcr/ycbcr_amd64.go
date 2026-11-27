//go:build amd64 && !noasm

package ycbcr

import "golang.org/x/sys/cpu"

//go:noescape
func rgbaAVX2(dst, y, cb, cr *byte, n int) int

//go:noescape
func bgraAVX2(dst, y, cb, cr *byte, n int) int

//go:noescape
func rgba2AVX2(dst, y, cb, cr *byte, n int) int

//go:noescape
func bgra2AVX2(dst, y, cb, cr *byte, n int) int

//go:noescape
func rgbaSSE41(dst, y, cb, cr *byte, n int) int

//go:noescape
func bgraSSE41(dst, y, cb, cr *byte, n int) int

//go:noescape
func rgba2SSE41(dst, y, cb, cr *byte, n int) int

//go:noescape
func bgra2SSE41(dst, y, cb, cr *byte, n int) int

var impls = []impl{
	{"avx2", cpu.X86.HasAVX2, rgbaAVX2, bgraAVX2, rgba2AVX2, bgra2AVX2},
	{"sse41", cpu.X86.HasSSE41, rgbaSSE41, bgraSSE41, rgba2SSE41, bgra2SSE41},
}
