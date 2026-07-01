//go:build linux

package iv

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"os"
	"slices"
	"sync/atomic"
	"unsafe"

	"github.com/NeowayLabs/drm"
	"github.com/NeowayLabs/drm/ioctl"
	"github.com/NeowayLabs/drm/mode"
	"github.com/holoplot/go-evdev"
	"github.com/pbnjay/pixfont"
	"golang.org/x/sys/unix"

	"github.com/gen2brain/iv/internal/swizzle"
)

type viewDRM struct {
	dev *os.File

	msets    []mset
	modesets []*mode.Modeset

	keyPressHandler      KeyPressHandler
	keyReleaseHandler    KeyReleaseHandler
	buttonPressHandler   ButtonPressHandler
	buttonReleaseHandler ButtonReleaseHandler
	motionHandler        MotionHandler
	scrollHandler        ScrollHandler
	createdHandler       CreatedHandler
	closedHandler        ClosedHandler

	termios  unix.Termios
	terminal bool

	events chan *evdev.InputEvent
	inputs []*evdev.InputDevice

	mouseX int
	mouseY int

	hasCursor    bool
	cursorHandle uint32
	cursorData   []byte
	cursorStride int
	cursorW      int
	cursorH      int

	bg        color.RGBA
	title     string
	textColor color.RGBA
	last      *image.RGBA

	connected atomic.Bool
}

const (
	drmCursorBO   = 0x01
	drmCursorMove = 0x02
	cursorSize    = 64
)

type drmModeCursor struct {
	flags  uint32
	crtcID uint32
	x      int32
	y      int32
	width  uint32
	height uint32
	handle uint32
}

var ioctlModeCursor = ioctl.NewCode(ioctl.Write|ioctl.Read, uint16(unsafe.Sizeof(drmModeCursor{})), drm.IOCTLBase, 0xA3)

func newDRM(opts Options) (*viewDRM, error) {
	v := &viewDRM{}
	var err error

	v.dev, err = drm.OpenCard(opts.Device)
	if err != nil {
		return v, fmt.Errorf("cannot open card: %w", err)
	}

	if !drm.HasDumbBuffer(v.dev) {
		return v, fmt.Errorf("drm device does not support dumb buffers")
	}

	v.connected.Store(true)

	err = v.msInit()
	if err != nil {
		return v, err
	}

	err = v.kbInit()
	if err != nil {
		return v, fmt.Errorf("cannot set termios: %w", err)
	}

	w, h := v.ScreenSize()
	v.mouseX = w / 2
	v.mouseY = h / 2

	v.cursorInit()

	v.events = make(chan *evdev.InputEvent)

	devices, err := v.inputDevices()
	if err != nil {
		return v, fmt.Errorf("cannot get input devices: %w", err)
	}

	for _, path := range devices {
		dev, err := evdev.Open(path)
		if err != nil {
			continue
		}

		v.inputs = append(v.inputs, dev)

		go func(d *evdev.InputDevice) {
			for d != nil {
				if !v.connected.Load() {
					return
				}

				e, err := d.ReadOne()
				if err == nil {
					v.events <- e
				}
			}
		}(dev)
	}

	textColor, err := parseHexColor(opts.TextColor)
	if err != nil {
		return v, fmt.Errorf("cannot parse color: %w", err)
	}

	v.textColor = textColor

	bgColor, err := parseHexColor(opts.BackgroundColor)
	if err != nil {
		return v, fmt.Errorf("cannot parse color: %w", err)
	}

	v.bg = bgColor

	if v.createdHandler != nil {
		v.createdHandler()
	}

	return v, nil
}

func (v *viewDRM) Driver() string {
	return "drm"
}

