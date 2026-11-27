//go:build linux

package iv

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"time"

	"github.com/gen2brain/wl"
	"github.com/gen2brain/wl-protocols/wp"
	"github.com/gen2brain/wl-protocols/xdg"
	"github.com/gen2brain/wl-protocols/zxdg"
	"golang.org/x/sys/unix"
)

const nbuf = 4

const axisStep = 120

type shmBuf struct {
	wl   wl.Buffer
	data []byte
	busy bool
}

type wlCanvas struct {
	data    []byte
	stride  int
	w, h    int
	useXBGR bool
}

type viewWayland struct {
	display    *wl.Display
	compositor wl.Compositor
	shm        wl.Shm
	seat       wl.Seat
	output     wl.Output
	wmBase     xdg.WmBase

	surface    wl.Surface
	xdgSurface xdg.Surface
	toplevel   xdg.Toplevel

	iconManager xdg.ToplevelIconManagerV1
	hasIconMgr  bool
	icon        xdg.ToplevelIconV1
	hasIcon     bool
	iconBuf     wl.Buffer
	iconFd      int
	iconData    []byte
	barIcon     *image.RGBA
	appID       string

	keyboard wl.Keyboard
	pointer  wl.Pointer

	cursorMgr    wp.CursorShapeManagerV1
	hasCursorMgr bool
	cursor       wp.CursorShapeDeviceV1
	hasCursor    bool
	cursorShape  wp.CursorShapeDeviceV1Shape
	enterSerial  uint32

	hasOutput bool
	useXBGR   bool
	format    wl.ShmFormat

	poolFd   int
	poolData []byte
	bufs     []*shmBuf
	current  *shmBuf
	bufW     int
	bufH     int

	bg        color.RGBA
	textColor color.RGBA
	image     *image.RGBA
	title     string
	csd       bool

	imgWidth, imgHeight       int
	winWidth, winHeight       int
	screenWidth, screenHeight int

	configured  bool
	running     bool
	frameWait   bool
	frameCb     bool
	fullscreen  bool
	maximized   bool
	closed      bool
	closedFired bool
	axisAccum   int32

	ptrX, ptrY int
	hoverBtn   int
	overBar    bool

	repeatKey   int
	repeatRate  int32
	repeatDelay int32
	repeatNext  time.Time

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
}

