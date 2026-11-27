// Package swizzle converts pixel buffers between RGBA and BGRA byte order.
package swizzle

import "errors"

// ErrSliceNot32Bit is returned when the input length is not a multiple of 4.
var ErrSliceNot32Bit = errors.New("input slice length is not a multiple of 4")

// ErrLengthMismatch is returned by CopyBGRA when dst and src differ in length.
var ErrLengthMismatch = errors.New("dst and src slice lengths differ")

// BGRA swaps the red and blue channels of a 4-bytes-per-pixel buffer.
func BGRA(p []byte) error {
	return CopyBGRA(p, p)
}

// CopyBGRA copies src to dst, swapping the red and blue channels; dst may be src itself but must not partially overlap it.
func CopyBGRA(dst, src []byte) error {
	if len(src)%4 != 0 {
		return ErrSliceNot32Bit
	}

	if len(dst) != len(src) {
		return ErrLengthMismatch
	}

	n := swizzleBGRA(dst, src)

	for i := n; i < len(src); i += 4 {
		dst[i], dst[i+1], dst[i+2], dst[i+3] = src[i+2], src[i+1], src[i], src[i+3]
	}

	return nil
}
