//go:build riscv64 && !noasm

package ycbcr

import "golang.org/x/sys/cpu"

//go:noescape
func rgbaRVV(dst, y, cb, cr *byte, n int) int

//go:noescape
func bgraRVV(dst, y, cb, cr *byte, n int) int

//go:noescape
func rgba2RVV(dst, y, cb, cr *byte, n int) int

//go:noescape
func bgra2RVV(dst, y, cb, cr *byte, n int) int

var impls = []impl{
	{"rvv", cpu.RISCV64.HasV, rgbaRVV, bgraRVV, rgba2RVV, bgra2RVV},
}