func newWayland(opts Options) (*viewWayland, error) {
	v := &viewWayland{}
	v.winWidth = opts.Width
	v.winHeight = opts.Height
	v.hoverBtn = -1
	v.appID = opts.AppID
	v.repeatRate = 25
	v.repeatDelay = 500
	v.cursorShape = wp.CursorShapeDeviceV1ShapeDefault

	bg, err := parseHexColor(opts.BackgroundColor)
	if err != nil {
		return v, fmt.Errorf("cannot parse color: %w", err)
	}
	v.bg = bg

	textColor, err := parseHexColor(opts.TextColor)
	if err != nil {
		return v, fmt.Errorf("cannot parse color: %w", err)
	}
	v.textColor = textColor

	v.display, err = wl.NewDisplay("")
	if err != nil {
		return v, fmt.Errorf("cannot connect to server: %w", err)
	}

	var compositor, shm, wmBase bool
	var deco zxdg.DecorationManagerV1
	var hasDeco bool
	formats := make(map[wl.ShmFormat]bool)

	registry := v.display.GetRegistry()
	registry.SetListener(wl.RegistryListener{
		Global: func(_ any, self wl.Registry, name uint32, iface string, version uint32) error {
			switch iface {
			case "wl_compositor":
				v.compositor = wl.Compositor(self.Bind(name, &wl.CompositorInterface, version))
				compositor = true
			case "wl_shm":
				v.shm = wl.Shm(self.Bind(name, &wl.ShmInterface, version))
				shm = true
				v.shm.SetListener(wl.ShmListener{
					Format: func(_ any, _ wl.Shm, format wl.ShmFormat) error {
						formats[format] = true
						return nil
					},
				}, nil)
			case "xdg_wm_base":
				v.wmBase = xdg.WmBase(self.Bind(name, &xdg.WmBaseInterface, version))
				wmBase = true
			case "wl_seat":
				v.seat = wl.Seat(self.Bind(name, &wl.SeatInterface, version))
				v.seat.SetListener(wl.SeatListener{Capabilities: v.onCapabilities}, nil)
			case "wl_output":
				if !v.hasOutput {
					v.output = wl.Output(self.Bind(name, &wl.OutputInterface, version))
					v.hasOutput = true
					v.output.SetListener(wl.OutputListener{Mode: v.onOutputMode}, nil)
				}
			case "zxdg_decoration_manager_v1":
				deco = zxdg.DecorationManagerV1(self.Bind(name, &zxdg.DecorationManagerV1Interface, version))
				hasDeco = true
			case "xdg_toplevel_icon_manager_v1":
				v.iconManager = xdg.ToplevelIconManagerV1(self.Bind(name, &xdg.ToplevelIconManagerV1Interface, version))
				v.hasIconMgr = true
			case "wp_cursor_shape_manager_v1":
				v.cursorMgr = wp.CursorShapeManagerV1(self.Bind(name, &wp.CursorShapeManagerV1Interface, version))
				v.hasCursorMgr = true
			}
			return nil
		},
	}, nil)

	if err := v.display.Roundtrip(); err != nil {
		return v, fmt.Errorf("cannot roundtrip: %w", err)
	}
	if err := v.display.Roundtrip(); err != nil {
		return v, fmt.Errorf("cannot roundtrip: %w", err)
	}

	if !compositor || !shm || !wmBase {
		return v, fmt.Errorf("missing required globals (wl_compositor, wl_shm, xdg_wm_base)")
	}

	v.useXBGR = formats[wl.ShmFormatXbgr8888]
	v.format = wl.ShmFormatXrgb8888
	if v.useXBGR {
		v.format = wl.ShmFormatXbgr8888
	}

	v.wmBase.SetListener(xdg.WmBaseListener{
		Ping: func(_ any, _ xdg.WmBase, serial uint32) error {
			v.wmBase.Pong(serial)
			return nil
		},
	}, nil)

	v.surface = v.compositor.CreateSurface()

	v.xdgSurface = v.wmBase.GetXdgSurface(v.surface)
	v.xdgSurface.SetListener(xdg.SurfaceListener{
		Configure: func(_ any, _ xdg.Surface, serial uint32) error {
			v.xdgSurface.AckConfigure(serial)
			if v.image != nil {
				if err := v.render(); err != nil {
					return err
				}
			}
			return nil
		},
	}, nil)

	v.toplevel = v.xdgSurface.GetToplevel()
	v.toplevel.SetListener(xdg.ToplevelListener{
		Configure: v.onToplevelConfigure,
		Close:     v.onToplevelClose,
	}, nil)
	v.toplevel.SetAppId(opts.AppID)

	if hasDeco {
		d := deco.GetToplevelDecoration(v.toplevel)
		d.SetListener(zxdg.ToplevelDecorationV1Listener{
			Configure: func(_ any, _ zxdg.ToplevelDecorationV1, mode zxdg.ToplevelDecorationV1Mode) error {
				v.setCSD(mode == zxdg.ToplevelDecorationV1ModeClientSide)
				return nil
			},
		}, nil)
		d.SetMode(zxdg.ToplevelDecorationV1ModeServerSide)
	} else {
		v.csd = true
	}

	v.surface.Commit()
	if err := v.display.Flush(); err != nil {
		return v, fmt.Errorf("cannot flush: %w", err)
	}

	return v, nil
}

func (v *viewWayland) Driver() string {
	return "wayland"
}

func (v *viewWayland) Display(ctx context.Context, img image.Image, args ...any) error {
	if len(args) > 0 {
		title, ok := args[0].(string)
		if !ok {
			return fmt.Errorf("invalid argument: %v", args[0])
		}
		if title != "" {
			v.title = title
			v.toplevel.SetTitle(title)
		}
	}

	return v.show(ctx, img, nil)
}

// Present draws img and waits until the compositor has shown it or ctx is done.
func (v *viewWayland) Present(ctx context.Context, img image.Image) error {
	v.frameWait = true

	return v.show(ctx, img, func() bool { return !v.frameWait })
}

