// Package iv.
package iv

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"runtime"

	"github.com/gen2brain/iv/internal/swizzle"
	"github.com/gen2brain/iv/internal/ycbcr"
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
	Present(ctx context.Context, img image.Image) error
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

// placeImage centers an iw x ih image in the area from row top to h of a w-wide buffer, clipped to it.
func placeImage(w, h, top, iw, ih int) image.Rectangle {
	dx := max((w-iw)/2, 0)
	dy := max(top+(h-top-ih)/2, top)

	return image.Rect(dx, dy, min(dx+iw, w), min(dy+ih, h))
}

// blitImage copies src into r of a w-wide buffer.
func blitImage(dst []byte, w int, r image.Rectangle, src image.Image, bgra bool) {
	switch s := src.(type) {
	case *image.RGBA:
		blitRGBA(dst, w, r, s, bgra)
	case *image.YCbCr:
		blitYCbCr(dst, w, r, s, bgra)
	default:
		blitRGBA(dst, w, r, imageToRGBA(src), bgra)
	}
}

func blitRGBA(dst []byte, w int, r image.Rectangle, src *image.RGBA, bgra bool) {
	n := r.Dx() * 4
	for y := 0; y < r.Dy(); y++ {
		so := src.PixOffset(src.Rect.Min.X, src.Rect.Min.Y+y)
		do := (r.Min.Y+y)*w*4 + r.Min.X*4
		if bgra {
			_ = swizzle.CopyBGRA(dst[do:do+n], src.Pix[so:so+n])
		} else {
			copy(dst[do:do+n], src.Pix[so:so+n])
		}
	}
}

func blitYCbCr(dst []byte, w int, r image.Rectangle, src *image.YCbCr, bgra bool) {
	n := r.Dx()
	x0 := src.Rect.Min.X
	for y := 0; y < r.Dy(); y++ {
		sy := src.Rect.Min.Y + y
		do := (r.Min.Y+y)*w*4 + r.Min.X*4
		row := dst[do : do+n*4]
		yi := src.YOffset(x0, sy)
		ys := src.Y[yi : yi+n]

		switch src.SubsampleRatio {
		case image.YCbCrSubsampleRatio444, image.YCbCrSubsampleRatio440:
			ci := src.COffset(x0, sy)
			ycbcr.Row(row, ys, src.Cb[ci:ci+n], src.Cr[ci:ci+n], bgra)
		case image.YCbCrSubsampleRatio422, image.YCbCrSubsampleRatio420:
			i := 0
			if x0&1 != 0 && n > 0 {
				ci := src.COffset(x0, sy)
				ycbcr.Row(row[:4], ys[:1], src.Cb[ci:ci+1], src.Cr[ci:ci+1], bgra)
				i = 1
			}
			if m := n - i; m > 0 {
				ci := src.COffset(x0+i, sy)
				c := (m + 1) / 2
				ycbcr.Row2(row[4*i:], ys[i:], src.Cb[ci:ci+c], src.Cr[ci:ci+c], bgra)
			}
		default:
			for i := 0; i < n; i++ {
				ci := src.COffset(x0+i, sy)
				ycbcr.Row(row[4*i:4*i+4], ys[i:i+1], src.Cb[ci:ci+1], src.Cr[ci:ci+1], bgra)
			}
		}
	}
}

// fillPixels fills a 32-bit buffer with px.
func fillPixels(data []byte, px [4]byte) {
	if len(data) == 0 {
		return
	}

	copy(data, px[:])
	for f := 4; f < len(data); f *= 2 {
		copy(data[f:], data[:f])
	}
}

// fillAround fills a w x h 32-bit buffer with px everywhere outside r.
func fillAround(data []byte, w, h int, r image.Rectangle, px [4]byte) {
	stride := w * 4
	if r.Empty() {
		fillPixels(data[:h*stride], px)
		return
	}

	fillPixels(data[:r.Min.Y*stride], px)
	fillPixels(data[r.Max.Y*stride:h*stride], px)

	left, right := r.Min.X*4, r.Max.X*4
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := data[y*stride : (y+1)*stride]
		fillPixels(row[:left], px)
		fillPixels(row[right:], px)
	}
}

func parseHexColor(s string) (c color.RGBA, err error) {
	c.A = 0xff
	_, err = fmt.Sscanf(s, "#%02x%02x%02x", &c.R, &c.G, &c.B)

	return
}
