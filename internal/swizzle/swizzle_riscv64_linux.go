//go:build riscv64 && linux && !noasm

package swizzle

import (
	"syscall"
	"unsafe"
)

const (
	sysRISCVHWProbe   = 258
	hwprobeKeyIMAExt0 = 0x4
	hwprobeIMAV       = 0x4
)

type hwprobePair struct {
	key   int64
	value uint64
}

// haveV reports whether the ratified RVV 1.0 vector extension is present (via riscv_hwprobe, Linux 6.4+).
func haveV() bool {
	pair := hwprobePair{key: hwprobeKeyIMAExt0}

	_, _, errno := syscall.Syscall6(sysRISCVHWProbe, uintptr(unsafe.Pointer(&pair)), 1, 0, 0, 0, 0)
	if errno != 0 || pair.key == -1 {
		return false
	}

	return pair.value&hwprobeIMAV != 0
}