func (v *viewWayland) show(ctx context.Context, img image.Image, done func() bool) error {
	b := img.Bounds()
	v.imgWidth = b.Dx()
	v.imgHeight = b.Dy()
	v.image = imageToRGBA(img)

	if v.configured {
		if err := v.render(); err != nil {
			return err
		}
	}

	if v.running {
		return nil
	}

	v.running = true
	defer func() {
		v.running = false
		v.frameWait = false
	}()

	for {
		if ctx.Err() != nil || v.closed || (done != nil && done()) {
			return nil
		}

		if err := v.dispatch(ctx); err != nil {
			if v.closed || ctx.Err() != nil {
				return nil
			}
			return err
		}

		if v.closed || v.display == nil {
			return nil
		}

		v.fireRepeat()

		if err := v.display.Flush(); err != nil {
			return err
		}
	}
}

// dispatch waits for compositor events until ctx is done or the next key repeat is due.
func (v *viewWayland) dispatch(ctx context.Context) error {
	if v.repeatKey == 0 || v.keyPressHandler == nil {
		return v.display.DispatchContext(ctx)
	}

	rctx, cancel := context.WithDeadline(ctx, v.repeatNext)
	defer cancel()

	err := v.display.DispatchContext(rctx)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		return nil
	}

	return err
}

func (v *viewWayland) render() error {
	w, h := v.winWidth, v.winHeight
	if w <= 0 || h <= 0 || v.image == nil {
		return nil
	}

	if err := v.ensurePool(w, h); err != nil {
		return err
	}

	b := v.acquire()
	if b == nil {
		return nil
	}

	ib := v.image.Bounds()
	r := placeImage(w, h, v.contentTop(), ib.Dx(), ib.Dy())

	fillAround(b.data, w, h, r, v.bgPixel())
	blitImage(b.data, w, r, v.image, !v.useXBGR)
	if v.hasTitleBar() {
		v.drawWindowBar(b.data, w)
	}

	b.busy = true
	v.current = b

	v.surface.Attach(b.wl, 0, 0)
	v.surface.DamageBuffer(0, 0, int32(w), int32(h))
	if v.frameWait && !v.frameCb {
		v.frameCb = true
		cb := v.surface.Frame()
		cb.SetListener(wl.CallbackListener{Done: v.onFrame}, nil)
	}
	v.surface.Commit()

	return v.display.Flush()
}

func (v *viewWayland) onFrame(_ any, _ wl.Callback, _ uint32) error {
	v.frameCb = false
	v.frameWait = false

	return nil
}

func (v *viewWayland) bgPixel() [4]byte {
	if v.useXBGR {
		return [4]byte{v.bg.R, v.bg.G, v.bg.B, 0xff}
	}

	return [4]byte{v.bg.B, v.bg.G, v.bg.R, 0xff}
}

func (v *viewWayland) setCSD(csd bool) {
	if v.csd == csd {
		return
	}
	v.csd = csd
	if v.configured && v.resizeHandler != nil {
		v.resizeHandler(v.winWidth, v.contentHeight())
	}
	if v.image != nil {
		_ = v.render()
	}
}

func (c *wlCanvas) Set(x, y int, clr color.Color) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return
	}

	r, g, b, _ := clr.RGBA()
	o := y*c.stride + x*4
	if c.useXBGR {
		c.data[o], c.data[o+1], c.data[o+2], c.data[o+3] = uint8(r>>8), uint8(g>>8), uint8(b>>8), 0xff
	} else {
		c.data[o], c.data[o+1], c.data[o+2], c.data[o+3] = uint8(b>>8), uint8(g>>8), uint8(r>>8), 0xff
	}
}

