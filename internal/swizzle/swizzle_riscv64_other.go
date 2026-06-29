//go:build riscv64 && !linux && !noasm

package swizzle

func haveV() bool { return false }
