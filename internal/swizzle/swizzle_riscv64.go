//go:build riscv64 && !noasm

package swizzle

//go:noescape
func swizzleBGRARVV(dst, src []byte) int

var useRVV = haveV()

func swizzleBGRA(dst, src []byte) int {
	if useRVV {
		return swizzleBGRARVV(dst, src)
	}

	return 0
}