func (v *viewWayland) ensurePool(w, h int) error {
	if v.bufs != nil && v.bufW == w && v.bufH == h {
		return nil
	}

	v.destroyPool()

	stride := w * 4
	frame := stride * h
	total := frame * nbuf

	fd, err := unix.MemfdCreate("iv", 0)
	if err != nil {
		return fmt.Errorf("cannot create anon file: %w", err)
	}

	if err := unix.Ftruncate(fd, int64(total)); err != nil {
		_ = unix.Close(fd)
		return fmt.Errorf("cannot truncate anon file: %w", err)
	}

	data, err := unix.Mmap(fd, 0, total, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		_ = unix.Close(fd)
		return fmt.Errorf("cannot map anon file: %w", err)
	}

	pool := v.shm.CreatePool(fd, int32(total))
	v.bufs = make([]*shmBuf, 0, nbuf)
	for i := 0; i < nbuf; i++ {
		sb := &shmBuf{
			wl:   pool.CreateBuffer(int32(i*frame), int32(w), int32(h), int32(stride), v.format),
			data: data[i*frame : (i+1)*frame],
		}
		sb.wl.SetListener(wl.BufferListener{
			Release: func(_ any, _ wl.Buffer) error {
				sb.busy = false
				return nil
			},
		}, nil)
		v.bufs = append(v.bufs, sb)
	}
	pool.Destroy()

	v.poolFd = fd
	v.poolData = data
	v.bufW = w
	v.bufH = h

	return nil
}

func (v *viewWayland) destroyPool() {
	for _, b := range v.bufs {
		b.wl.Destroy()
	}
	v.bufs = nil
	v.current = nil

	if v.poolData != nil {
		_ = unix.Munmap(v.poolData)
		v.poolData = nil
	}
	if v.poolFd != 0 {
		_ = unix.Close(v.poolFd)
		v.poolFd = 0
	}
	v.bufW, v.bufH = 0, 0
}

func (v *viewWayland) acquire() *shmBuf {
	for _, b := range v.bufs {
		if !b.busy {
			return b
		}
	}
	if len(v.bufs) > 0 {
		return v.bufs[0]
	}

	return nil
}

func (v *viewWayland) onToplevelConfigure(_ any, _ xdg.Toplevel, width, height int32, states []byte) error {
	first := !v.configured
	if first {
		v.configured = true
		if v.createdHandler != nil {
			v.createdHandler()
		}
	}

	v.maximized = false
	for i := 0; i+4 <= len(states); i += 4 {
		if binary.NativeEndian.Uint32(states[i:i+4]) == xdgStateMaximized {
			v.maximized = true
		}
	}

	changed := false
	if width != 0 && height != 0 && (int(width) != v.winWidth || int(height) != v.winHeight) {
		v.winWidth = int(width)
		v.winHeight = int(height)
		changed = true
	}

	if (first || changed) && v.resizeHandler != nil {
		v.resizeHandler(v.winWidth, v.contentHeight())
	}

	return nil
}

func (v *viewWayland) onToplevelClose(_ any, _ xdg.Toplevel) error {
	v.fireClose()
	return nil
}

func (v *viewWayland) onCapabilities(_ any, self wl.Seat, caps wl.SeatCapability) error {
	if caps&wl.SeatCapabilityKeyboard != 0 && v.keyboard == (wl.Keyboard{}) {
		v.keyboard = self.GetKeyboard()
		v.keyboard.SetListener(wl.KeyboardListener{Leave: v.onKeyboardLeave, Key: v.onKey, RepeatInfo: v.onRepeatInfo}, nil)
	}

	if caps&wl.SeatCapabilityPointer != 0 && v.pointer == (wl.Pointer{}) {
		v.pointer = self.GetPointer()
		v.pointer.SetListener(wl.PointerListener{
			Button:       v.onButton,
			AxisValue120: v.onAxisValue120,
			AxisDiscrete: v.onAxisDiscrete,
			Frame:        v.onPointerFrame,
			Motion:       v.onMotion,
			Enter:        v.onEnter,
			Leave:        v.onLeave,
		}, nil)

		if v.hasCursorMgr {
			v.cursor = v.cursorMgr.GetPointer(v.pointer)
			v.hasCursor = true
		}
	}

	return nil
}

func (v *viewWayland) onKey(_ any, _ wl.Keyboard, _, _, key uint32, state wl.KeyboardKeyState) error {
	val := int(key) + 8

	if state == wl.KeyboardKeyStatePressed {
		v.startRepeat(val)
		if v.keyPressHandler != nil {
			v.keyPressHandler(val)
		}
	} else if state == wl.KeyboardKeyStateReleased {
		if v.repeatKey == val {
			v.repeatKey = 0
		}
		if v.keyReleaseHandler != nil {
			v.keyReleaseHandler(val)
		}
	}

	return nil
}

