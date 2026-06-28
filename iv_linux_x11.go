//go:build linux || freebsd || netbsd || openbsd || dragonfly || x11

package iv

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"sync/atomic"

	"github.com/jezek/xgb"
	mshm "github.com/jezek/xgb/shm"
	"github.com/jezek/xgb/xproto"

	"github.com/gen2brain/shm"

	"github.com/gen2brain/iv/internal/swizzle"
)

type viewX11 struct {
	xc *xgb.Conn
	gc xproto.Gcontext

	window xproto.Window
	screen *xproto.ScreenInfo

	events chan xgb.Event

	shmId   int
	shmSeg  mshm.Seg
	shmData []byte
	useShm  bool

	bg         color.RGBA
	buf        []byte
	bufW, bufH int

	keyPressHandler      KeyPressHandler
	keyReleaseHandler    KeyReleaseHandler
	buttonPressHandler   ButtonPressHandler
	buttonReleaseHandler ButtonReleaseHandler
	motionHandler        MotionHandler
	scrollHandler        ScrollHandler
	enterHandler         EnterHandler
	leaveHandler         LeaveHandler
	resizeHandler        ResizeHandler
	createdHandler       CreatedHandler
	closedHandler        ClosedHandler

	winWidth, winHeight       int
	screenWidth, screenHeight int

	fullscreen bool
	running    bool

	connected atomic.Bool
}

func newX11(opts Options) (*viewX11, error) {
	v := &viewX11{}

	var err error
	xgb.Logger = log.New(io.Discard, "", 0)

	v.xc, err = xgb.NewConn()
	if err != nil {
		return v, fmt.Errorf("cannot create connection: %w", err)
	}

	v.useShm = true
	if err := mshm.Init(v.xc); err != nil {
		v.useShm = false
	}

	v.connected.Store(true)

	v.screen = xproto.Setup(v.xc).DefaultScreen(v.xc)
	v.window, err = xproto.NewWindowId(v.xc)
	if err != nil {
		return v, fmt.Errorf("cannot create window id: %w", err)
	}

	v.winWidth = opts.Width
	v.winHeight = opts.Height

	v.screenWidth = int(v.screen.WidthInPixels)
	v.screenHeight = int(v.screen.HeightInPixels)

	col, err := parseHexColor(opts.BackgroundColor)
	if err != nil {
		return v, fmt.Errorf("cannot parse color: %w", err)
	}
	v.bg = col
	backPixel := uint32(col.A)<<24 + uint32(col.R)<<16 + uint32(col.G)<<8 + uint32(col.B)

	x := int16(int(v.screenWidth)/2 - v.winWidth/2)
	y := int16(int(v.screenHeight)/2 - v.winHeight/2)

	err = xproto.CreateWindowChecked(v.xc, v.screen.RootDepth, v.window, v.screen.Root,
		x, y, uint16(opts.Width), uint16(opts.Height), 0,
		xproto.WindowClassInputOutput, v.screen.RootVisual,
		xproto.CwBackPixel|xproto.CwEventMask,
		[]uint32{
			backPixel,
			xproto.EventMaskKeyPress | xproto.EventMaskKeyRelease | xproto.EventMaskButtonPress | xproto.EventMaskButtonRelease |
				xproto.EventMaskPointerMotion | xproto.EventMaskEnterWindow | xproto.EventMaskLeaveWindow |
				xproto.EventMaskExposure | xproto.EventMaskStructureNotify,
		},
	).Check()
	if err != nil {
		return v, fmt.Errorf("cannot create window: %w", err)
	}

	v.gc, err = xproto.NewGcontextId(v.xc)
	if err != nil {
		return v, fmt.Errorf("cannot create gc id: %w", err)
	}

	if err := xproto.CreateGCChecked(v.xc, v.gc, xproto.Drawable(v.window), 0, nil).Check(); err != nil {
		return v, fmt.Errorf("cannot create gc: %w", err)
	}

	err = v.setClass(opts.AppID)
	if err != nil {
		return v, fmt.Errorf("cannot set class: %w", err)
	}

	err = v.setNormalHints()
	if err != nil {
		return v, fmt.Errorf("cannot set normal hints: %w", err)
	}

	err = v.SetTitle(" ")
	if err != nil {
		return v, fmt.Errorf("cannot set title: %w", err)
	}

	v.events = make(chan xgb.Event)

	go func(conn *xgb.Conn) {
		for {
			if !v.connected.Load() {
				return
			}

			ev, err := conn.WaitForEvent()
			if err == nil && ev == nil {
				event := xproto.DestroyNotifyEvent{Event: xproto.DestroyNotify, Window: v.window}
				v.events <- event

				return
			}

			if err == nil {
				v.events <- ev
			}
		}
	}(v.xc)

	xproto.MapWindow(v.xc, v.window)

	return v, nil
}

