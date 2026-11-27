//go:build (!amd64 && !arm64 && !riscv64) || noasm

package swizzle

func swizzleBGRA(dst, src []byte) int { return 0 }