func (v *viewWayland) onKeyboardLeave(_ any, _ wl.Keyboard, _ uint32, _ wl.Surface) error {
	v.repeatKey = 0

	return nil
}

func (v *viewWayland) onRepeatInfo(_ any, _ wl.Keyboard, rate, delay int32) error {
	v.repeatRate = rate
	v.repeatDelay = delay
	if rate <= 0 {
		v.repeatKey = 0
	}

	return nil
}

func (v *viewWayland) startRepeat(key int) {
	if v.repeatRate <= 0 || repeatExempt(key) {
		v.repeatKey = 0
		return
	}

	v.repeatKey = key
	v.repeatNext = time.Now().Add(time.Duration(v.repeatDelay) * time.Millisecond)
}

func (v *viewWayland) fireRepeat() {
	if v.repeatKey == 0 || v.keyPressHandler == nil {
		return
	}

	now := time.Now()
	if now.Before(v.repeatNext) {
		return
	}

	v.repeatNext = now.Add(time.Second / time.Duration(v.repeatRate))
	v.keyPressHandler(v.repeatKey)
}

// repeatExempt reports whether a key should not auto-repeat (modifiers).
func repeatExempt(key int) bool {
	switch key {
	case KeyLeftShift, KeyRightShift, KeyLeftControl, KeyRightControl, KeyLeftAlt, KeyRightAlt:
		return true
	}

	return false
}

func (v *viewWayland) onButton(_ any, _ wl.Pointer, serial, _, button uint32, state wl.PointerButtonState) error {
	if v.hasTitleBar() && button == btnLeft && v.ptrY < titleBarHeight {
		if state == wl.PointerButtonStatePressed {
			v.pressTitleBar(serial)
		}
		return nil
	}

	val, ok := buttonMapWayland[int(button)]
	if !ok {
		return nil
	}

	if state == wl.PointerButtonStatePressed {
		if v.buttonPressHandler != nil {
			v.buttonPressHandler(val)
		}
	} else if state == wl.PointerButtonStateReleased {
		if v.buttonReleaseHandler != nil {
			v.buttonReleaseHandler(val)
		}
	}

	return nil
}

func (v *viewWayland) onAxisValue120(_ any, _ wl.Pointer, axis wl.PointerAxis, value120 int32) error {
	if axis == wl.PointerAxisVerticalScroll {
		v.axisAccum += value120
	}

	return nil
}

func (v *viewWayland) onAxisDiscrete(_ any, _ wl.Pointer, axis wl.PointerAxis, discrete int32) error {
	if axis == wl.PointerAxisVerticalScroll {
		v.axisAccum += discrete * axisStep
	}

	return nil
}

func (v *viewWayland) onPointerFrame(_ any, _ wl.Pointer) error {
	if v.scrollHandler == nil {
		v.axisAccum = 0
		return nil
	}

	for v.axisAccum >= axisStep {
		v.axisAccum -= axisStep
		v.scrollHandler(ScrollDown)
	}
	for v.axisAccum <= -axisStep {
		v.axisAccum += axisStep
		v.scrollHandler(ScrollUp)
	}

	return nil
}

func (v *viewWayland) onMotion(_ any, _ wl.Pointer, _ uint32, x, y float64) error {
	v.ptrX, v.ptrY = int(x), int(y)

	if v.hasTitleBar() {
		overBar := v.ptrY < titleBarHeight
		if overBar && !v.overBar && v.hasCursor {
			v.cursor.SetShape(v.enterSerial, wp.CursorShapeDeviceV1ShapeDefault)
		}
		v.overBar = overBar

		if hb := v.titleBarButtonAt(v.ptrX, v.ptrY); hb != v.hoverBtn {
			v.hoverBtn = hb
			_ = v.render()
		}
		if overBar {
			return nil
		}
	}

	if v.motionHandler != nil {
		v.motionHandler(v.ptrX, v.ptrY-v.contentTop())
	}

	return nil
}

