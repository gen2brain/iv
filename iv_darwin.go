//go:build darwin && !x11

package iv

import (
	"context"
	"fmt"
	"image"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

type NSPoint struct {
	X, Y float64
}

type NSSize struct {
	Width, Height float64
}

type NSRect struct {
	Origin NSPoint
	Size   NSSize
}

const (
	nsApplicationActivationPolicyRegular = 0
	nsWindowStyleMaskTitled              = 1 << 0
	nsWindowStyleMaskClosable            = 1 << 1
	nsWindowStyleMaskMiniaturizable      = 1 << 2
	nsWindowStyleMaskResizable           = 1 << 3
	nsBackingStoreBuffered               = 2

	nsImageScaleNone   = 2
	nsImageAlignCenter = 0
	nsImageCacheNever  = 3

	nsEventMaskAny = ^uint(0)

	nsEventTypeLeftMouseDown  = 1
	nsEventTypeLeftMouseUp    = 2
	nsEventTypeRightMouseDown = 3
	nsEventTypeRightMouseUp   = 4
	nsEventTypeMouseMoved     = 5
	nsEventTypeKeyDown        = 10
	nsEventTypeKeyUp          = 11
	nsEventTypeFlagsChanged   = 12
	nsEventTypeScrollWheel    = 22
	nsEventTypeOtherMouseDown = 25
	nsEventTypeOtherMouseUp   = 26

	nsEventModifierFlagShift   = 1 << 17
	nsEventModifierFlagControl = 1 << 18
	nsEventModifierFlagOption  = 1 << 19
	nsEventModifierFlagCommand = 1 << 20
)

var (
	selSharedApplication     = objc.RegisterName("sharedApplication")
	selSetActivationPolicy   = objc.RegisterName("setActivationPolicy:")
	selActivateIgnoring      = objc.RegisterName("activateIgnoringOtherApps:")
	selFinishLaunching       = objc.RegisterName("finishLaunching")
	selAlloc                 = objc.RegisterName("alloc")
	selInit                  = objc.RegisterName("init")
	selNew                   = objc.RegisterName("new")
	selRelease               = objc.RegisterName("release")
	selDrain                 = objc.RegisterName("drain")
	selInitWindow            = objc.RegisterName("initWithContentRect:styleMask:backing:defer:")
	selSetTitle              = objc.RegisterName("setTitle:")
	selMakeKeyAndOrderFront  = objc.RegisterName("makeKeyAndOrderFront:")
	selOrderOut              = objc.RegisterName("orderOut:")
	selCenter                = objc.RegisterName("center")
	selClose                 = objc.RegisterName("close")
	selSetReleasedWhenClosed = objc.RegisterName("setReleasedWhenClosed:")
	selSetBackgroundColor    = objc.RegisterName("setBackgroundColor:")
	selSetContentView        = objc.RegisterName("setContentView:")
	selSetDelegate           = objc.RegisterName("setDelegate:")
	selSetAcceptsMouseMoved  = objc.RegisterName("setAcceptsMouseMovedEvents:")
	selToggleFullScreen      = objc.RegisterName("toggleFullScreen:")
	selBackingScaleFactor    = objc.RegisterName("backingScaleFactor")
	selFrame                 = objc.RegisterName("frame")
	selConvertSizeToBacking  = objc.RegisterName("convertSizeToBacking:")
	selNextEvent             = objc.RegisterName("nextEventMatchingMask:untilDate:inMode:dequeue:")
	selSendEvent             = objc.RegisterName("sendEvent:")
	selType                  = objc.RegisterName("type")
	selKeyCode               = objc.RegisterName("keyCode")
	selModifierFlags         = objc.RegisterName("modifierFlags")
	selScrollingDeltaY       = objc.RegisterName("scrollingDeltaY")
	selLocationInWindow      = objc.RegisterName("locationInWindow")
	selInitWithUTF8          = objc.RegisterName("initWithUTF8String:")
	selDateWithInterval      = objc.RegisterName("dateWithTimeIntervalSinceNow:")
	selColorSRGB             = objc.RegisterName("colorWithSRGBRed:green:blue:alpha:")
	selSetImageScaling       = objc.RegisterName("setImageScaling:")
	selSetImageAlignment     = objc.RegisterName("setImageAlignment:")
	selSetImage              = objc.RegisterName("setImage:")
	selSetAppIconImage       = objc.RegisterName("setApplicationIconImage:")
	selSetCacheMode          = objc.RegisterName("setCacheMode:")
	selInitBitmap            = objc.RegisterName("initWithBitmapDataPlanes:pixelsWide:pixelsHigh:bitsPerSample:samplesPerPixel:hasAlpha:isPlanar:colorSpaceName:bytesPerRow:bitsPerPixel:")
	selBitmapData            = objc.RegisterName("bitmapData")
	selAddRepresentation     = objc.RegisterName("addRepresentation:")
	selInitWithSize          = objc.RegisterName("initWithSize:")
	selMainScreen            = objc.RegisterName("mainScreen")
	selWindowWillClose       = objc.RegisterName("windowWillClose:")
	selWindowDidResize       = objc.RegisterName("windowDidResize:")
)

var (
	delegateClass objc.Class
	delegateOnce  sync.Once
	registry      = map[objc.ID]*View{}
)

// View type.
type View struct {
	app       objc.ID
	window    objc.ID
	imageView objc.ID
	delegate  objc.ID

	mode      objc.ID
	deviceRGB objc.ID

	scale               float64
	winWidth, winHeight int

	running     bool
	created     bool
	closed      bool
	closedFired bool
	fullscreen  bool

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

func ensureDelegateClass() {
	delegateOnce.Do(func() {
		c, err := objc.RegisterClass("ivWindowDelegate", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
			{
				Cmd: selWindowWillClose,
				Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) {
					if v := registry[self]; v != nil {
						v.onClose()
					}
				},
			},
			{
				Cmd: selWindowDidResize,
				Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) {
					if v := registry[self]; v != nil {
						v.onResize()
					}
				},
			},
		})
		if err == nil {
			delegateClass = c
		}
	})
}