func (v *viewX11) Driver() string {
	return "x11"
}

func (v *viewX11) Display(ctx context.Context, img image.Image, args ...any) error {
	if len(args) > 0 {
		title, ok := args[0].(string)
		if !ok {
			return fmt.Errorf("invalid argument: %v", args[0])
		}

		if title != "" {
			err := v.SetTitle(title)
			if err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}

				return fmt.Errorf("cannot set title: %w", err)
			}
		}
	}

	if v.useShm {
		if err := v.ensureBuffer(); err != nil {
			return fmt.Errorf("cannot create image: %w", err)
		}
		v.compose(v.shmData, img)
	}

	event := xproto.ExposeEvent{Window: v.window, Width: uint16(v.winWidth), Height: uint16(v.winHeight)}
	xproto.SendEvent(v.xc, false, v.window, xproto.EventMaskExposure, string(event.Bytes()))

	if v.running {
		return nil
	}

	v.running = true

	defer func() {
		v.running = false
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-v.events:
			if ctx.Err() != nil {
				return nil
			}

			switch e := ev.(type) {
			case xproto.ConfigureNotifyEvent:
				if int(e.Width) != v.winWidth || int(e.Height) != v.winHeight {
					v.winWidth = int(e.Width)
					v.winHeight = int(e.Height)

					if v.resizeHandler != nil {
						v.resizeHandler(v.winWidth, v.winHeight)
					}
				}
			case xproto.KeyPressEvent:
				if v.keyPressHandler != nil {
					v.keyPressHandler(int(e.Detail))
				}
			case xproto.KeyReleaseEvent:
				if v.keyReleaseHandler != nil {
					v.keyReleaseHandler(int(e.Detail))
				}
			case xproto.ButtonPressEvent:
				if int(e.Detail) == ScrollUp || int(e.Detail) == ScrollDown {
					if v.scrollHandler != nil {
						v.scrollHandler(int(e.Detail))
					}
				} else {
					if v.buttonPressHandler != nil {
						v.buttonPressHandler(int(e.Detail))
					}
				}
			case xproto.ButtonReleaseEvent:
				if v.buttonReleaseHandler != nil {
					v.buttonReleaseHandler(int(e.Detail))
				}
			case xproto.MotionNotifyEvent:
				if v.motionHandler != nil {
					v.motionHandler(int(e.EventX), int(e.EventY))
				}
			case xproto.EnterNotifyEvent:
				if v.enterHandler != nil {
					v.enterHandler()
				}
			case xproto.LeaveNotifyEvent:
				if v.leaveHandler != nil {
					v.leaveHandler()
				}
			case xproto.MapNotifyEvent:
				if v.createdHandler != nil {
					v.createdHandler()
				}
			case xproto.ExposeEvent:
				if e.Count == 0 {
					if v.useShm {
						v.imageShmPut()
					} else {
						v.imagePut(img)
					}
				}
			case xproto.DestroyNotifyEvent:
				if v.closedHandler != nil {
					v.closedHandler()
				}

				return nil
			}
		}
	}
}