func (v *viewDRM) Display(ctx context.Context, img image.Image, args ...any) error {
	if len(args) > 0 {
		title, ok := args[0].(string)
		if !ok {
			return fmt.Errorf("invalid argument: %v", args[0])
		}

		if title != "" {
			v.title = title
		}
	}

	src := imageToRGBA(img)
	v.last = src
	for j := range v.msets {
		v.render(&v.msets[j], src)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-v.events:
			if ctx.Err() != nil {
				return nil
			}

			if ev.Type == evdev.EV_REL {
				if ev.Code == evdev.REL_WHEEL {
					if v.scrollHandler != nil {
						if ev.Value == -1 {
							v.scrollHandler(ScrollDown)
						} else if ev.Value == 1 {
							v.scrollHandler(ScrollUp)
						}
					}
				} else if ev.Code == evdev.REL_X || ev.Code == evdev.REL_Y {
					if ev.Code == evdev.REL_X {
						v.mouseX += int(ev.Value)
					} else if ev.Code == evdev.REL_Y {
						v.mouseY += int(ev.Value)
					}

					sw, sh := v.ScreenSize()
					v.mouseX = min(max(v.mouseX, 0), sw-1)
					v.mouseY = min(max(v.mouseY, 0), sh-1)

					v.cursorMove()

					if v.motionHandler != nil {
						v.motionHandler(v.mouseX, v.mouseY)
					}
				}

				continue
			}

			if slices.Contains([]int{evdev.BTN_LEFT, evdev.BTN_MIDDLE, evdev.BTN_RIGHT}, int(ev.Code)) {
				val, ok := buttonMapDRM[int(ev.Code)]
				if !ok {
					continue
				}

				if ev.Value == 1 {
					if v.buttonPressHandler != nil {
						v.buttonPressHandler(val)
					}
				} else if ev.Value == 0 {
					if v.buttonReleaseHandler != nil {
						v.buttonReleaseHandler(val)
					}
				}
			} else {
				val, ok := keyMapDRM[int(ev.Code)]
				if !ok {
					continue
				}

				if ev.Value == 1 || ev.Value == 2 {
					if v.keyPressHandler != nil {
						v.keyPressHandler(val)
					}
				} else if ev.Value == 0 {
					if v.keyReleaseHandler != nil {
						v.keyReleaseHandler(val)
					}
				}
			}
		}
	}

}

func (v *viewDRM) ToggleFullscreen() error {
	return nil
}

func (v *viewDRM) Fullscreen() bool {
	return false
}

func (v *viewDRM) Maximize() error {
	return nil
}

