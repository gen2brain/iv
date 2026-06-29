// Package iv.
package iv

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"runtime"
)

// Cursor is a window cursor shape. Backends map each to the closest native cursor.
type Cursor int

const (
	// CursorDefault is the arrow cursor.
	CursorDefault Cursor = iota
	// CursorPointer is the pointing hand, used for links.
	CursorPointer
	// CursorGrab is the open hand.
	CursorGrab
	// CursorGrabbing is the closed hand, shown while dragging.
	CursorGrabbing
	// CursorText is the text I-beam.
	CursorText
	// CursorCrosshair is the crosshair.
	CursorCrosshair
)

// Viewer interface.
type Viewer interface {
	Driver() string
	Display(ctx context.Context, img image.Image, args ...any) error
	ToggleFullscreen() error
	Fullscreen() bool
	Maximize() error
	Raise() error
	SetTitle(string) error
	SetIcon(image.Image) error
	SetCursor(Cursor) error
	ScreenSize() (int, int)
	WindowSize() (int, int)
	Close() error
	Clear()

	SetKeyPressHandler(handler KeyPressHandler)
	SetKeyReleaseHandler(handler KeyReleaseHandler)
	SetButtonPressHandler(handler ButtonPressHandler)
	SetButtonReleaseHandler(handler ButtonReleaseHandler)
	SetMotionHandler(handler MotionHandler)
	SetScrollHandler(handler ScrollHandler)
	SetEnterHandler(handler EnterHandler)
	SetLeaveHandler(handler LeaveHandler)
	SetResizeHandler(handler ResizeHandler)
	SetCreatedHandler(handler CreatedHandler)
	SetClosedHandler(handler ClosedHandler)
}

// KeyPressHandler function.
type KeyPressHandler func(key int)

// KeyReleaseHandler function.
type KeyReleaseHandler func(key int)

// ButtonPressHandler function.
type ButtonPressHandler func(button int)

// ButtonReleaseHandler function.
type ButtonReleaseHandler func(button int)

// MotionHandler function.
type MotionHandler func(x, y int)

// ScrollHandler function.
type ScrollHandler func(direction int)

// EnterHandler function.
type EnterHandler func()

// LeaveHandler function.
type LeaveHandler func()

// ResizeHandler function.
type ResizeHandler func(width, height int)

// CreatedHandler function.
type CreatedHandler func()

// ClosedHandler function.
type ClosedHandler func()

// Options type.
type Options struct {
	// AppID e.g. class name
	AppID string
	// Window width
	Width int
	// Window height
	Height int
	// DRM device index
	Device int
	// Text color (HEX)
	TextColor string
	// Window background color (HEX)
	BackgroundColor string
}

func (o *Options) defaults() {
	o.AppID = "iv"
	o.Width = 1024
	o.Height = 768
	o.TextColor = "#FFFFFF"
	o.BackgroundColor = "#000000"
}

func (o *Options) validate() {
	if o.AppID == "" {
		o.AppID = "iv"
	}

	if o.Width == 0 || o.Height == 0 {
		o.Width = 1024
		o.Height = 768
	}

	if o.TextColor == "" {
		o.TextColor = "#FFFFFF"
	}

	if o.BackgroundColor == "" {
		o.BackgroundColor = "#000000"
	}
}

var (
	// ErrUnsupported is returned when operating system is not supported.
	ErrUnsupported = fmt.Errorf("unsupported os: %s", runtime.GOOS)
)

func imageToRGBA(src image.Image) *image.RGBA {
	if dst, ok := src.(*image.RGBA); ok {
		b := dst.Bounds()
		if b.Min.X == 0 && b.Min.Y == 0 && dst.Stride == b.Dx()*4 {
			return dst
		}
	}

	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)

	return dst
}

func parseHexColor(s string) (c color.RGBA, err error) {
	c.A = 0xff
	_, err = fmt.Sscanf(s, "#%02x%02x%02x", &c.R, &c.G, &c.B)

	return
}
