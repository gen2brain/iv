package main

import (
	"bytes"
	"image/png"
	"path/filepath"

	"golang.design/x/clipboard"
)

func (v *view) copyPath() {
	if !v.clip {
		stderr("clipboard unavailable")
		return
	}

	if len(v.args) == 0 {
		return
	}

	a := v.args[v.idx]
	p := a.Name
	if !a.IsURL {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
	}

	clipboard.Write(clipboard.FmtText, []byte(p))
}

func (v *view) copyImage() {
	if !v.clip {
		stderr("clipboard unavailable")
		return
	}

	frame := v.frame
	if frame >= len(v.origs) {
		frame = 0
	}

	if frame >= len(v.origs) {
		return
	}

	var b bytes.Buffer
	if err := png.Encode(&b, v.origs[frame]); err != nil {
		stderr(err)
		return
	}

	clipboard.Write(clipboard.FmtImage, b.Bytes())
}
