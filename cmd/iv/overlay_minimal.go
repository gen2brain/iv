//go:build minimal

package main

import "image"

func (v *view) toggleInfo() {}

func (v *view) drawOverlay(img *image.RGBA) *image.RGBA { return img }
