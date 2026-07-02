//go:build linux

package iv

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"os"
	"strings"

	"github.com/pbnjay/pixfont"
)

// View type.
type View struct {
	viewer Viewer
}

// New returns new View.
func New(args ...any) (*View, error) {
	var err error
	v := &View{}

	var opts Options
	if len(args) > 0 {
		val, ok := args[0].(Options)
		if !ok {
			return v, fmt.Errorf("invalid argument: %v", args[0])
		}

		opts = val
		opts.validate()
	} else {
		opts.defaults()
	}

	var session string
	if os.Getenv("XDG_SESSION_TYPE") == "wayland" || os.Getenv("WAYLAND_DISPLAY") != "" {
		session = "wayland"
	} else if os.Getenv("DISPLAY") != "" {
		session = "x11"
	} else if os.Getenv("TERM") == "linux" {
		session = "drm"
	}

	if os.Getenv("IV_DRIVER") != "" {
		session = strings.ToLower(os.Getenv("IV_DRIVER"))
	}

	switch session {
	case "x11":
		v.viewer, err = newX11(opts)
		if err != nil {
			return v, err
		}
	case "wayland":
		v.viewer, err = newWayland(opts)
		if err != nil {
			return v, err
		}
	case "drm":
		v.viewer, err = newDRM(opts)
		if err != nil {
			return v, err
		}
	default:
		v.viewer, err = newX11(opts)
		if err != nil {
			return v, err
		}
	}

	return v, nil
}

// Driver returns the name of the display driver.
func (v *View) Driver() string {
	return v.viewer.Driver()
}

// Display displays image, optional argument is the title of the window.
func (v *View) Display(ctx context.Context, img image.Image, args ...any) error {
	return v.viewer.Display(ctx, img, args...)
}

// ToggleFullscreen toggles window fullscreen.
func (v *View) ToggleFullscreen() error {
	return v.viewer.ToggleFullscreen()
}

// Fullscreen returns current fullscreen state.
func (v *View) Fullscreen() bool {
	return v.viewer.Fullscreen()
}

// Maximize maximizes the window to the available work area.
func (v *View) Maximize() error {
	return v.viewer.Maximize()
}

// SetCursor sets the window cursor shape.
func (v *View) SetCursor(c Cursor) error {
	return v.viewer.SetCursor(c)
}

// SetTitle sets window title.
func (v *View) SetTitle(title string) error {
	return v.viewer.SetTitle(title)
}

// ScreenSize returns screen dimensions.
func (v *View) ScreenSize() (int, int) {
	return v.viewer.ScreenSize()
}

// WindowSize returns window dimensions.
func (v *View) WindowSize() (int, int) {
	return v.viewer.WindowSize()
}

// Close closes the window.
func (v *View) Close() error {
	return v.viewer.Close()
}

// Clear clears the window contents.
func (v *View) Clear() {
	v.viewer.Clear()
}

// SetKeyPressHandler sets key press handler.
func (v *View) SetKeyPressHandler(handler KeyPressHandler) {
	v.viewer.SetKeyPressHandler(handler)
}

// SetKeyReleaseHandler sets key release handler.
func (v *View) SetKeyReleaseHandler(handler KeyReleaseHandler) {
	v.viewer.SetKeyReleaseHandler(handler)
}

// SetButtonPressHandler sets mouse button press handler.
func (v *View) SetButtonPressHandler(handler ButtonPressHandler) {
	v.viewer.SetButtonPressHandler(handler)
}

// SetButtonReleaseHandler sets mouse button release handler.
func (v *View) SetButtonReleaseHandler(handler ButtonReleaseHandler) {
	v.viewer.SetButtonReleaseHandler(handler)
}

// SetMotionHandler sets mouse motion handler.
func (v *View) SetMotionHandler(handler MotionHandler) {
	v.viewer.SetMotionHandler(handler)
}

// SetScrollHandler sets mouse scroll handler.
func (v *View) SetScrollHandler(handler ScrollHandler) {
	v.viewer.SetScrollHandler(handler)
}

// SetEnterHandler sets mouse enter handler.
func (v *View) SetEnterHandler(handler EnterHandler) {
	v.viewer.SetEnterHandler(handler)
}

// SetLeaveHandler sets mouse leave handler.
func (v *View) SetLeaveHandler(handler LeaveHandler) {
	v.viewer.SetLeaveHandler(handler)
}

// SetResizeHandler sets window resize handler.
func (v *View) SetResizeHandler(handler ResizeHandler) {
	v.viewer.SetResizeHandler(handler)
}

// SetCreatedHandler sets window created handler.
func (v *View) SetCreatedHandler(handler CreatedHandler) {
	v.viewer.SetCreatedHandler(handler)
}

// SetClosedHandler sets window closed handler.
func (v *View) SetClosedHandler(handler ClosedHandler) {
	v.viewer.SetClosedHandler(handler)
}

// SetIcon sets the window icon.
func (v *View) SetIcon(img image.Image) error {
	return v.viewer.SetIcon(img)
}

// Raise brings the window to the front.
func (v *View) Raise() error {
	return v.viewer.Raise()
}

const titleMargin = 10

// titleFontHeight is the height of the 8x8 pixfont used for the drawn title.
const titleFontHeight = 8

// drawTitleBar draws title onto c with a dimmed background box so it stays legible over any image.
func drawTitleBar(c *wlCanvas, title string, textColor color.Color) {
	elided := elideTitle(title, c.w-2*titleMargin)
	if elided == "" {
		return
	}

	const pad = 3
	tw := pixfont.MeasureString(elided)

	dimRect(c, titleMargin-pad, titleMargin-pad, titleMargin+tw+pad, titleMargin+titleFontHeight+pad)
	pixfont.DrawString(c, titleMargin, titleMargin, elided, textColor)
}

// dimRect darkens the color channels of the rectangle on the canvas buffer, leaving alpha.
func dimRect(c *wlCanvas, x0, y0, x1, y1 int) {
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > c.w {
		x1 = c.w
	}
	if y1 > c.h {
		y1 = c.h
	}

	for y := y0; y < y1; y++ {
		row := y * c.stride
		for x := x0; x < x1; x++ {
			o := row + x*4
			c.data[o] /= 3
			c.data[o+1] /= 3
			c.data[o+2] /= 3
		}
	}
}

func elideTitle(title string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if pixfont.MeasureString(title) <= maxWidth {
		return title
	}

	const ellipsis = "..."
	ellipsisWidth := pixfont.MeasureString(ellipsis)
	if maxWidth <= ellipsisWidth {
		return ""
	}

	r := []rune(title)
	budget := maxWidth - ellipsisWidth

	lo, hi := 0, len(r)
	var lw, rw int
	fromLeft := true
	for lo < hi {
		if fromLeft {
			w := pixfont.MeasureString(string(r[lo]))
			if lw+w+rw > budget {
				break
			}
			lw += w
			lo++
		} else {
			w := pixfont.MeasureString(string(r[hi-1]))
			if lw+rw+w > budget {
				break
			}
			rw += w
			hi--
		}
		fromLeft = !fromLeft
	}

	return string(r[:lo]) + ellipsis + string(r[hi:])
}