// New returns new View.
func New(args ...any) (*View, error) {
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

	if _, err := purego.Dlopen("/System/Library/Frameworks/Cocoa.framework/Cocoa", purego.RTLD_GLOBAL|purego.RTLD_LAZY); err != nil {
		return v, fmt.Errorf("cannot load Cocoa: %w", err)
	}

	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selAlloc).Send(selInit)
	defer pool.Send(selDrain)

	v.mode = objc.ID(objc.GetClass("NSString")).Send(selAlloc).Send(selInitWithUTF8, "kCFRunLoopDefaultMode\x00")
	v.deviceRGB = objc.ID(objc.GetClass("NSString")).Send(selAlloc).Send(selInitWithUTF8, "NSDeviceRGBColorSpace\x00")

	v.app = objc.ID(objc.GetClass("NSApplication")).Send(selSharedApplication)
	v.app.Send(selSetActivationPolicy, nsApplicationActivationPolicyRegular)

	style := nsWindowStyleMaskTitled | nsWindowStyleMaskClosable | nsWindowStyleMaskMiniaturizable | nsWindowStyleMaskResizable
	rect := NSRect{Size: NSSize{Width: float64(opts.Width), Height: float64(opts.Height)}}

	v.window = objc.ID(objc.GetClass("NSWindow")).Send(selAlloc)
	v.window = v.window.Send(selInitWindow, rect, uint(style), nsBackingStoreBuffered, false)
	v.window.Send(selSetReleasedWhenClosed, false)
	v.window.Send(selSetAcceptsMouseMoved, true)

	col, err := parseHexColor(opts.BackgroundColor)
	if err != nil {
		return v, fmt.Errorf("cannot parse color: %w", err)
	}
	bg := objc.ID(objc.GetClass("NSColor")).Send(selColorSRGB,
		float64(col.R)/255, float64(col.G)/255, float64(col.B)/255, 1.0)
	v.window.Send(selSetBackgroundColor, bg)

	v.imageView = objc.ID(objc.GetClass("NSImageView")).Send(selAlloc).Send(selInit)
	v.imageView.Send(selSetImageScaling, nsImageScaleNone)
	v.imageView.Send(selSetImageAlignment, nsImageAlignCenter)
	v.window.Send(selSetContentView, v.imageView)

	ensureDelegateClass()
	v.delegate = objc.ID(delegateClass).Send(selNew)
	registry[v.delegate] = v
	v.window.Send(selSetDelegate, v.delegate)

	v.setWindowTitle(opts.AppID)
	v.window.Send(selCenter)
	v.app.Send(selActivateIgnoring, true)
	v.window.Send(selMakeKeyAndOrderFront, objc.ID(0))
	v.app.Send(selFinishLaunching)

	v.scale = objc.Send[float64](v.window, selBackingScaleFactor)
	v.updateSize()

	return v, nil
}

