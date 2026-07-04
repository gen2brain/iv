//go:build minimal

package main

import "image"

func (v *view) toggleInfo() {}

func (v *view) overlayed(img *image.RGBA) *image.RGBA { return img }
