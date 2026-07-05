package codec

import (
	"errors"
	"image"
	"image/color"
	"io"
	"strconv"
	"strings"
)

func init() {
	image.RegisterFormat("xbm", "#define ", decodeXBM, decodeXBMConfig)
}

var errXBM = errors.New("xbm: invalid data")

// xbmDim returns the integer following the first `#define ..._<suffix>` line.
func xbmDim(s, suffix string) int {
	for _, line := range strings.Split(s, "\n") {
		if !strings.Contains(line, suffix) {
			continue
		}
		if fields := strings.Fields(line); len(fields) > 0 {
			if n, err := strconv.Atoi(fields[len(fields)-1]); err == nil {
				return n
			}
		}
	}

	return 0
}

// xbmBits parses the comma-separated byte array between the braces.
func xbmBits(s string) []byte {
	open := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if open < 0 || end < open {
		return nil
	}

	var bits []byte
	for _, tok := range strings.Split(s[open+1:end], ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if v, err := strconv.ParseUint(tok, 0, 8); err == nil {
			bits = append(bits, byte(v))
		}
	}

	return bits
}

func decodeXBM(r io.Reader) (image.Image, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	s := string(data)
	w, h := xbmDim(s, "_width"), xbmDim(s, "_height")
	if w <= 0 || h <= 0 {
		return nil, errXBM
	}

	bits := xbmBits(s)
	stride := (w + 7) / 8
	if len(bits) < stride*h {
		return nil, errXBM
	}

	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			g := color.Gray{Y: 0xff}
			if bits[y*stride+x/8]&(1<<(uint(x)%8)) != 0 {
				g.Y = 0x00
			}
			img.SetGray(x, y, g)
		}
	}

	return img, nil
}

func decodeXBMConfig(r io.Reader) (image.Config, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return image.Config{}, err
	}

	s := string(data)
	w, h := xbmDim(s, "_width"), xbmDim(s, "_height")
	if w <= 0 || h <= 0 {
		return image.Config{}, errXBM
	}

	return image.Config{ColorModel: color.GrayModel, Width: w, Height: h}, nil
}
