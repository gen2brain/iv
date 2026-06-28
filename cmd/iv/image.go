package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/anthonynsimon/bild/clone"
	"github.com/anthonynsimon/bild/transform"
)

func decode(fileInfo info) (*image.RGBA, error) {
	var err error
	var rc io.ReadCloser
	var img image.Image

	if fileInfo.IsURL {
		b, err := fetchURL(fileInfo.Name)
		if err != nil {
			return nil, err
		}

		rc = io.NopCloser(bytes.NewReader(b))
	} else {
		rc, err = os.Open(fileInfo.Name)
		if err != nil {
			return nil, err
		}
	}

	defer rc.Close()

	img, err = decodeImage(fileInfo.Format, rc)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", fileInfo.Name, err)
	}

	return clone.AsShallowRGBA(img), nil
}

func resize(img image.Image, width, height int, filter transform.ResampleFilter) *image.RGBA {
	dstW, dstH := width, height

	srcW := img.Bounds().Dx()
	srcH := img.Bounds().Dy()

	if dstW == 0 {
		tmpW := float64(dstH) * float64(srcW) / float64(srcH)
		dstW = int(math.Max(1.0, math.Floor(tmpW+0.5)))
	}
	if dstH == 0 {
		tmpH := float64(dstW) * float64(srcH) / float64(srcW)
		dstH = int(math.Max(1.0, math.Floor(tmpH+0.5)))
	}

	if srcW == dstW && srcH == dstH {
		return clone.AsShallowRGBA(img)
	}

	return transform.Resize(img, dstW, dstH, filter)
}

func fit(img image.Image, width, height int, filter transform.ResampleFilter) *image.RGBA {
	maxW, maxH := width, height

	b := img.Bounds()
	srcW := b.Dx()
	srcH := b.Dy()

	if srcW <= maxW && srcH <= maxH {
		return clone.AsShallowRGBA(img)
	}

	srcAspectRatio := float64(srcW) / float64(srcH)
	maxAspectRatio := float64(maxW) / float64(maxH)

	var dstW, dstH int
	if srcAspectRatio > maxAspectRatio {
		dstW = maxW
		dstH = int(float64(dstW) / srcAspectRatio)
	} else {
		dstH = maxH
		dstW = int(float64(dstH) * srcAspectRatio)
	}

	return resize(img, dstW, dstH, filter)
}

func isImage(fileName string) bool {
	for _, format := range formats {
		if strings.ToLower(filepath.Ext(fileName)) == format {
			return true
		}
	}

	return false
}

func isAnimation(fileName string) bool {
	for _, animation := range animations {
		if strings.ToLower(filepath.Ext(fileName)) == animation {
			return true
		}
	}

	return false
}

func isInRect(x, y int, r image.Rectangle) bool {
	return r.Min.X <= x && x < r.Max.X && r.Min.Y <= y && y < r.Max.Y
}

func isTransparent(c color.Color) bool {
	_, _, _, a := c.RGBA()

	return a > 0
}

func restoreGIF(current image.Image, prev image.Image, rect image.Rectangle) *image.RGBA {
	img := image.NewRGBA(rect)

	for x := 0; x < rect.Dx(); x++ {
		for y := 0; y < rect.Dy(); y++ {
			if isInRect(x, y, current.Bounds()) && isTransparent(current.At(x, y)) {
				img.Set(x, y, current.At(x, y))
			} else {
				img.Set(x, y, prev.At(x, y))
			}
		}
	}

	return img
}
