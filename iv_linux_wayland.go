//go:build linux

package iv

import (
	"context"
	"fmt"
	"image"
	"image/color"

	"codeberg.org/tesselslate/wl"
	"codeberg.org/tesselslate/wl-protocols/xdg"
	"codeberg.org/tesselslate/wl-protocols/zxdg"
	"github.com/pbnjay/pixfont"
	"golang.org/x/sys/unix"

	"github.com/gen2brain/iv/internal/swizzle"
)

const nbuf = 4

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

	keyboard wl.Keyboard
	pointer  wl.Pointer

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
	fullscreen  bool
	beat        bool
	closed      bool
	closedFired bool

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
			if err := v.SetTitle(title); err != nil {
				return err
			}
		}
	}

	b := img.Bounds()
	v.imgWidth = b.Dx()
	v.imgHeight = b.Dy()
	v.image = imageToRGBA(img)

	if v.configured {
		if err := v.render(); err != nil {
			return err
		}
	}

	_, hasDeadline := ctx.Deadline()
	v.beat = hasDeadline
	if v.beat && v.configured {
		v.armFrame()
	}

	if v.running {
		return nil
	}

	v.running = true
	defer func() {
		v.running = false
		v.beat = false
	}()

	for {
		if ctx.Err() != nil || v.closed {
			return nil
		}

		if err := v.display.Dispatch(); err != nil {
			if v.closed {
				return nil
			}
			return err
		}

		if v.closed || v.display == nil {
			return nil
		}

		if err := v.display.Flush(); err != nil {
			return err
		}
	}
}

func (v *viewWayland) armFrame() {
	cb := v.surface.Frame()
	cb.SetListener(wl.CallbackListener{Done: v.onFrame}, nil)
	v.surface.Commit()
}

func (v *viewWayland) onFrame(_ any, _ wl.Callback, _ uint32) error {
	if v.beat && !v.closed {
		v.surface.DamageBuffer(0, 0, 1, 1)
		v.armFrame()
	}

	return nil
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

	v.fill(b.data)
	v.blit(b.data, w)
	if v.csd && !v.fullscreen && v.title != "" {
		v.drawTitle(b.data, w)
	}

	b.busy = true
	v.current = b

	v.surface.Attach(b.wl, 0, 0)
	v.surface.DamageBuffer(0, 0, int32(w), int32(h))
	v.surface.Commit()

	return v.display.Flush()
}

func (v *viewWayland) fill(data []byte) {
	var px [4]byte
	if v.useXBGR {
		px = [4]byte{v.bg.R, v.bg.G, v.bg.B, 0xff}
	} else {
		px = [4]byte{v.bg.B, v.bg.G, v.bg.R, 0xff}
	}

	copy(data[0:4], px[:])
	for f := 4; f < len(data); f *= 2 {
		copy(data[f:], data[:f])
	}
}

func (v *viewWayland) blit(data []byte, w int) {
	b := v.image.Bounds()
	iw, ih := b.Dx(), b.Dy()

	dx := (w - iw) / 2
	dy := (v.winHeight - ih) / 2
	if dx < 0 {
		dx = 0
	}
	if dy < 0 {
		dy = 0
	}

	cols, rows := iw, ih
	if dx+cols > w {
		cols = w - dx
	}
	if dy+rows > v.winHeight {
		rows = v.winHeight - dy
	}

	dStride := w * 4
	for y := 0; y < rows; y++ {
		so := y * v.image.Stride
		do := (dy+y)*dStride + dx*4
		row := data[do : do+cols*4]
		copy(row, v.image.Pix[so:so+cols*4])
		if !v.useXBGR {
			_ = swizzle.BGRA(row)
		}
	}
}

func (v *viewWayland) setCSD(csd bool) {
	if v.csd == csd {
		return
	}
	v.csd = csd
	if v.image != nil {
		_ = v.render()
	}
}

