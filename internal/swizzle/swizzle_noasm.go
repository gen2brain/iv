//go:build (!amd64 && !arm64 && !riscv64) || noasm

package swizzle

func swizzleBGRA(p []byte) int { return 0 }
