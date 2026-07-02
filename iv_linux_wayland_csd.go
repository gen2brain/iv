//go:build linux

package iv

import (
	"image/color"

	"github.com/pbnjay/pixfont"
)

const (
	titleBarHeight = 30
	titleBtnWidth  = 44

	xdgStateMaximized = 1
)

var (
	barColor      = color.RGBA{0x2b, 0x2b, 0x2b, 0xff}
	barHover      = color.RGBA{0x45, 0x45, 0x45, 0xff}
	barCloseHover = color.RGBA{0xc0, 0x39, 0x2b, 0xff}
)

// hasTitleBar reports whether a client-side titlebar is drawn (CSD, windowed).
func (v *viewWayland) hasTitleBar() bool {
	return v.csd && !v.fullscreen
}

// contentTop is the y offset of the image content area below the titlebar.
func (v *viewWayland) contentTop() int {
	if v.hasTitleBar() {
		return titleBarHeight
	}
	return 0
}

// contentHeight is the height of the image content area, excluding the titlebar.
func (v *viewWayland) contentHeight() int {
	return v.winHeight - v.contentTop()
}

// fireClose marks the window closed and notifies the controller once.
func (v *viewWayland) fireClose() {
	v.running = false
	v.closed = true
	if v.closedHandler != nil && !v.closedFired {
		v.closedFired = true
		v.closedHandler()
	}
}

// drawWindowBar draws the titlebar with the title and minimize/maximize/close buttons.
func (v *viewWayland) drawWindowBar(data []byte, w int) {
	c := &wlCanvas{data: data, stride: w * 4, w: w, h: v.winHeight, useXBGR: v.useXBGR}

	fillRect(c, 0, 0, w, titleBarHeight, barColor)

	btnStart := w - 3*titleBtnWidth

	if v.title != "" {
		title := elideTitle(v.title, btnStart-2*titleMargin)
		tw := pixfont.MeasureString(title)
		tx := (btnStart - tw) / 2
		if tx < titleMargin {
			tx = titleMargin
		}
		pixfont.DrawString(c, tx, (titleBarHeight-titleFontHeight)/2, title, v.textColor)
	}

	for i := 0; i < 3; i++ {
		x0 := btnStart + i*titleBtnWidth
		if v.hoverBtn == i {
			hl := barHover
			if i == 2 {
				hl = barCloseHover
			}
			fillRect(c, x0, 0, x0+titleBtnWidth, titleBarHeight, hl)
		}
		v.drawGlyph(c, i, x0+titleBtnWidth/2, titleBarHeight/2)
	}
}

// drawGlyph draws the minimize (0), maximize/restore (1) or close (2) icon centered at cx,cy.
func (v *viewWayland) drawGlyph(c *wlCanvas, which, cx, cy int) {
	const r = 5
	clr := v.textColor

	switch which {
	case 0:
		for x := cx - r; x <= cx+r; x++ {
			c.Set(x, cy, clr)
			c.Set(x, cy+1, clr)
		}
	case 1:
		if v.maximized {
			drawSquare(c, cx-r+3, cy-r, cx+r, cy+r-3, clr)
			drawSquare(c, cx-r, cy-r+3, cx+r-3, cy+r, clr)
		} else {
			drawSquare(c, cx-r, cy-r, cx+r, cy+r, clr)
		}
	case 2:
		for i := -r; i <= r; i++ {
			c.Set(cx+i, cy+i, clr)
			c.Set(cx+i+1, cy+i, clr)
			c.Set(cx+i, cy-i, clr)
			c.Set(cx+i+1, cy-i, clr)
		}
	}
}

// titleBarButtonAt returns the button index (0/1/2) at x,y, or -1 when outside the buttons.
func (v *viewWayland) titleBarButtonAt(x, y int) int {
	if !v.hasTitleBar() || y < 0 || y >= titleBarHeight {
		return -1
	}

	start := v.winWidth - 3*titleBtnWidth
	if x < start || x >= v.winWidth {
		return -1
	}

	return (x - start) / titleBtnWidth
}

// pressTitleBar handles a left press in the titlebar: a button action, or an interactive move.
func (v *viewWayland) pressTitleBar(serial uint32) {
	switch v.titleBarButtonAt(v.ptrX, v.ptrY) {
	case 0:
		v.toplevel.SetMinimized()
	case 1:
		if v.maximized {
			v.toplevel.UnsetMaximized()
		} else {
			v.toplevel.SetMaximized()
		}
	case 2:
		v.fireClose()
	default:
		v.toplevel.Move(v.seat, serial)
	}
}

func fillRect(c *wlCanvas, x0, y0, x1, y1 int, clr color.Color) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c.Set(x, y, clr)
		}
	}
}

func drawSquare(c *wlCanvas, x0, y0, x1, y1 int, clr color.Color) {
	for x := x0; x <= x1; x++ {
		c.Set(x, y0, clr)
		c.Set(x, y1, clr)
	}
	for y := y0; y <= y1; y++ {
		c.Set(x0, y, clr)
		c.Set(x1, y, clr)
	}
}