func (v *viewWayland) drawTitle(data []byte, w int) {
	c := &wlCanvas{data: data, stride: w * 4, w: w, h: v.winHeight, useXBGR: v.useXBGR}
	pixfont.DrawString(c, titleMargin, titleMargin, elideTitle(v.title, w-2*titleMargin), v.textColor)
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

func (v *viewWayland) onToplevelConfigure(_ any, _ xdg.Toplevel, width, height int32, _ []byte) error {
	if !v.configured {
		v.configured = true
		if v.createdHandler != nil {
			v.createdHandler()
		}
	}

	if width == 0 || height == 0 {
		return nil
	}

	if int(width) != v.winWidth || int(height) != v.winHeight {
		v.winWidth = int(width)
		v.winHeight = int(height)
		if v.resizeHandler != nil {
			v.resizeHandler(v.winWidth, v.winHeight)
		}
	}

	return nil
}

func (v *viewWayland) onToplevelClose(_ any, _ xdg.Toplevel) error {
	v.running = false
	v.closed = true
	if v.closedHandler != nil && !v.closedFired {
		v.closedFired = true
		v.closedHandler()
	}

	return nil
}

func (v *viewWayland) onCapabilities(_ any, self wl.Seat, caps wl.SeatCapability) error {
	if caps&wl.SeatCapabilityKeyboard != 0 && v.keyboard == (wl.Keyboard{}) {
		v.keyboard = self.GetKeyboard()
		v.keyboard.SetListener(wl.KeyboardListener{Key: v.onKey}, nil)
	}

	if caps&wl.SeatCapabilityPointer != 0 && v.pointer == (wl.Pointer{}) {
		v.pointer = self.GetPointer()
		v.pointer.SetListener(wl.PointerListener{
			Button: v.onButton,
			Axis:   v.onAxis,
			Motion: v.onMotion,
			Enter:  v.onEnter,
			Leave:  v.onLeave,
		}, nil)
	}

	return nil
}

func (v *viewWayland) onKey(_ any, _ wl.Keyboard, _, _, key uint32, state wl.KeyboardKeyState) error {
	val := int(key) + 8

	if state == wl.KeyboardKeyStatePressed {
		if v.keyPressHandler != nil {
			v.keyPressHandler(val)
		}
	} else if state == wl.KeyboardKeyStateReleased {
		if v.keyReleaseHandler != nil {
			v.keyReleaseHandler(val)
		}
	}

	return nil
}

func (v *viewWayland) onButton(_ any, _ wl.Pointer, _, _, button uint32, state wl.PointerButtonState) error {
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

func (v *viewWayland) onAxis(_ any, _ wl.Pointer, _ uint32, axis wl.PointerAxis, value float64) error {
	if axis != wl.PointerAxisVerticalScroll || v.scrollHandler == nil {
		return nil
	}

	if value > 0 {
		v.scrollHandler(ScrollDown)
	} else if value < 0 {
		v.scrollHandler(ScrollUp)
	}

	return nil
}

func (v *viewWayland) onMotion(_ any, _ wl.Pointer, _ uint32, x, y float64) error {
	if v.motionHandler != nil {
		v.motionHandler(int(x), int(y))
	}

	return nil
}

func (v *viewWayland) onEnter(_ any, _ wl.Pointer, _ uint32, _ wl.Surface, _, _ float64) error {
	if v.enterHandler != nil {
		v.enterHandler()
	}

	return nil
}

func (v *viewWayland) onLeave(_ any, _ wl.Pointer, _ uint32, _ wl.Surface) error {
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

func (v *viewWayland) Raise() error {
	return nil
}

func (v *viewWayland) SetTitle(title string) error {
	v.title = title
	v.toplevel.SetTitle(title)

	return nil
}

func (v *viewWayland) SetIcon(img image.Image) error {
	if !v.hasIconMgr {
		return nil
	}

	v.destroyIcon()

	src := imageToRGBA(img)
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
	return v.winWidth, v.winHeight
}

func (v *viewWayland) Close() error {
	if v.display == nil {
		return nil
	}

	v.running = false
	v.beat = false

	if v.keyboard != (wl.Keyboard{}) {
		v.keyboard.Release()
		v.keyboard = wl.Keyboard{}
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

func (v *viewWayland) Clear() {}

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
