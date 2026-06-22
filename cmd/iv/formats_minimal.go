//go:build minimal

package main

import (
	"bytes"
	"image"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"time"
)

var (
	formats        = []string{".jpg", ".jpeg", ".png", ".gif"}
	animations     = []string{".gif"}
	formatsMime    = []string{"image/jpeg", "image/png", "image/gif"}
	animationsMime = []string{"image/gif"}
)

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
	}

	return images, delay, nil
}
