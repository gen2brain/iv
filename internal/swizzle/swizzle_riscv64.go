//go:build riscv64 && !noasm

package swizzle

//go:noescape
func swizzleBGRARVV(p []byte) int

var useRVV = haveV()

func swizzleBGRA(p []byte) int {
	if useRVV {
		return swizzleBGRARVV(p)
	}

	return 0
}
