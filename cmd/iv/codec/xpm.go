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
	image.RegisterFormat("xpm", "/* XPM */", decodeXPM, decodeXPMConfig)
}

var errXPM = errors.New("xpm: invalid data")

func xpmStrings(data []byte) []string {
	var out []string
	s := string(data)
	for {
		i := strings.IndexByte(s, '"')
		if i < 0 {
			break
		}
		s = s[i+1:]
		j := strings.IndexByte(s, '"')
		if j < 0 {
			break
		}
		out = append(out, s[:j])
		s = s[j+1:]
	}

	return out
}

// xpmHeader finds the "W H NCOLORS CPP" literal, tolerating a leading colormap array.
func xpmHeader(lits []string) (idx, w, h, ncolors, cpp int, err error) {
	for i, lit := range lits {
		f := strings.Fields(lit)
		if len(f) < 4 {
			continue
		}

		var v [4]int
		var ok = true
		for k := 0; k < 4; k++ {
			if v[k], err = strconv.Atoi(f[k]); err != nil {
				ok = false
				break
			}
		}
		if !ok || v[0] <= 0 || v[1] <= 0 || v[2] <= 0 || v[3] <= 0 {
			continue
		}

		return i, v[0], v[1], v[2], v[3], nil
	}

	return 0, 0, 0, 0, 0, errXPM
}

func decodeXPM(r io.Reader) (image.Image, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	lits := xpmStrings(data)
	idx, w, h, ncolors, cpp, err := xpmHeader(lits)
	if err != nil {
		return nil, err
	}
	if len(lits) < idx+1+ncolors+h {
		return nil, errXPM
	}

	palette := make(map[string]color.RGBA, ncolors)
	for i := 0; i < ncolors; i++ {
		lit := lits[idx+1+i]
		if len(lit) < cpp {
			return nil, errXPM
		}
		palette[lit[:cpp]] = xpmColor(lit[cpp:])
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row := lits[idx+1+ncolors+y]
		for x := 0; x < w; x++ {
			off := x * cpp
			if off+cpp > len(row) {
				break
			}
			img.SetRGBA(x, y, palette[row[off:off+cpp]])
		}
	}

	return img, nil
}

func decodeXPMConfig(r io.Reader) (image.Config, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return image.Config{}, err
	}

	_, w, h, _, _, err := xpmHeader(xpmStrings(data))
	if err != nil {
		return image.Config{}, err
	}

	return image.Config{ColorModel: color.RGBAModel, Width: w, Height: h}, nil
}

var xpmKeys = map[string]bool{"m": true, "g": true, "g4": true, "c": true, "s": true}

// xpmColor picks the visual (c) color from a "key value ..." color spec.
func xpmColor(spec string) color.RGBA {
	toks := strings.Fields(spec)

	var name []string
	for i := 0; i < len(toks); {
		if !xpmKeys[toks[i]] {
			i++
			continue
		}

		kt := toks[i]
		i++
		var val []string
		for i < len(toks) && !xpmKeys[toks[i]] {
			val = append(val, toks[i])
			i++
		}
		if kt == "c" {
			name = val
		}
	}

	return xpmParseColor(strings.Join(name, " "))
}

func xpmParseColor(name string) color.RGBA {
	if name == "" {
		return color.RGBA{}
	}

	n := strings.ToLower(name)
	if n == "none" {
		return color.RGBA{}
	}
	if strings.HasPrefix(name, "#") {
		return xpmHex(name[1:])
	}

	key := strings.ReplaceAll(n, " ", "")
	if c, ok := xpmNames[key]; ok {
		return c
	}
	if c, ok := xpmGray(key); ok {
		return c
	}

	return color.RGBA{A: 0xff}
}