func (v *viewX11) ToggleFullscreen() error {
	name := "_NET_WM_STATE"
	atomState, err := xproto.InternAtom(v.xc, false, uint16(len(name)), name).Reply()
	if err != nil {
		return err
	}

	name = "_NET_WM_STATE_FULLSCREEN"
	atomStateFullscreen, err := xproto.InternAtom(v.xc, false, uint16(len(name)), name).Reply()
	if err != nil {
		return err
	}

	data := make([]uint32, 5)
	data[0] = uint32(2) // _NET_WM_STATE_TOGGLE
	data[1] = uint32(atomStateFullscreen.Atom)

	ev := &xproto.ClientMessageEvent{
		Format: 32,
		Window: v.window,
		Type:   atomState.Atom,
		Data:   xproto.ClientMessageDataUnionData32New(data),
	}

	evMask := xproto.EventMaskSubstructureNotify | xproto.EventMaskSubstructureRedirect
	if err := xproto.SendEventChecked(v.xc, false, v.screen.Root, uint32(evMask), string(ev.Bytes())).Check(); err != nil {
		return err
	}

	v.fullscreen = !v.fullscreen

	return nil
}

func (v *viewX11) Fullscreen() bool {
	return v.fullscreen
}

func (v *viewX11) Raise() error {
	name := "_NET_ACTIVE_WINDOW"
	atom, err := xproto.InternAtom(v.xc, false, uint16(len(name)), name).Reply()
	if err != nil {
		return err
	}

	data := make([]uint32, 5)
	data[0] = uint32(1)

	ev := &xproto.ClientMessageEvent{
		Format: 32,
		Window: v.window,
		Type:   atom.Atom,
		Data:   xproto.ClientMessageDataUnionData32New(data),
	}

	evMask := xproto.EventMaskSubstructureNotify | xproto.EventMaskSubstructureRedirect
	if err := xproto.SendEventChecked(v.xc, false, v.screen.Root, uint32(evMask), string(ev.Bytes())).Check(); err != nil {
		return err
	}

	xproto.ConfigureWindow(v.xc, v.window, xproto.ConfigWindowStackMode, []uint32{xproto.StackModeAbove})

	return nil
}

func (v *viewX11) SetTitle(title string) error {
	data := []byte(title)
	err := xproto.ChangePropertyChecked(v.xc, xproto.PropModeReplace, v.window,
		xproto.AtomWmName, xproto.AtomString, 8, uint32(len(data)), data).Check()
	if err != nil {
		return err
	}

	return nil
}

const maxIconX11 = 128

func scaleIconDown(src *image.RGBA, max int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= max && h <= max {
		return src
	}

	nw, nh := max, max
	if w >= h {
		nh = max * h / w
	} else {
		nw = max * w / h
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := b.Min.Y + y*h/nh
		for x := 0; x < nw; x++ {
			sx := b.Min.X + x*w/nw
			so := sy*src.Stride + sx*4
			do := y*dst.Stride + x*4
			copy(dst.Pix[do:do+4], src.Pix[so:so+4])
		}
	}

	return dst
}

func (v *viewX11) SetIcon(img image.Image) error {
	src := scaleIconDown(imageToRGBA(img), maxIconX11)
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()

	buf := make([]byte, (2+w*h)*4)
	xgb.Put32(buf[0:], uint32(w))
	xgb.Put32(buf[4:], uint32(h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			o := y*src.Stride + x*4
			argb := uint32(src.Pix[o+3])<<24 | uint32(src.Pix[o])<<16 | uint32(src.Pix[o+1])<<8 | uint32(src.Pix[o+2])
			xgb.Put32(buf[(2+y*w+x)*4:], argb)
		}
	}

	name := "_NET_WM_ICON"
	atom, err := xproto.InternAtom(v.xc, false, uint16(len(name)), name).Reply()
	if err != nil {
		return err
	}

	return xproto.ChangePropertyChecked(v.xc, xproto.PropModeReplace, v.window,
		atom.Atom, xproto.AtomCardinal, 32, uint32(len(buf)/4), buf).Check()
}

func (v *viewX11) ScreenSize() (int, int) {
	return v.screenWidth, v.screenHeight
}

