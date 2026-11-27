//go:build (!amd64 && !arm64 && !riscv64) || noasm

package ycbcr

var impls []impl