// cursorInit allocates a hardware cursor on the KMS cursor plane, if the driver provides one.
func (v *viewDRM) cursorInit() {
	cw, err := drm.GetCap(v.dev, drm.CapCursorWidth)
	if err != nil || cw == 0 {
		return
	}
	ch, err := drm.GetCap(v.dev, drm.CapCursorHeight)
	if err != nil || ch == 0 {
		return
	}

	w, h := min(cursorSize, int(cw)), min(cursorSize, int(ch))

	fb, err := mode.CreateFB(v.dev, uint16(w), uint16(h), 32)
	if err != nil {
		return
	}

	offset, err := mode.MapDumb(v.dev, fb.Handle)
	if err != nil {
		_ = mode.DestroyDumb(v.dev, fb.Handle)
		return
	}

	data, err := unix.Mmap(int(v.dev.Fd()), int64(offset), int(fb.Size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		_ = mode.DestroyDumb(v.dev, fb.Handle)
		return
	}

	v.cursorHandle = fb.Handle
	v.cursorData = data
	v.cursorStride = int(fb.Pitch)
	v.cursorW, v.cursorH = w, h
	v.hasCursor = true

	v.drawCursor(CursorDefault)
	for j := range v.msets {
		v.cursorIoctl(v.msets[j].mode.Crtc, drmCursorBO, 0, 0)
	}
	v.cursorMove()
}

func (v *viewDRM) cursorIoctl(crtc uint32, flags uint32, x, y int) {
	c := drmModeCursor{flags: flags, crtcID: crtc, x: int32(x), y: int32(y),
		width: uint32(v.cursorW), height: uint32(v.cursorH), handle: v.cursorHandle}

	_ = ioctl.Do(v.dev.Fd(), uintptr(ioctlModeCursor), uintptr(unsafe.Pointer(&c)))
}

func (v *viewDRM) cursorMove() {
	if !v.hasCursor {
		return
	}

	sw, sh := v.ScreenSize()
	x := min(max(v.mouseX, 0), sw-1)
	y := min(max(v.mouseY, 0), sh-1)

	for j := range v.msets {
		v.cursorIoctl(v.msets[j].mode.Crtc, drmCursorMove, x, y)
	}
}

// drawCursor renders the cursor bitmap into the mapped buffer as ARGB8888 (BGRA byte order).
func (v *viewDRM) drawCursor(c Cursor) {
	for i := range v.cursorData {
		v.cursorData[i] = 0
	}

	art := cursorArrow
	if c == CursorGrabbing {
		art = cursorFleur
	}

	for y, row := range art {
		if y >= v.cursorH {
			break
		}
		for x, ch := range row {
			if x >= v.cursorW {
				break
			}

			var px color.RGBA
			switch ch {
			case 'o':
				px = color.RGBA{A: 0xff}
			case '#':
				px = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
			default:
				continue
			}

			o := y*v.cursorStride + x*4
			v.cursorData[o+0] = px.R
			v.cursorData[o+1] = px.G
			v.cursorData[o+2] = px.B
			v.cursorData[o+3] = px.A
		}
	}

	_ = swizzle.BGRA(v.cursorData)
}

func (v *viewDRM) SetCursor(c Cursor) error {
	if !v.hasCursor {
		return nil
	}

	v.drawCursor(c)
	for j := range v.msets {
		v.cursorIoctl(v.msets[j].mode.Crtc, drmCursorBO, 0, 0)
	}

	return nil
}

func (v *viewDRM) Raise() error {
	return nil
}

func (v *viewDRM) SetTitle(title string) error {
	v.title = title

	// The title is drawn into the framebuffer, so repaint to reflect it immediately.
	if v.last != nil {
		for j := range v.msets {
			v.render(&v.msets[j], v.last)
		}
	}

	return nil
}

// render composes into the modeset's reused back buffer, then presents it stride-aware.
func (v *viewDRM) render(ms *mset, src *image.RGBA) {
	w, h := int(ms.mode.Width), int(ms.mode.Height)

	v.fill(ms.back)
	v.blit(ms.back, w, h, src)

	if v.title != "" {
		c := &wlCanvas{data: ms.back, stride: w * 4, w: w, h: h}
		pixfont.DrawString(c, titleMargin, titleMargin, elideTitle(v.title, w-2*titleMargin), v.textColor)
	}

	v.present(ms, w, h)
}

func (v *viewDRM) fill(data []byte) {
	px := [4]byte{v.bg.B, v.bg.G, v.bg.R, 0xff}

	copy(data[0:4], px[:])
	for f := 4; f < len(data); f *= 2 {
		copy(data[f:], data[:f])
	}
}

func (v *viewDRM) blit(data []byte, w, h int, src *image.RGBA) {
	b := src.Bounds()
	iw, ih := b.Dx(), b.Dy()

	dx := (w - iw) / 2
	dy := (h - ih) / 2
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
	if dy+rows > h {
		rows = h - dy
	}

	stride := w * 4
	for y := 0; y < rows; y++ {
		so := y * src.Stride
		do := (dy+y)*stride + dx*4
		row := data[do : do+cols*4]
		copy(row, src.Pix[so:so+cols*4])
		_ = swizzle.BGRA(row)
	}
}

func (v *viewDRM) present(ms *mset, w, h int) {
	stride := int(ms.fb.stride)
	if stride == w*4 {
		copy(ms.fb.data, ms.back)

		return
	}

	for y := 0; y < h; y++ {
		copy(ms.fb.data[y*stride:y*stride+w*4], ms.back[y*w*4:(y+1)*w*4])
	}
}

func (v *viewDRM) SetIcon(image.Image) error {
	return nil
}

func (v *viewDRM) ScreenSize() (int, int) {
	return int(v.msets[0].mode.Width), int(v.msets[0].mode.Height)
}

func (v *viewDRM) WindowSize() (int, int) {
	return v.ScreenSize()
}

func (v *viewDRM) Close() error {
	var e error

	if v.dev == nil {
		return e
	}

	for idx, dev := range v.inputs {
		if dev == nil {
			continue
		}

		if err := dev.Close(); err != nil {
			e = errors.Join(e, err)
		}

		v.inputs[idx] = nil
	}

	if v.terminal {
		if err := unix.IoctlSetTermios(int(os.Stdin.Fd()), unix.TCSETSF, &v.termios); err != nil {
			e = errors.Join(e, err)
		}
	}

	if v.hasCursor {
		handle := v.cursorHandle
		v.cursorHandle = 0 // handle 0 disables the cursor plane

		for j := range v.msets {
			v.cursorIoctl(v.msets[j].mode.Crtc, drmCursorBO, 0, 0)
		}

		_ = unix.Munmap(v.cursorData)
		_ = mode.DestroyDumb(v.dev, handle)
		v.hasCursor = false
	}

	if err := v.fbCleanup(); err != nil {
		e = errors.Join(e, err)
	}

	if err := v.dev.Close(); err != nil {
		e = errors.Join(e, err)
	}

	v.dev = nil

	v.connected.Store(false)

	if v.closedHandler != nil {
		v.closedHandler()
	}

	return e
}

func (v *viewDRM) Clear() {
	for j := range v.msets {
		ms := &v.msets[j]
		v.fill(ms.back)
		v.present(ms, int(ms.mode.Width), int(ms.mode.Height))
	}
}

func (v *viewDRM) SetKeyPressHandler(handler KeyPressHandler) {
	v.keyPressHandler = handler
}

func (v *viewDRM) SetKeyReleaseHandler(handler KeyReleaseHandler) {
	v.keyReleaseHandler = handler
}

func (v *viewDRM) SetButtonPressHandler(handler ButtonPressHandler) {
	v.buttonPressHandler = handler
}

func (v *viewDRM) SetButtonReleaseHandler(handler ButtonReleaseHandler) {
	v.buttonReleaseHandler = handler
}

func (v *viewDRM) SetMotionHandler(handler MotionHandler) {
	v.motionHandler = handler
}

func (v *viewDRM) SetScrollHandler(handler ScrollHandler) {
	v.scrollHandler = handler
}

func (v *viewDRM) SetEnterHandler(handler EnterHandler) {
}

func (v *viewDRM) SetLeaveHandler(handler LeaveHandler) {
}

func (v *viewDRM) SetResizeHandler(handler ResizeHandler) {
}

func (v *viewDRM) SetCreatedHandler(handler CreatedHandler) {
	v.createdHandler = handler
}

func (v *viewDRM) SetClosedHandler(handler ClosedHandler) {
	v.closedHandler = handler
}

func (v *viewDRM) msInit() error {
	res, err := mode.GetResources(v.dev)
	if err != nil {
		return fmt.Errorf("cannot retrieve resources: %w", err)
	}

	for i := 0; i < len(res.Connectors); i++ {
		conn, err := mode.GetConnector(v.dev, res.Connectors[i])
		if err != nil {
			return fmt.Errorf("cannot retrieve connector: %w", err)
		}

		if conn.Connection != mode.Connected || len(conn.Modes) == 0 || conn.EncoderID == 0 {
			continue
		}

		modeset := mode.Modeset{}
		modeset.Conn = conn.ID
		modeset.Mode = conn.Modes[0]
		modeset.Width = conn.Modes[0].Hdisplay
		modeset.Height = conn.Modes[0].Vdisplay

		encoder, err := mode.GetEncoder(v.dev, conn.EncoderID)
		if err != nil {
			return fmt.Errorf("cannot retrieve encoder: %w", err)
		}

		modeset.Crtc = encoder.CrtcID
		v.modesets = append(v.modesets, &modeset)
	}

	v.msets = make([]mset, 0)

	for _, mod := range v.modesets {
		fb, err := v.fbCreate(mod)
		if err != nil {
			_ = v.fbCleanup()

			return fmt.Errorf("cannot create fb: %w", err)
		}

		savedCrtc, err := mode.GetCrtc(v.dev, mod.Crtc)
		if err != nil {
			_ = v.fbCleanup()

			return fmt.Errorf("cannot get crtc: %w", err)
		}

		err = mode.SetCrtc(v.dev, mod.Crtc, fb.id, 0, 0, &mod.Conn, 1, &mod.Mode)
		if err != nil {
			_ = v.fbCleanup()

			return fmt.Errorf("cannot set crtc: %w", err)
		}

		v.msets = append(v.msets, mset{
			mode:      mod,
			fb:        fb,
			savedCrtc: savedCrtc,
			back:      make([]byte, int(mod.Width)*int(mod.Height)*4),
		})
	}

	return nil
}

func (v *viewDRM) kbInit() error {
	v.terminal = true
	termios, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TCGETS)
	if err != nil {
		v.terminal = false
	}

	if v.terminal {
		v.termios = *termios
		termios.Lflag &^= unix.ICANON | unix.ECHO | unix.ISIG

		if err := unix.IoctlSetTermios(int(os.Stdin.Fd()), unix.TCSETS, termios); err != nil {
			return err
		}
	}

	return nil
}