func (v *viewX11) WindowSize() (int, int) {
	return v.winWidth, v.winHeight
}

func (v *viewX11) Close() error {
	var e error

	if v.xc == nil {
		return nil
	}

	if v.shmId != 0 {
		err := v.imageDestroy()
		if err != nil {
			e = errors.Join(e, err)
		}
	}

	err := xproto.FreeGCChecked(v.xc, v.gc).Check()
	if err != nil {
		e = errors.Join(e, err)
	}

	err = xproto.DestroyWindowChecked(v.xc, v.window).Check()
	if err != nil {
		e = errors.Join(e, err)
	}

	v.xc.Close()
	v.xc = nil

	v.connected.Store(false)

	if v.closedHandler != nil {
		v.closedHandler()
	}

	if errors.Is(err, io.EOF) {
		return nil
	}

	return e
}

func (v *viewX11) Clear() {}

func (v *viewX11) SetKeyPressHandler(handler KeyPressHandler) {
	v.keyPressHandler = handler
}

func (v *viewX11) SetKeyReleaseHandler(handler KeyReleaseHandler) {
	v.keyReleaseHandler = handler
}

func (v *viewX11) SetButtonPressHandler(handler ButtonPressHandler) {
	v.buttonPressHandler = handler
}

func (v *viewX11) SetButtonReleaseHandler(handler ButtonReleaseHandler) {
	v.buttonReleaseHandler = handler
}

func (v *viewX11) SetMotionHandler(handler MotionHandler) {
	v.motionHandler = handler
}

func (v *viewX11) SetScrollHandler(handler ScrollHandler) {
	v.scrollHandler = handler
}

func (v *viewX11) SetEnterHandler(handler EnterHandler) {
	v.enterHandler = handler
}

func (v *viewX11) SetLeaveHandler(handler LeaveHandler) {
	v.leaveHandler = handler
}

func (v *viewX11) SetResizeHandler(handler ResizeHandler) {
	v.resizeHandler = handler
}

func (v *viewX11) SetCreatedHandler(handler CreatedHandler) {
	v.createdHandler = handler
}

func (v *viewX11) SetClosedHandler(handler ClosedHandler) {
	v.closedHandler = handler
}

func (v *viewX11) ensureBuffer() error {
	if v.shmData != nil && v.bufW == v.winWidth && v.bufH == v.winHeight {
		return nil
	}

	if err := v.imageDestroy(); err != nil {
		return err
	}

	var err error
	size := v.winWidth * v.winHeight * 4

	v.shmId, err = shm.Get(shm.IPC_PRIVATE, size, shm.IPC_CREAT|0666)
	if err != nil {
		return err
	}

	v.shmSeg, err = mshm.NewSegId(v.xc)
	if err != nil {
		return err
	}

	v.shmData, err = shm.At(v.shmId, 0, 0)
	if err != nil {
		return err
	}

	mshm.Attach(v.xc, v.shmSeg, uint32(v.shmId), false)

	v.bufW = v.winWidth
	v.bufH = v.winHeight

	return nil
}

func (v *viewX11) imageDestroy() error {
	if v.shmId == 0 || v.shmSeg == 0 {
		return nil
	}

	mshm.Detach(v.xc, v.shmSeg)

	err := shm.Dt(v.shmData)
	if err != nil {
		return err
	}

	err = shm.Rm(v.shmId)
	if err != nil {
		return err
	}

	v.shmId = 0
	v.shmSeg = 0
	v.shmData = nil
	v.bufW = 0
	v.bufH = 0

	return nil
}

func (v *viewX11) compose(data []byte, img image.Image) {
	v.fill(data)
	v.blit(data, imageToRGBA(img))
}

func (v *viewX11) fill(data []byte) {
	px := [4]byte{v.bg.B, v.bg.G, v.bg.R, 0xff}

	copy(data[0:4], px[:])
	for f := 4; f < len(data); f *= 2 {
		copy(data[f:], data[:f])
	}
}