// xpmHex parses #RGB, #RRGGBB, #RRRGGGBBB, or #RRRRGGGGBBBB into 8-bit channels.
func xpmHex(h string) color.RGBA {
	if len(h) == 0 || len(h)%3 != 0 {
		return color.RGBA{A: 0xff}
	}

	n := len(h) / 3
	comp := func(s string) uint8 {
		v, err := strconv.ParseUint(s, 16, 32)
		if err != nil {
			return 0
		}
		switch n {
		case 1:
			return uint8(v) * 0x11
		case 2:
			return uint8(v)
		case 3:
			return uint8(v >> 4)
		default:
			return uint8(v >> 8)
		}
	}

	return color.RGBA{R: comp(h[:n]), G: comp(h[n : 2*n]), B: comp(h[2*n : 3*n]), A: 0xff}
}

// xpmGray resolves grayNN/greyNN (NN in 0..100) to a shade.
func xpmGray(key string) (color.RGBA, bool) {
	var pfx string
	switch {
	case strings.HasPrefix(key, "gray"):
		pfx = "gray"
	case strings.HasPrefix(key, "grey"):
		pfx = "grey"
	default:
		return color.RGBA{}, false
	}

	num := key[len(pfx):]
	if num == "" {
		return color.RGBA{}, false
	}

	p, err := strconv.Atoi(num)
	if err != nil || p < 0 || p > 100 {
		return color.RGBA{}, false
	}

	v := uint8((p*255 + 50) / 100)

	return color.RGBA{R: v, G: v, B: v, A: 0xff}, true
}

// xpmNames is a curated subset of the X11 rgb.txt names common in real .xpm files.
var xpmNames = map[string]color.RGBA{
	"black":       {0, 0, 0, 0xff},
	"white":       {255, 255, 255, 0xff},
	"red":         {255, 0, 0, 0xff},
	"green":       {0, 255, 0, 0xff},
	"blue":        {0, 0, 255, 0xff},
	"cyan":        {0, 255, 255, 0xff},
	"magenta":     {255, 0, 255, 0xff},
	"yellow":      {255, 255, 0, 0xff},
	"gray":        {190, 190, 190, 0xff},
	"grey":        {190, 190, 190, 0xff},
	"dimgray":     {105, 105, 105, 0xff},
	"dimgrey":     {105, 105, 105, 0xff},
	"darkgray":    {169, 169, 169, 0xff},
	"darkgrey":    {169, 169, 169, 0xff},
	"lightgray":   {211, 211, 211, 0xff},
	"lightgrey":   {211, 211, 211, 0xff},
	"slategray":   {112, 128, 144, 0xff},
	"slategrey":   {112, 128, 144, 0xff},
	"orange":      {255, 165, 0, 0xff},
	"purple":      {160, 32, 240, 0xff},
	"brown":       {165, 42, 42, 0xff},
	"pink":        {255, 192, 203, 0xff},
	"navy":        {0, 0, 128, 0xff},
	"navyblue":    {0, 0, 128, 0xff},
	"maroon":      {176, 48, 96, 0xff},
	"gold":        {255, 215, 0, 0xff},
	"violet":      {238, 130, 238, 0xff},
	"indigo":      {75, 0, 130, 0xff},
	"turquoise":   {64, 224, 208, 0xff},
	"tan":         {210, 180, 140, 0xff},
	"beige":       {245, 245, 220, 0xff},
	"ivory":       {255, 255, 240, 0xff},
	"khaki":       {240, 230, 140, 0xff},
	"salmon":      {250, 128, 114, 0xff},
	"coral":       {255, 127, 80, 0xff},
	"crimson":     {220, 20, 60, 0xff},
	"orchid":      {218, 112, 214, 0xff},
	"plum":        {221, 160, 221, 0xff},
	"wheat":       {245, 222, 179, 0xff},
	"skyblue":     {135, 206, 235, 0xff},
	"lightblue":   {173, 216, 230, 0xff},
	"darkblue":    {0, 0, 139, 0xff},
	"lightgreen":  {144, 238, 144, 0xff},
	"darkgreen":   {0, 100, 0, 0xff},
	"lightyellow": {255, 255, 224, 0xff},
	"darkred":     {139, 0, 0, 0xff},
	"forestgreen": {34, 139, 34, 0xff},
	"royalblue":   {65, 105, 225, 0xff},
	"steelblue":   {70, 130, 180, 0xff},
}
