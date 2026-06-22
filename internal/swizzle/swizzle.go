// Package swizzle converts pixel buffers between RGBA and BGRA byte order.
package swizzle

import "errors"

// ErrSliceNot32Bit is returned by BGRA when the input length is not a multiple of 4.
var ErrSliceNot32Bit = errors.New("input slice length is not a multiple of 4")

// BGRA swaps the red and blue channels of a 4-bytes-per-pixel buffer.
func BGRA(p []byte) error {
	if len(p)%4 != 0 {
		return ErrSliceNot32Bit
	}

	n := swizzleBGRA(p)

	for i := n; i < len(p); i += 4 {
		p[i], p[i+2] = p[i+2], p[i]
	}

	return nil
}