func (v *viewX11) blit(data []byte, src *image.RGBA) {
	b := src.Bounds()
	iw, ih := b.Dx(), b.Dy()

	dx := (v.bufW - iw) / 2
	dy := (v.bufH - ih) / 2
	if dx < 0 {
		dx = 0
	}
	if dy < 0 {
		dy = 0
	}

	cols, rows := iw, ih
	if dx+cols > v.bufW {
		cols = v.bufW - dx
	}
	if dy+rows > v.bufH {
		rows = v.bufH - dy
	}

	dStride := v.bufW * 4
	for y := 0; y < rows; y++ {
		so := y * src.Stride
		do := (dy+y)*dStride + dx*4
		row := data[do : do+cols*4]
		copy(row, src.Pix[so:so+cols*4])
		_ = swizzle.BGRA(row)
	}
}

func (v *viewX11) imageShmPut() {
	mshm.PutImage(v.xc, xproto.Drawable(v.window), v.gc,
		uint16(v.bufW), uint16(v.bufH), // TotalWidth, TotalHeight,
		0, 0, // SrcX, SrcY,
		uint16(v.bufW), uint16(v.bufH), // SrcWidth, SrcHeight,
		0, 0, // DstX, DstY,
		v.screen.RootDepth, xproto.ImageFormatZPixmap, 0, v.shmSeg, 0)
}

func (v *viewX11) imagePut(img image.Image) {
	size := v.winWidth * v.winHeight * 4
	if len(v.buf) != size {
		v.buf = make([]byte, size)
	}
	v.bufW = v.winWidth
	v.bufH = v.winHeight

	v.compose(v.buf, img)

	widthPerReq := v.bufW
	rowPerReq := xPutImageReqDataSize / (widthPerReq * 4)
	dataPerReq := rowPerReq * widthPerReq * 4

	start := 0
	end := 0
	dstY := 0

	for end < len(v.buf) {
		end = start + dataPerReq
		if end > len(v.buf) {
			end = len(v.buf)
		}

		data := v.buf[start:end]
		heightPerReq := len(data) / (widthPerReq * 4)

		xproto.PutImage(v.xc, xproto.ImageFormatZPixmap, xproto.Drawable(v.window), v.gc,
			uint16(widthPerReq), uint16(heightPerReq),
			0, int16(dstY),
			0, v.screen.RootDepth, data)

		start = end
		dstY += rowPerReq
	}
}

func (v *viewX11) setClass(class string) error {
	raw := make([]byte, len(class)+len(class)+2)
	copy(raw, class)
	copy(raw[(len(class)+1):], class)

	err := xproto.ChangePropertyChecked(v.xc, xproto.PropModeReplace, v.window,
		xproto.AtomWmClass, xproto.AtomString, 8, uint32(len(raw)), raw).Check()
	if err != nil {
		return err
	}

	return nil
}

func (v *viewX11) setNormalHints() error {
	nh := &normalHints{}
	nh.Flags = sizeHintUSPosition | sizeHintPWinGravity
	nh.WinGravity = xproto.GravityCenter
	nh.X = int(v.screenWidth)/2 - v.winWidth/2
	nh.Y = int(v.screenHeight)/2 - v.winHeight/2

	data := []uint{
		nh.Flags,
		uint(nh.X), uint(nh.Y), nh.Width, nh.Height,
		nh.MinWidth, nh.MinHeight,
		nh.MaxWidth, nh.MaxHeight,
		nh.WidthInc, nh.HeightInc,
		nh.MinAspectNum, nh.MinAspectDen,
		nh.MaxAspectNum, nh.MaxAspectDen,
		nh.BaseWidth, nh.BaseHeight,
		nh.WinGravity,
	}

	buf := make([]byte, len(data)*4)
	for i, d := range data {
		xgb.Put32(buf[(i*4):], uint32(d))
	}

	err := xproto.ChangePropertyChecked(v.xc, xproto.PropModeReplace, v.window,
		xproto.AtomWmNormalHints, xproto.AtomWmSizeHints, 32, uint32(len(buf)/4), buf).Check()
	if err != nil {
		return err
	}

	return nil
}

