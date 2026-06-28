//go:build !minimal

package main

import (
	"bytes"
	"image"
	"image/gif"
	//_ "image/jpeg"
	"io"
	"os"
	"time"

	_ "github.com/biessek/golang-ico"
	_ "github.com/donatj/mpo"
	"github.com/gen2brain/avif"
	_ "github.com/gen2brain/heic"
	"github.com/gen2brain/jpegn"
	"github.com/gen2brain/jpegxl"
	_ "github.com/gen2brain/svg"
	"github.com/gen2brain/webp"
	_ "github.com/jbuchbinder/gopnm"
	_ "github.com/jsummers/gobmp"
	"github.com/kettek/apng"
	_ "github.com/oov/psd"
	_ "github.com/samuel/go-pcx/pcx"
	_ "github.com/samuel/go-psp/psp"
	_ "github.com/xfmoulet/qoi"
	_ "golang.org/x/image/tiff"
)

var (
	formats = []string{".jpg", ".jpeg", ".png", ".apng", ".gif", ".bmp", ".webp", ".avif", ".avifs", ".jxl", ".heic",
		".ico", ".pcx", ".tif", ".tiff", ".pnm", ".pbm", ".pgm", ".ppm", ".svg", ".psd", ".psp", ".mpo", ".qoi"}

	animations = []string{".gif", ".png", ".apng", ".webp", ".avif", ".avifs", ".jxl"}

	formatsMime = []string{"image/jpeg", "image/png", "image/apng", "image/gif", "image/bmp", "image/webp", "image/avif",
		"image/avif-sequence", "image/jxl", "image/heic", "image/vnd.microsoft.icon", "image/x-pcx", "image/tiff",
		"image/x-portable-anymap", "image/x-portable-bitmap", "image/x-portable-graymap", "image/x-portable-pixmap",
		"image/svg+xml", "image/vnd.adobe.photoshop", "application/x-paintshoppro", "image/mpo", "image/x-qoi"}

	animationsMime = []string{"image/png", "image/apng", "image/gif", "image/webp", "image/avif", "image/avif-sequence", "image/jxl"}
)

// decodeImage decodes a single image, using format-specific options where supported, else image.Decode.
func decodeImage(format string, r io.Reader) (image.Image, error) {
	switch format {
	case "JPEG":
		return jpegn.Decode(r, &jpegn.Options{AutoRotate: true, ToRGBA: true})
	case "WEBP":
		return webp.Decode(r, webp.Options{AutoRotate: true})
	default:
		img, _, err := image.Decode(r)
		return img, err
	}
}

func decodeAll(fileInfo info) ([]image.Image, []time.Duration, error) {
	var err error
	var rc io.ReadCloser

	if fileInfo.IsURL {
		b, err := fetchURL(fileInfo.Name)
		if err != nil {
			return nil, nil, err
		}

		rc = io.NopCloser(bytes.NewReader(b))
	} else {
		rc, err = os.Open(fileInfo.Name)
		if err != nil {
			return nil, nil, err
		}
	}

	defer rc.Close()

	delay := make([]time.Duration, 0)
	images := make([]image.Image, 0)

	switch fileInfo.Format {
	case "GIF":
		ret, err := gif.DecodeAll(rc)
		if err != nil {
			return images, delay, err
		}

		images = append(images, ret.Image[0])
		for i := 1; i < len(ret.Image); i++ {
			img := restoreGIF(ret.Image[i], images[i-1], images[0].Bounds())
			images = append(images, img)
		}

		for _, d := range ret.Delay {
			delay = append(delay, time.Duration(d*10)*time.Millisecond)
		}
	case "APNG", "PNG":
		ret, err := apng.DecodeAll(rc)
		if err != nil {
			return images, delay, err
		}

		for _, frame := range ret.Frames {
			images = append(images, frame.Image)

			d := float64(frame.DelayNumerator) / float64(frame.DelayDenominator)
			delay = append(delay, time.Duration(d*float64(time.Second)))
		}
	case "WEBP":
		ret, err := webp.DecodeAll(rc, webp.Options{AutoRotate: true})
		if err != nil {
			return images, delay, err
		}

		images = ret.Image
		for _, d := range ret.Delay {
			delay = append(delay, time.Duration(d)*time.Millisecond)
		}
	case "AVIF", "AVIFS":
		ret, err := avif.DecodeAll(rc)
		if err != nil {
			return images, delay, err
		}

		images = ret.Image
		for _, d := range ret.Delay {
			delay = append(delay, time.Duration(d*float64(time.Second)))
		}
	case "JXL":
		ret, err := jpegxl.DecodeAll(rc)
		if err != nil {
			return images, delay, err
		}

		images = ret.Image
		for _, d := range ret.Delay {
			delay = append(delay, time.Duration(d*100)*time.Millisecond)
		}
	}

	return images, delay, nil
}
