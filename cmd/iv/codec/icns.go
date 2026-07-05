// Package codec registers extra image decoders with the standard image package.
package codec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	_ "image/png"
	"io"
)

func init() {
	image.RegisterFormat("icns", "icns", decodeICNS, decodeICNSConfig)
}

var errICNS = errors.New("icns: no decodable image entry")

// rleType maps a 24-bit RLE color OSType to its pixel dimension and mask OSType.
var rleType = map[string]struct {
	dim  int
	mask string
}{
	"is32": {16, "s8mk"},
	"il32": {32, "l8mk"},
	"ih32": {48, "h8mk"},
	"it32": {128, "t8mk"},
}

// icnsEntries parses an icns container into a map of OSType to entry data.
func icnsEntries(r io.Reader) (map[string][]byte, error) {
	head := make([]byte, 8)
	if _, err := io.ReadFull(r, head); err != nil {
		return nil, err
	}
	if string(head[:4]) != "icns" {
		return nil, image.ErrFormat
	}

	body, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	out := make(map[string][]byte)
	for off := 0; off+8 <= len(body); {
		typ := string(body[off : off+4])
		length := int(binary.BigEndian.Uint32(body[off+4 : off+8]))
		if length < 8 || off+length > len(body) {
			break
		}

		out[typ] = body[off+8 : off+length]
		off += length
	}

	return out, nil
}

// best returns the OSType and dimension of the largest decodable icon.
func best(entries map[string][]byte) (string, int) {
	var bestType string
	var bestDim int

	for typ, data := range entries {
		dim := 0
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
			dim = max(cfg.Width, cfg.Height)
		} else if t, ok := rleType[typ]; ok {
			dim = t.dim
		}

		if dim > bestDim {
			bestType, bestDim = typ, dim
		}
	}

	return bestType, bestDim
}

func decodeICNS(r io.Reader) (image.Image, error) {
	entries, err := icnsEntries(r)
	if err != nil {
		return nil, err
	}

	typ, _ := best(entries)
	if typ == "" {
		return nil, errICNS
	}

	if t, ok := rleType[typ]; ok {
		return decodeRLE(entries[typ], t.dim, entries[t.mask]), nil
	}

	img, _, err := image.Decode(bytes.NewReader(entries[typ]))

	return img, err
}

func decodeICNSConfig(r io.Reader) (image.Config, error) {
	entries, err := icnsEntries(r)
	if err != nil {
		return image.Config{}, err
	}

	typ, dim := best(entries)
	if typ == "" {
		return image.Config{}, errICNS
	}

	if _, ok := rleType[typ]; ok {
		return image.Config{ColorModel: color.RGBAModel, Width: dim, Height: dim}, nil
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(entries[typ]))

	return cfg, err
}

// decodeRLE unpacks a 24-bit RLE color icon and applies its 8-bit alpha mask.
func decodeRLE(data []byte, dim int, mask []byte) *image.RGBA {
	if dim == 128 && len(data) >= 4 {
		data = data[4:] // it32 leading zero header
	}

	px := dim * dim
	planes := make([]byte, px*3)
	unpackICNS(data, planes)

	img := image.NewRGBA(image.Rect(0, 0, dim, dim))
	for i := 0; i < px; i++ {
		img.Pix[i*4] = planes[i]
		img.Pix[i*4+1] = planes[px+i]
		img.Pix[i*4+2] = planes[2*px+i]
		if i < len(mask) {
			img.Pix[i*4+3] = mask[i]
		} else {
			img.Pix[i*4+3] = 0xff
		}
	}

	return img
}

// unpackICNS decompresses Apple's PackBits variant (used by the RLE icons) into dst.
func unpackICNS(src, dst []byte) {
	var s, d int
	for s < len(src) && d < len(dst) {
		c := src[s]
		s++

		if c < 0x80 {
			for n := int(c) + 1; n > 0 && s < len(src) && d < len(dst); n-- {
				dst[d] = src[s]
				d++
				s++
			}
			continue
		}

		if s >= len(src) {
			break
		}
		v := src[s]
		s++
		for n := int(c) - 125; n > 0 && d < len(dst); n-- {
			dst[d] = v
			d++
		}
	}
}
