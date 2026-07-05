//go:build !minimal

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"os"
	"strings"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/heic"
	"github.com/gen2brain/jpegn"
	"github.com/gen2brain/jpegxl"
	"github.com/gen2brain/webp"
	"github.com/pbnjay/pixfont"
)

const (
	overlayMargin = 6
	overlayPad    = 4
	overlayLineH  = 10
)

// toggleInfo flips the metadata overlay and rebuilds the current frame.
func (v *view) toggleInfo() {
	v.showInfo = !v.showInfo
	v.rebuild()
}

// overlayed draws the metadata overlay on the window letterbox, or returns img unchanged when disabled.
func (v *view) overlayed(img *image.RGBA) *image.RGBA {
	if !v.showInfo || len(v.args) == 0 {
		return img
	}

	lines := v.infoLines(v.args[v.idx])
	if len(lines) == 0 {
		return img
	}

	ib := img.Bounds()
	w, h := v.width, v.height
	if w < ib.Dx() || h < ib.Dy() {
		w, h = ib.Dx(), ib.Dy()
	}

	canvas := image.Rect(0, 0, w, h)
	if v.scratch == nil || v.scratch.Bounds() != canvas {
		v.scratch = image.NewRGBA(canvas)
	}
	out := v.scratch

	bg := color.RGBA{A: 0xff}
	fmt.Sscanf(v.opts.BackgroundColor, "#%02x%02x%02x", &bg.R, &bg.G, &bg.B)
	draw.Draw(out, canvas, image.NewUniform(bg), image.Point{}, draw.Src)

	ox := (w - ib.Dx()) / 2
	oy := (h - ib.Dy()) / 2
	draw.Draw(out, image.Rect(ox, oy, ox+ib.Dx(), oy+ib.Dy()), img, ib.Min, draw.Src)

	width := 0
	for _, s := range lines {
		if lw := pixfont.MeasureString(s); lw > width {
			width = lw
		}
	}

	boxW := width + 2*overlayPad
	boxH := len(lines)*overlayLineH + 2*overlayPad

	x0 := overlayMargin
	y0 := h - overlayMargin - boxH
	if y0 < 0 {
		y0 = 0
	}

	for y := y0; y < y0+boxH && y < h; y++ {
		for x := x0; x < x0+boxW && x < w; x++ {
			dim(out, x, y)
		}
	}

	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	for i, s := range lines {
		pixfont.DrawString(out, x0+overlayPad, y0+overlayPad+i*overlayLineH, s, white)
	}

	return out
}

// dim darkens the pixel at x,y so overlay text stays legible over any image.
func dim(img *image.RGBA, x, y int) {
	i := img.PixOffset(x, y)
	img.Pix[i] /= 3
	img.Pix[i+1] /= 3
	img.Pix[i+2] /= 3
}

// infoLines returns the cached EXIF overlay text for a; basics are omitted since the titlebar shows them.
func (v *view) infoLines(a info) []string {
	if lines, ok := v.infoCache[a.Name]; ok {
		return lines
	}

	lines := exifLines(a)
	if len(lines) == 0 {
		lines = []string{"No EXIF data"}
	}

	v.infoCache[a.Name] = lines

	return lines
}

type exifData struct {
	make     string
	model    string
	date     string
	exposure float64
	fnumber  float64
	iso      int
	focal    float64
	lat      float64
	lon      float64
}

// exifLines reads the EXIF of a and formats the notable fields.
func exifLines(a info) []string {
	r, closer := openInput(a)
	if r == nil {
		return nil
	}
	defer closer()

	ex, ok := fetchExif(a.Format, r)
	if !ok {
		return nil
	}

	var out []string
	if s := strings.TrimSpace(ex.make + " " + ex.model); s != "" {
		out = append(out, s)
	}
	if ex.date != "" {
		out = append(out, ex.date)
	}
	if ex.exposure > 0 {
		out = append(out, shutter(ex.exposure))
	}
	if ex.fnumber > 0 {
		out = append(out, fmt.Sprintf("f/%.1f", ex.fnumber))
	}
	if ex.iso > 0 {
		out = append(out, fmt.Sprintf("ISO %d", ex.iso))
	}
	if ex.focal > 0 {
		out = append(out, fmt.Sprintf("%.0f mm", ex.focal))
	}
	if ex.lat != 0 || ex.lon != 0 {
		out = append(out, fmt.Sprintf("%.5f, %.5f", ex.lat, ex.lon))
	}

	return out
}

// shutter formats a shutter time in seconds as a fraction for sub-second speeds.
func shutter(t float64) string {
	if t >= 1 {
		return fmt.Sprintf("%.1fs", t)
	}

	return fmt.Sprintf("1/%.0fs", 1/t)
}

func openInput(a info) (io.Reader, func()) {
	if a.IsURL {
		b, err := fetchURL(a.Name)
		if err != nil {
			return nil, func() {}
		}

		return bytes.NewReader(b), func() {}
	}

	f, err := os.Open(a.Name)
	if err != nil {
		return nil, func() {}
	}

	return f, func() { _ = f.Close() }
}

func fetchExif(format string, r io.Reader) (exifData, bool) {
	switch format {
	case "JPEG":
		if e, err := jpegn.DecodeExif(r); err == nil {
			return exifData{e.Make, e.Model, e.DateTimeOriginal, e.ExposureTime, e.FNumber, e.ISOSpeed, e.FocalLength, e.GPSLatitude, e.GPSLongitude}, true
		}
	case "WEBP":
		if e, err := webp.DecodeExif(r); err == nil {
			return exifData{e.Make, e.Model, e.DateTimeOriginal, e.ExposureTime, e.FNumber, e.ISOSpeed, e.FocalLength, e.GPSLatitude, e.GPSLongitude}, true
		}
	case "AVIF", "AVIFS":
		if e, err := avif.DecodeExif(r); err == nil {
			return exifData{e.Make, e.Model, e.DateTimeOriginal, e.ExposureTime, e.FNumber, e.ISOSpeed, e.FocalLength, e.GPSLatitude, e.GPSLongitude}, true
		}
	case "JXL":
		if e, err := jpegxl.DecodeExif(r); err == nil {
			return exifData{e.Make, e.Model, e.DateTimeOriginal, e.ExposureTime, e.FNumber, e.ISOSpeed, e.FocalLength, e.GPSLatitude, e.GPSLongitude}, true
		}
	case "HEIC":
		if e, err := heic.DecodeExif(r); err == nil {
			return exifData{e.Make, e.Model, e.DateTimeOriginal, e.ExposureTime, e.FNumber, e.ISOSpeed, e.FocalLength, e.GPSLatitude, e.GPSLongitude}, true
		}
	}

	return exifData{}, false
}
