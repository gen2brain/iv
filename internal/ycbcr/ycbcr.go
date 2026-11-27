// Package ycbcr converts YCbCr rows to RGBA or BGRA.
package ycbcr

import "image/color"

type kernel func(dst, y, cb, cr *byte, n int) int

type impl struct {
	name                     string
	ok                       bool
	rgba, bgra, rgba2, bgra2 kernel
}

var active = pick()

func pick() *impl {
	for i := range impls {
		if impls[i].ok {
			return &impls[i]
		}
	}

	return nil
}

func run(k impl, dst, y, cb, cr []byte, n int, bgra, half bool) int {
	f := k.rgba
	switch {
	case half && bgra:
		f = k.bgra2
	case half:
		f = k.rgba2
	case bgra:
		f = k.bgra
	}

	return f(&dst[0], &y[0], &cb[0], &cr[0], n)
}

// Row converts a row with full chroma.
func Row(dst, y, cb, cr []byte, bgra bool) {
	n := len(y)
	if n == 0 {
		return
	}
	_, _, _ = dst[4*n-1], cb[n-1], cr[n-1]

	i := 0
	if active != nil {
		i = run(*active, dst, y, cb, cr, n, bgra, false)
	}
	for ; i < n; i++ {
		put(dst[4*i:4*i+4], y[i], cb[i], cr[i], bgra)
	}
}

// Row2 converts a row with horizontally halved chroma.
func Row2(dst, y, cb, cr []byte, bgra bool) {
	n := len(y)
	if n == 0 {
		return
	}
	_, _, _ = dst[4*n-1], cb[(n-1)/2], cr[(n-1)/2]

	i := 0
	if active != nil {
		i = run(*active, dst, y, cb, cr, n, bgra, true)
	}
	for ; i < n; i++ {
		put(dst[4*i:4*i+4], y[i], cb[i/2], cr[i/2], bgra)
	}
}

func put(p []byte, y, cb, cr byte, bgra bool) {
	r, g, b := color.YCbCrToRGB(y, cb, cr)
	if bgra {
		r, b = b, r
	}
	p[0], p[1], p[2], p[3] = r, g, b, 0xff
}