func (v *viewWayland) onEnter(_ any, _ wl.Pointer, serial uint32, _ wl.Surface, _, _ float64) error {
	v.enterSerial = serial
	if v.hasCursor {
		v.cursor.SetShape(serial, v.cursorShape)
	}

	if v.enterHandler != nil {
		v.enterHandler()
	}

	return nil
}

func wlCursorShape(c Cursor) wp.CursorShapeDeviceV1Shape {
	switch c {
	case CursorPointer:
		return wp.CursorShapeDeviceV1ShapePointer
	case CursorGrab:
		return wp.CursorShapeDeviceV1ShapeGrab
	case CursorGrabbing:
		return wp.CursorShapeDeviceV1ShapeGrabbing
	case CursorText:
		return wp.CursorShapeDeviceV1ShapeText
	case CursorCrosshair:
		return wp.CursorShapeDeviceV1ShapeCrosshair
	default:
		return wp.CursorShapeDeviceV1ShapeDefault
	}
}

func (v *viewWayland) SetCursor(c Cursor) error {
	v.cursorShape = wlCursorShape(c)

	if !v.hasCursor || v.enterSerial == 0 {
		return nil
	}

	v.cursor.SetShape(v.enterSerial, v.cursorShape)

	return v.display.Flush()
}

func (v *viewWayland) onLeave(_ any, _ wl.Pointer, _ uint32, _ wl.Surface) error {
	v.overBar = false
	if v.hoverBtn != -1 {
		v.hoverBtn = -1
		if v.image != nil {
			_ = v.render()
		}
	}

	if v.leaveHandler != nil {
		v.leaveHandler()
	}

	return nil
}

func (v *viewWayland) onOutputMode(_ any, _ wl.Output, flags wl.OutputMode, width, height, _ int32) error {
	if flags&wl.OutputModeCurrent != 0 {
		v.screenWidth = int(width)
		v.screenHeight = int(height)
	}

	return nil
}

func (v *viewWayland) ToggleFullscreen() error {
	if v.fullscreen {
		v.toplevel.UnsetFullscreen()
		v.fullscreen = false
	} else {
		v.toplevel.SetFullscreen(v.output)
		v.fullscreen = true
	}

	return v.display.Flush()
}

func (v *viewWayland) Fullscreen() bool {
	return v.fullscreen
}

func (v *viewWayland) Maximize() error {
	v.toplevel.SetMaximized()

	return v.display.Flush()
}

func (v *viewWayland) Raise() error {
	return nil
}

func (v *viewWayland) SetTitle(title string) error {
	v.title = title
	v.toplevel.SetTitle(title)

	// Client-side titles are drawn into the buffer, so repaint to show the change now.
	if v.csd && v.configured && v.image != nil {
		return v.render()
	}

	return nil
}

func (v *viewWayland) SetIcon(img image.Image) error {
	src := imageToRGBA(img)

	v.barIcon = scaleIconDown(src, titleIconSize)
	if v.csd && v.configured && v.image != nil {
		_ = v.render()
	}

	if !v.hasIconMgr {
		if v.csd && v.appID != "" {
			v.installDesktopIcon(img)
		}
		return nil
	}

	v.destroyIcon()

	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	stride := w * 4
	total := stride * h

	fd, err := unix.MemfdCreate("iv-icon", 0)
	if err != nil {
		return fmt.Errorf("cannot create anon file: %w", err)
	}
	if err := unix.Ftruncate(fd, int64(total)); err != nil {
		_ = unix.Close(fd)
		return fmt.Errorf("cannot truncate anon file: %w", err)
	}
	data, err := unix.Mmap(fd, 0, total, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		_ = unix.Close(fd)
		return fmt.Errorf("cannot map anon file: %w", err)
	}

	for y := 0; y < h; y++ {
		so := y * src.Stride
		do := y * stride
		for x := 0; x < w; x++ {
			r, g, bl, a := src.Pix[so+x*4], src.Pix[so+x*4+1], src.Pix[so+x*4+2], src.Pix[so+x*4+3]
			data[do+x*4], data[do+x*4+1], data[do+x*4+2], data[do+x*4+3] = bl, g, r, a
		}
	}

	pool := v.shm.CreatePool(fd, int32(total))
	v.iconBuf = pool.CreateBuffer(0, int32(w), int32(h), int32(stride), wl.ShmFormatArgb8888)
	pool.Destroy()

	v.icon = v.iconManager.CreateIcon()
	v.icon.AddBuffer(v.iconBuf, 1)
	v.iconManager.SetIcon(v.toplevel, v.icon)

	v.iconFd = fd
	v.iconData = data
	v.hasIcon = true

	return v.display.Flush()
}