// Driver returns the name of the display driver.
func (v *View) Driver() string {
	return "cocoa"
}

// Display displays image, optional argument is the title of the window.
func (v *View) Display(ctx context.Context, img image.Image, args ...any) error {
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

	v.setImage(img)

	if !v.created {
		v.created = true
		if v.createdHandler != nil {
			v.createdHandler()
		}
	}

	if v.running {
		return nil
	}

	v.running = true
	defer func() {
		v.running = false
	}()

	for {
		if ctx.Err() != nil || v.closed {
			return nil
		}

		pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selAlloc).Send(selInit)

		date := objc.ID(objc.GetClass("NSDate")).Send(selDateWithInterval, 0.008)
		ev := v.app.Send(selNextEvent, nsEventMaskAny, date, v.mode, true)
		if ev != 0 {
			if !v.handleEvent(ev) {
				v.app.Send(selSendEvent, ev)
			}
		}

		pool.Send(selDrain)
	}
}

// handleEvent dispatches an NSEvent and reports whether it was consumed.
func (v *View) handleEvent(ev objc.ID) bool {
	switch int(ev.Send(selType)) {
	case nsEventTypeKeyDown:
		if v.keyPressHandler != nil {
			v.keyPressHandler(int(ev.Send(selKeyCode)) & 0xffff)
		}
		return true
	case nsEventTypeKeyUp:
		if v.keyReleaseHandler != nil {
			v.keyReleaseHandler(int(ev.Send(selKeyCode)) & 0xffff)
		}
		return true
	case nsEventTypeFlagsChanged:
		kc := int(ev.Send(selKeyCode)) & 0xffff
		flags := uint(ev.Send(selModifierFlags))
		if flag := modifierFlag(kc); flag != 0 {
			if flags&flag != 0 {
				if v.keyPressHandler != nil {
					v.keyPressHandler(kc)
				}
			} else if v.keyReleaseHandler != nil {
				v.keyReleaseHandler(kc)
			}
		}
		return true
	case nsEventTypeLeftMouseDown:
		v.button(v.buttonPressHandler, ButtonLeft)
	case nsEventTypeLeftMouseUp:
		v.button(v.buttonReleaseHandler, ButtonLeft)
	case nsEventTypeRightMouseDown:
		v.button(v.buttonPressHandler, ButtonRight)
	case nsEventTypeRightMouseUp:
		v.button(v.buttonReleaseHandler, ButtonRight)
	case nsEventTypeOtherMouseDown:
		v.button(v.buttonPressHandler, ButtonMiddle)
	case nsEventTypeOtherMouseUp:
		v.button(v.buttonReleaseHandler, ButtonMiddle)
	case nsEventTypeScrollWheel:
		if v.scrollHandler != nil {
			dy := objc.Send[float64](ev, selScrollingDeltaY)
			if dy > 0 {
				v.scrollHandler(ScrollUp)
			} else if dy < 0 {
				v.scrollHandler(ScrollDown)
			}
		}
	case nsEventTypeMouseMoved:
		if v.motionHandler != nil {
			p := objc.Send[NSPoint](ev, selLocationInWindow)
			v.motionHandler(int(p.X), int(p.Y))
		}
	}

	return false
}