func (v *viewDRM) fbCreate(dev *mode.Modeset) (frameBuffer, error) {
	frameBuf := frameBuffer{}

	fb, err := mode.CreateFB(v.dev, dev.Width, dev.Height, 32)
	if err != nil {
		return frameBuf, err
	}

	stride := fb.Pitch
	size := fb.Size
	handle := fb.Handle

	fbID, err := mode.AddFB(v.dev, dev.Width, dev.Height, 24, 32, stride, handle)
	if err != nil {
		return frameBuf, err
	}

	offset, err := mode.MapDumb(v.dev, handle)
	if err != nil {
		return frameBuf, err
	}

	mm, err := unix.Mmap(int(v.dev.Fd()), int64(offset), int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return frameBuf, err
	}

	frameBuf.id = fbID
	frameBuf.handle = handle
	frameBuf.data = mm
	frameBuf.fb = fb
	frameBuf.size = size
	frameBuf.stride = stride

	return frameBuf, nil
}

func (v *viewDRM) fbDestroy(ms mset) error {
	var e error

	if err := unix.Munmap(ms.fb.data); err != nil {
		e = errors.Join(e, fmt.Errorf("cannot munmap fb data: %w", err))
	}

	if err := mode.RmFB(v.dev, ms.fb.id); err != nil {
		e = errors.Join(e, fmt.Errorf("cannot rm fb: %w", err))
	}

	if err := mode.DestroyDumb(v.dev, ms.fb.handle); err != nil {
		e = errors.Join(e, fmt.Errorf("cannot destroy dumb: %w", err))
	}

	if err := mode.SetCrtc(v.dev, ms.savedCrtc.ID, ms.savedCrtc.BufferID, ms.savedCrtc.X, ms.savedCrtc.Y, &ms.mode.Conn, 1, &ms.savedCrtc.Mode); err != nil {
		e = errors.Join(e, fmt.Errorf("cannot set crtc: %w", err))
	}

	return e
}