func (v *viewWayland) destroyIcon() {
	if !v.hasIcon {
		return
	}

	v.icon.Destroy()
	v.iconBuf.Destroy()
	if v.iconData != nil {
		_ = unix.Munmap(v.iconData)
		v.iconData = nil
	}
	if v.iconFd != 0 {
		_ = unix.Close(v.iconFd)
		v.iconFd = 0
	}
	v.hasIcon = false
}

func (v *viewWayland) ScreenSize() (int, int) {
	return v.screenWidth, v.screenHeight
}

func (v *viewWayland) WindowSize() (int, int) {
	return v.winWidth, v.contentHeight()
}

func (v *viewWayland) Close() error {
	if v.display == nil {
		return nil
	}

	v.running = false

	if v.keyboard != (wl.Keyboard{}) {
		v.keyboard.Release()
		v.keyboard = wl.Keyboard{}
	}
	if v.hasCursor {
		v.cursor.Destroy()
		v.hasCursor = false
	}
	if v.pointer != (wl.Pointer{}) {
		v.pointer.Release()
		v.pointer = wl.Pointer{}
	}

	v.destroyIcon()
	v.destroyPool()

	err := v.display.Close()
	v.display = nil

	if v.closedHandler != nil && !v.closedFired {
		v.closedFired = true
		v.closedHandler()
	}

	return err
}

func (v *viewWayland) Clear() {
	w, h := v.winWidth, v.winHeight
	if w <= 0 || h <= 0 || !v.configured {
		return
	}

	if err := v.ensurePool(w, h); err != nil {
		return
	}

	b := v.acquire()
	if b == nil {
		return
	}

	v.image = nil
	fillPixels(b.data, v.bgPixel())

	b.busy = true
	v.current = b

	v.surface.Attach(b.wl, 0, 0)
	v.surface.DamageBuffer(0, 0, int32(w), int32(h))
	v.surface.Commit()

	_ = v.display.Flush()
}

func (v *viewWayland) SetKeyPressHandler(handler KeyPressHandler) {
	v.keyPressHandler = handler
}

func (v *viewWayland) SetKeyReleaseHandler(handler KeyReleaseHandler) {
	v.keyReleaseHandler = handler
}

func (v *viewWayland) SetButtonPressHandler(handler ButtonPressHandler) {
	v.buttonPressHandler = handler
}

func (v *viewWayland) SetButtonReleaseHandler(handler ButtonReleaseHandler) {
	v.buttonReleaseHandler = handler
}

func (v *viewWayland) SetMotionHandler(handler MotionHandler) {
	v.motionHandler = handler
}

func (v *viewWayland) SetScrollHandler(handler ScrollHandler) {
	v.scrollHandler = handler
}

func (v *viewWayland) SetEnterHandler(handler EnterHandler) {
	v.enterHandler = handler
}

func (v *viewWayland) SetLeaveHandler(handler LeaveHandler) {
	v.leaveHandler = handler
}

func (v *viewWayland) SetResizeHandler(handler ResizeHandler) {
	v.resizeHandler = handler
}

func (v *viewWayland) SetCreatedHandler(handler CreatedHandler) {
	v.createdHandler = handler
}

func (v *viewWayland) SetClosedHandler(handler ClosedHandler) {
	v.closedHandler = handler
}

const (
	btnLeft   = 0x110
	btnRight  = 0x111
	btnMiddle = 0x112
)

var buttonMapWayland = map[int]int{
	btnLeft:   ButtonLeft,
	btnMiddle: ButtonMiddle,
	btnRight:  ButtonRight,
}