func (v *View) button(handler func(int), button int) {
	if handler != nil {
		handler(button)
	}
}

func modifierFlag(keyCode int) uint {
	switch keyCode {
	case KeyLeftShift, KeyRightShift:
		return nsEventModifierFlagShift
	case KeyLeftControl, KeyRightControl:
		return nsEventModifierFlagControl
	case KeyLeftAlt, KeyRightAlt:
		return nsEventModifierFlagOption
	case KeyLeftSuper, KeyRightSuper:
		return nsEventModifierFlagCommand
	}

	return 0
}

func (v *View) setImage(img image.Image) {
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selAlloc).Send(selInit)
	defer pool.Send(selDrain)

	src := imageToRGBA(img)
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()

	rep := objc.ID(objc.GetClass("NSBitmapImageRep")).Send(selAlloc)
	rep = rep.Send(selInitBitmap, uintptr(0), w, h, 8, 4, true, false, v.deviceRGB, w*4, 32)

	data := objc.Send[unsafe.Pointer](rep, selBitmapData)
	dst := unsafe.Slice((*byte)(data), w*h*4)
	for y := 0; y < h; y++ {
		so := y * src.Stride
		copy(dst[y*w*4:(y+1)*w*4], src.Pix[so:so+w*4])
	}

	size := NSSize{Width: float64(w) / v.scale, Height: float64(h) / v.scale}
	nsImage := objc.ID(objc.GetClass("NSImage")).Send(selAlloc).Send(selInitWithSize, size)
	nsImage.Send(selSetCacheMode, nsImageCacheNever)
	nsImage.Send(selAddRepresentation, rep)

	v.imageView.Send(selSetImage, nsImage)

	nsImage.Send(selRelease)
	rep.Send(selRelease)
}

func (v *View) setWindowTitle(title string) {
	ns := objc.ID(objc.GetClass("NSString")).Send(selAlloc).Send(selInitWithUTF8, title+"\x00")
	v.window.Send(selSetTitle, ns)
	ns.Send(selRelease)
}

func (v *View) updateSize() {
	frame := objc.Send[NSRect](v.imageView, selFrame)
	backing := objc.Send[NSSize](v.imageView, selConvertSizeToBacking, frame.Size)
	v.winWidth = int(backing.Width)
	v.winHeight = int(backing.Height)
}

func (v *View) onResize() {
	v.scale = objc.Send[float64](v.window, selBackingScaleFactor)
	v.updateSize()

	if v.resizeHandler != nil {
		v.resizeHandler(v.winWidth, v.winHeight)
	}
}

func (v *View) onClose() {
	v.closed = true

	if v.closedHandler != nil && !v.closedFired {
		v.closedFired = true
		v.closedHandler()
	}
}

// ToggleFullscreen toggles window fullscreen.
func (v *View) ToggleFullscreen() error {
	v.window.Send(selToggleFullScreen, objc.ID(0))
	v.fullscreen = !v.fullscreen

	return nil
}

// Fullscreen returns current fullscreen state.
func (v *View) Fullscreen() bool {
	return v.fullscreen
}

// SetTitle sets window title.
func (v *View) SetTitle(title string) error {
	v.setWindowTitle(title)

	return nil
}

// Raise brings the window to the front.
func (v *View) Raise() error {
	v.app.Send(selActivateIgnoring, true)
	v.window.Send(selMakeKeyAndOrderFront, objc.ID(0))

	return nil
}