type normalHints struct {
	Flags                      uint
	X, Y                       int
	Width, Height              uint
	MinWidth, MinHeight        uint
	MaxWidth, MaxHeight        uint
	WidthInc, HeightInc        uint
	MinAspectNum, MinAspectDen uint
	MaxAspectNum, MaxAspectDen uint
	BaseWidth, BaseHeight      uint
	WinGravity                 uint
}

const (
	xPutImageReqSizeMax   = (1 << 16) * 4
	xPutImageReqSizeFixed = 28
	xPutImageReqDataSize  = xPutImageReqSizeMax - xPutImageReqSizeFixed
)

const (
	sizeHintUSPosition  = 1
	sizeHintPWinGravity = 512
)

const (
	KeySpace        = 65
	KeyEscape       = 9
	KeyEnter        = 36
	KeyTab          = 23
	KeyBackspace    = 22
	KeyInsert       = 118
	KeyDelete       = 119
	KeyRight        = 114
	KeyLeft         = 113
	KeyDown         = 116
	KeyUp           = 111
	KeyPageUp       = 112
	KeyPageDown     = 117
	KeyHome         = 110
	KeyEnd          = 115
	KeyCapsLock     = 66
	KeyScrollLock   = 78
	KeyNumLock      = 77
	KeyPrintScreen  = 107
	KeyPause        = 127
	KeyF1           = 67
	KeyF2           = 68
	KeyF3           = 69
	KeyF4           = 70
	KeyF5           = 71
	KeyF6           = 72
	KeyF7           = 73
	KeyF8           = 74
	KeyF9           = 75
	KeyF10          = 76
	KeyF11          = 95
	KeyF12          = 96
	KeyLeftShift    = 50
	KeyLeftControl  = 37
	KeyLeftAlt      = 64
	KeyLeftSuper    = 133
	KeyRightShift   = 62
	KeyRightControl = 105
	KeyRightAlt     = 108
	KeyRightSuper   = 134
	KeyLeftBracket  = 34
	KeyBackSlash    = 51
	KeyRightBracket = 35
	KeyGrave        = 49
	KeyKp0          = 90
	KeyKp1          = 87
	KeyKp2          = 88
	KeyKp3          = 89
	KeyKp4          = 83
	KeyKp5          = 84
	KeyKp6          = 85
	KeyKp7          = 79
	KeyKp8          = 80
	KeyKp9          = 81
	KeyKpDecimal    = 91
	KeyKpDivide     = 106
	KeyKpMultiply   = 63
	KeyKpSubtract   = 82
	KeyKpAdd        = 8
	KeyKpEnter      = 104
	KeyApostrophe   = 48
	KeyComma        = 59
	KeyMinus        = 20
	KeyPeriod       = 60
	KeySlash        = 61
	Key0            = 19
	Key1            = 10
	Key2            = 11
	Key3            = 12
	Key4            = 13
	Key5            = 14
	Key6            = 15
	Key7            = 16
	Key8            = 17
	Key9            = 18
	KeySemicolon    = 47
	KeyEqual        = 21
	KeyA            = 38
	KeyB            = 56
	KeyC            = 54
	KeyD            = 40
	KeyE            = 26
	KeyF            = 41
	KeyG            = 42
	KeyH            = 43
	KeyI            = 31
	KeyJ            = 44
	KeyK            = 45
	KeyL            = 46
	KeyM            = 58
	KeyN            = 57
	KeyO            = 32
	KeyP            = 33
	KeyQ            = 24
	KeyR            = 27
	KeyS            = 39
	KeyT            = 28
	KeyU            = 30
	KeyV            = 55
	KeyW            = 25
	KeyX            = 53
	KeyY            = 29
	KeyZ            = 52

	ButtonLeft   = 1
	ButtonMiddle = 2
	ButtonRight  = 3

	ScrollUp   = 4
	ScrollDown = 5
)