func (v *viewDRM) fbCleanup() error {
	var e error

	for _, mset := range v.msets {
		if err := v.fbDestroy(mset); err != nil {
			e = errors.Join(e, err)
		}
	}

	return e
}

func (v *viewDRM) inputDevices() ([]string, error) {
	out := make([]string, 0)

	devices, err := evdev.ListDevicePaths()
	if err != nil {
		return out, err
	}

	for _, dev := range devices {
		d, err := evdev.Open(dev.Path)
		if err != nil {
			return out, err
		}

		for _, t := range d.CapableTypes() {
			if t != evdev.EV_REL && t != evdev.EV_KEY {
				continue
			}

			state, err := d.State(t)
			if err == nil {
				for code, _ := range state {
					if code == evdev.BTN_LEFT || code == evdev.KEY_SPACE {
						out = append(out, dev.Path)
						break
					}
				}
			}
		}

		if err := d.Close(); err != nil {
			return out, err
		}
	}

	return out, nil
}

type frameBuffer struct {
	id     uint32
	handle uint32
	data   []byte
	fb     *mode.FB
	size   uint64
	stride uint32
}

type mset struct {
	mode      *mode.Modeset
	fb        frameBuffer
	savedCrtc *mode.Crtc
	back      []byte
}

var keyMapDRM = map[int]int{
	evdev.KEY_SPACE:      KeySpace,
	evdev.KEY_ESC:        KeyEscape,
	evdev.KEY_ENTER:      KeyEnter,
	evdev.KEY_TAB:        KeyTab,
	evdev.KEY_BACKSPACE:  KeyBackspace,
	evdev.KEY_INSERT:     KeyInsert,
	evdev.KEY_DELETE:     KeyDelete,
	evdev.KEY_RIGHT:      KeyRight,
	evdev.KEY_LEFT:       KeyLeft,
	evdev.KEY_DOWN:       KeyDown,
	evdev.KEY_UP:         KeyUp,
	evdev.KEY_PAGEUP:     KeyPageUp,
	evdev.KEY_PAGEDOWN:   KeyPageDown,
	evdev.KEY_HOME:       KeyHome,
	evdev.KEY_END:        KeyEnd,
	evdev.KEY_CAPSLOCK:   KeyCapsLock,
	evdev.KEY_SCROLLLOCK: KeyScrollLock,
	evdev.KEY_NUMLOCK:    KeyNumLock,
	evdev.KEY_PRINT:      KeyPrintScreen,
	evdev.KEY_PAUSE:      KeyPause,
	evdev.KEY_F1:         KeyF1,
	evdev.KEY_F2:         KeyF2,
	evdev.KEY_F3:         KeyF3,
	evdev.KEY_F4:         KeyF4,
	evdev.KEY_F5:         KeyF5,
	evdev.KEY_F6:         KeyF6,
	evdev.KEY_F7:         KeyF7,
	evdev.KEY_F8:         KeyF8,
	evdev.KEY_F9:         KeyF9,
	evdev.KEY_F10:        KeyF10,
	evdev.KEY_F11:        KeyF11,
	evdev.KEY_F12:        KeyF12,
	evdev.KEY_LEFTSHIFT:  KeyLeftShift,
	evdev.KEY_LEFTCTRL:   KeyLeftControl,
	evdev.KEY_LEFTALT:    KeyLeftAlt,
	evdev.KEY_LEFTMETA:   KeyLeftSuper,
	evdev.KEY_RIGHTSHIFT: KeyRightShift,
	evdev.KEY_RIGHTCTRL:  KeyRightControl,
	evdev.KEY_RIGHTALT:   KeyRightAlt,
	evdev.KEY_RIGHTMETA:  KeyRightSuper,
	evdev.KEY_LEFTBRACE:  KeyLeftBracket,
	evdev.KEY_BACKSLASH:  KeyBackSlash,
	evdev.KEY_RIGHTBRACE: KeyRightBracket,
	evdev.KEY_GRAVE:      KeyGrave,
	evdev.KEY_NUMERIC_0:  KeyKp0,
	evdev.KEY_NUMERIC_1:  KeyKp1,
	evdev.KEY_NUMERIC_2:  KeyKp2,
	evdev.KEY_NUMERIC_3:  KeyKp3,
	evdev.KEY_NUMERIC_4:  KeyKp4,
	evdev.KEY_NUMERIC_5:  KeyKp5,
	evdev.KEY_NUMERIC_6:  KeyKp6,
	evdev.KEY_NUMERIC_7:  KeyKp7,
	evdev.KEY_NUMERIC_8:  KeyKp8,
	evdev.KEY_NUMERIC_9:  KeyKp9,
	evdev.KEY_KPDOT:      KeyKpDecimal,
	evdev.KEY_KPSLASH:    KeyKpDivide,
	evdev.KEY_KPASTERISK: KeyKpMultiply,
	evdev.KEY_KPMINUS:    KeyKpSubtract,
	evdev.KEY_KPPLUS:     KeyKpAdd,
	evdev.KEY_KPENTER:    KeyKpEnter,
	evdev.KEY_APOSTROPHE: KeyApostrophe,
	evdev.KEY_COMMA:      KeyComma,
	evdev.KEY_MINUS:      KeyMinus,
	evdev.KEY_DOT:        KeyPeriod,
	evdev.KEY_SLASH:      KeySlash,
	evdev.KEY_0:          Key0,
	evdev.KEY_1:          Key1,
	evdev.KEY_2:          Key2,
	evdev.KEY_3:          Key3,
	evdev.KEY_4:          Key4,
	evdev.KEY_5:          Key5,
	evdev.KEY_6:          Key6,
	evdev.KEY_7:          Key7,
	evdev.KEY_8:          Key8,
	evdev.KEY_9:          Key9,
	evdev.KEY_SEMICOLON:  KeySemicolon,
	evdev.KEY_EQUAL:      KeyEqual,
	evdev.KEY_A:          KeyA,
	evdev.KEY_B:          KeyB,
	evdev.KEY_C:          KeyC,
	evdev.KEY_D:          KeyD,
	evdev.KEY_E:          KeyE,
	evdev.KEY_F:          KeyF,
	evdev.KEY_G:          KeyG,
	evdev.KEY_H:          KeyH,
	evdev.KEY_I:          KeyI,
	evdev.KEY_J:          KeyJ,
	evdev.KEY_K:          KeyK,
	evdev.KEY_L:          KeyL,
	evdev.KEY_M:          KeyM,
	evdev.KEY_N:          KeyN,
	evdev.KEY_O:          KeyO,
	evdev.KEY_P:          KeyP,
	evdev.KEY_Q:          KeyQ,
	evdev.KEY_R:          KeyR,
	evdev.KEY_S:          KeyS,
	evdev.KEY_T:          KeyT,
	evdev.KEY_U:          KeyU,
	evdev.KEY_V:          KeyV,
	evdev.KEY_W:          KeyW,
	evdev.KEY_X:          KeyX,
	evdev.KEY_Y:          KeyY,
	evdev.KEY_Z:          KeyZ,
}

var buttonMapDRM = map[int]int{
	evdev.BTN_LEFT:   ButtonLeft,
	evdev.BTN_MIDDLE: ButtonMiddle,
	evdev.BTN_RIGHT:  ButtonRight,
}

// Cursor bitmaps: o = black outline, # = white fill, space = transparent.
var cursorArrow = []string{
	"o",
	"oo",
	"o#o",
	"o##o",
	"o###o",
	"o####o",
	"o#####o",
	"o######o",
	"o#######o",
	"o########o",
	"o#####oooo",
	"o##o##o",
	"o#o o##o",
	"oo  o##o",
	"o    o##o",
	"      oo",
}

var cursorFleur = []string{
	"        o",
	"       o#o",
	"      o###o",
	"       o#o",
	"       o#o",
	"       o#o",
	"  o    o#o    o",
	" o#oooo###oooo#o",
	"o###############o",
	" o#oooo###oooo#o",
	"  o    o#o    o",
	"       o#o",
	"       o#o",
	"       o#o",
	"      o###o",
	"       o#o",
	"        o",
}