// SetIcon sets the application (Dock) icon. macOS has no per-window icon.
func (v *View) SetIcon(img image.Image) error {
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selAlloc).Send(selInit)
	defer pool.Send(selDrain)

	src := imageToRGBA(img)
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()

	rep := objc.ID(objc.GetClass("NSBitmapImageRep")).Send(selAlloc)
	rep = rep.Send(selInitBitmap, uintptr(0), w, h, 8, 4, true, false, v.deviceRGB, w*4, 32)

	data := objc.Send[unsafe.Pointer](rep, selBitmapData)
	dst := unsafe.Slice((*byte)(data), w*h*4)
	for y := 0; y < h; y++ {
		so := y * src.Stride
		copy(dst[y*w*4:(y+1)*w*4], src.Pix[so:so+w*4])
	}

	size := NSSize{Width: float64(w), Height: float64(h)}
	nsImage := objc.ID(objc.GetClass("NSImage")).Send(selAlloc).Send(selInitWithSize, size)
	nsImage.Send(selAddRepresentation, rep)

	v.app.Send(selSetAppIconImage, nsImage)

	nsImage.Send(selRelease)
	rep.Send(selRelease)

	return nil
}

// ScreenSize returns screen dimensions.
func (v *View) ScreenSize() (int, int) {
	screen := objc.ID(objc.GetClass("NSScreen")).Send(selMainScreen)
	if screen == 0 {
		return 0, 0
	}

	frame := objc.Send[NSRect](screen, selFrame)

	return int(frame.Size.Width * v.scale), int(frame.Size.Height * v.scale)
}

// WindowSize returns window dimensions.
func (v *View) WindowSize() (int, int) {
	return v.winWidth, v.winHeight
}

// Close closes the window.
func (v *View) Close() error {
	if v.window == 0 {
		return nil
	}

	v.closed = true

	v.window.Send(selOrderOut, objc.ID(0))
	v.window.Send(selClose)

	delete(registry, v.delegate)

	if v.delegate != 0 {
		v.delegate.Send(selRelease)
		v.delegate = 0
	}

	v.window.Send(selRelease)
	v.window = 0

	v.imageView.Send(selRelease)
	v.mode.Send(selRelease)
	v.deviceRGB.Send(selRelease)
	v.imageView, v.mode, v.deviceRGB = 0, 0, 0

	if v.closedHandler != nil && !v.closedFired {
		v.closedFired = true
		v.closedHandler()
	}

	return nil
}

// Clear clears the window contents.
func (v *View) Clear() {
	if v.imageView != 0 {
		v.imageView.Send(selSetImage, objc.ID(0))
	}
}

// SetKeyPressHandler sets key press handler.
func (v *View) SetKeyPressHandler(handler KeyPressHandler) {
	v.keyPressHandler = handler
}

// SetKeyReleaseHandler sets key release handler.
func (v *View) SetKeyReleaseHandler(handler KeyReleaseHandler) {
	v.keyReleaseHandler = handler
}

// SetButtonPressHandler sets mouse button press handler.
func (v *View) SetButtonPressHandler(handler ButtonPressHandler) {
	v.buttonPressHandler = handler
}

// SetButtonReleaseHandler sets mouse button release handler.
func (v *View) SetButtonReleaseHandler(handler ButtonReleaseHandler) {
	v.buttonReleaseHandler = handler
}

// SetMotionHandler sets mouse motion handler.
func (v *View) SetMotionHandler(handler MotionHandler) {
	v.motionHandler = handler
}

// SetScrollHandler sets mouse scroll handler.
func (v *View) SetScrollHandler(handler ScrollHandler) {
	v.scrollHandler = handler
}

// SetEnterHandler sets mouse enter handler.
func (v *View) SetEnterHandler(handler EnterHandler) {
	v.enterHandler = handler
}

// SetLeaveHandler sets mouse leave handler.
func (v *View) SetLeaveHandler(handler LeaveHandler) {
	v.leaveHandler = handler
}

// SetResizeHandler sets window resize handler.
func (v *View) SetResizeHandler(handler ResizeHandler) {
	v.resizeHandler = handler
}

// SetCreatedHandler sets window created handler.
func (v *View) SetCreatedHandler(handler CreatedHandler) {
	v.createdHandler = handler
}

// SetClosedHandler sets window closed handler.
func (v *View) SetClosedHandler(handler ClosedHandler) {
	v.closedHandler = handler
}

const (
	KeyA            = 0x00
	KeyS            = 0x01
	KeyD            = 0x02
	KeyF            = 0x03
	KeyH            = 0x04
	KeyG            = 0x05
	KeyZ            = 0x06
	KeyX            = 0x07
	KeyC            = 0x08
	KeyV            = 0x09
	KeyB            = 0x0B
	KeyQ            = 0x0C
	KeyW            = 0x0D
	KeyE            = 0x0E
	KeyR            = 0x0F
	KeyY            = 0x10
	KeyT            = 0x11
	Key1            = 0x12
	Key2            = 0x13
	Key3            = 0x14
	Key4            = 0x15
	Key6            = 0x16
	Key5            = 0x17
	KeyEqual        = 0x18
	Key9            = 0x19
	Key7            = 0x1A
	KeyMinus        = 0x1B
	Key8            = 0x1C
	Key0            = 0x1D
	KeyRightBracket = 0x1E
	KeyO            = 0x1F
	KeyU            = 0x20
	KeyLeftBracket  = 0x21
	KeyI            = 0x22
	KeyP            = 0x23
	KeyEnter        = 0x24
	KeyL            = 0x25
	KeyJ            = 0x26
	KeyApostrophe   = 0x27
	KeyK            = 0x28
	KeySemicolon    = 0x29
	KeyBackSlash    = 0x2A
	KeyComma        = 0x2B
	KeySlash        = 0x2C
	KeyN            = 0x2D
	KeyM            = 0x2E
	KeyPeriod       = 0x2F
	KeyTab          = 0x30
	KeySpace        = 0x31
	KeyGrave        = 0x32
	KeyBackspace    = 0x33
	KeyEscape       = 0x35
	KeyRightSuper   = 0x36
	KeyLeftSuper    = 0x37
	KeyLeftShift    = 0x38
	KeyCapsLock     = 0x39
	KeyLeftAlt      = 0x3A
	KeyLeftControl  = 0x3B
	KeyRightShift   = 0x3C
	KeyRightAlt     = 0x3D
	KeyRightControl = 0x3E
	KeyKpDecimal    = 0x41
	KeyKpMultiply   = 0x43
	KeyKpAdd        = 0x45
	KeyKpDivide     = 0x4B
	KeyKpEnter      = 0x4C
	KeyKpSubtract   = 0x4E
	KeyKp0          = 0x52
	KeyKp1          = 0x53
	KeyKp2          = 0x54
	KeyKp3          = 0x55
	KeyKp4          = 0x56
	KeyKp5          = 0x57
	KeyKp6          = 0x58
	KeyKp7          = 0x59
	KeyKp8          = 0x5B
	KeyKp9          = 0x5C
	KeyF5           = 0x60
	KeyF6           = 0x61
	KeyF7           = 0x62
	KeyF3           = 0x63
	KeyF8           = 0x64
	KeyF9           = 0x65
	KeyF11          = 0x67
	KeyF10          = 0x6D
	KeyF12          = 0x6F
	KeyHome         = 0x73
	KeyPageUp       = 0x74
	KeyDelete       = 0x75
	KeyF4           = 0x76
	KeyEnd          = 0x77
	KeyF2           = 0x78
	KeyPageDown     = 0x79
	KeyF1           = 0x7A
	KeyLeft         = 0x7B
	KeyRight        = 0x7C
	KeyDown         = 0x7D
	KeyUp           = 0x7E

	KeyInsert      = 0xFFF0
	KeyScrollLock  = 0xFFF1
	KeyNumLock     = 0xFFF2
	KeyPrintScreen = 0xFFF3
	KeyPause       = 0xFFF4

	ButtonLeft   = 1
	ButtonMiddle = 2
	ButtonRight  = 3

	ScrollUp   = 4
	ScrollDown = 5
)
