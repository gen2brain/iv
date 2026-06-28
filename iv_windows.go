//go:build windows && !x11

package iv

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"unsafe"

	"github.com/tailscale/win"
	"golang.org/x/sys/windows"

	"github.com/gen2brain/iv/internal/swizzle"
)

var (
	_gdi32            *windows.LazyDLL
	_createSolidBrush *windows.LazyProc
)

func init() {
	_gdi32 = windows.NewLazySystemDLL("gdi32.dll")
	_createSolidBrush = _gdi32.NewProc("CreateSolidBrush")
}

// View type.
type View struct {
	hwnd          win.HWND
	hBitmap       win.HBITMAP
	hIcon         win.HICON
	winPlacement  *win.WINDOWPLACEMENT
	lpszClassName *uint16
	background    win.HBRUSH

	memDC      win.HDC
	memBmp     win.HBITMAP
	memOld     win.HGDIOBJ
	srcDC      win.HDC
	memW, memH int32

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

	mouseIn                   bool
	imgWidth, imgHeight       int
	winWidth, winHeight       int
	screenWidth, screenHeight int
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

	hInstance := win.GetModuleHandle(nil)
	if hInstance == 0 {
		return v, fmt.Errorf("cannot get module handle")
	}

	lpszClassName, err := windows.UTF16PtrFromString(opts.AppID)
	if err != nil {
		return v, err
	}
	v.lpszClassName = lpszClassName

	col, err := parseHexColor(opts.BackgroundColor)
	if err != nil {
		return v, fmt.Errorf("cannot parse color: %w", err)
	}
	v.background = createSolidBrush(win.RGB(col.R, col.G, col.B))

	var wc win.WNDCLASSEX
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.LpfnWndProc = windows.NewCallback(v.wndProc)
	wc.HInstance = hInstance
	wc.HIcon = 0
	wc.HCursor = win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_ARROW))
	wc.HbrBackground = v.background
	wc.LpszClassName = v.lpszClassName
	wc.Style = win.CS_HREDRAW | win.CS_VREDRAW

	if atom := win.RegisterClassEx(&wc); atom == 0 {
		return v, fmt.Errorf("cannot register class")
	}

	v.screenWidth = int(win.GetSystemMetrics(win.SM_CXSCREEN))
	v.screenHeight = int(win.GetSystemMetrics(win.SM_CYSCREEN))

	var b win.RECT
	win.SetRect(&b, 0, 0, uint32(opts.Width), uint32(opts.Height))
	win.AdjustWindowRect(&b, win.WS_OVERLAPPEDWINDOW|win.WS_VISIBLE, false)
	x := (v.screenWidth - int(b.Right-b.Left)) / 2
	y := (v.screenHeight - int(b.Bottom-b.Top)) / 2

	var wName *uint16
	v.hwnd = win.CreateWindowEx(0, lpszClassName, wName, win.WS_OVERLAPPEDWINDOW|win.WS_VISIBLE,
		int32(x), int32(y), int32(opts.Width), int32(opts.Height), 0, 0, hInstance, nil)
	if v.hwnd == 0 {
		return v, fmt.Errorf("cannot create window")
	}

	win.SetTimer(v.hwnd, 313, 10, 0)

	bringToForeground(v.hwnd)

	return v, nil
}

// bringToForeground raises and activates the window; a synthetic Alt lifts the foreground lock.
func bringToForeground(hwnd win.HWND) {
	if hwnd == 0 || win.GetForegroundWindow() == hwnd {
		return
	}

	if win.IsIconic(hwnd) {
		win.ShowWindow(hwnd, win.SW_RESTORE)
	}

	inputs := [2]win.KEYBD_INPUT{
		{Type: win.INPUT_KEYBOARD, Ki: win.KEYBDINPUT{WVk: win.VK_MENU}},
		{Type: win.INPUT_KEYBOARD, Ki: win.KEYBDINPUT{WVk: win.VK_MENU, DwFlags: win.KEYEVENTF_KEYUP}},
	}
	win.SendInput(2, unsafe.Pointer(&inputs[0]), int32(unsafe.Sizeof(inputs[0])))

	win.SetForegroundWindow(hwnd)
}

// Driver returns the name of the display driver.
func (v *View) Driver() string {
	return "win32"
}

// Display displays image, optional argument is the title of the window.
func (v *View) Display(ctx context.Context, img image.Image, args ...any) error {
	if len(args) > 0 && args[0].(string) != "" {
		title, ok := args[0].(string)
		if !ok {
			return fmt.Errorf("invalid argument: %v", args[0])
		}

		if title != "" {
			err := v.SetTitle(title)
			if err != nil {
				return err
			}
		}
	}

	hB, err := hBitmapFromImage(img, 96)
	if err != nil {
		return fmt.Errorf("cannot get bitmap from image: %w", err)
	}

	old := v.hBitmap
	v.hBitmap = hB
	v.imgWidth = img.Bounds().Dx()
	v.imgHeight = img.Bounds().Dy()

	if old != 0 {
		win.DeleteObject(win.HGDIOBJ(old))
	}

	win.InvalidateRect(v.hwnd, nil, false)
	win.UpdateWindow(v.hwnd)

	var msg win.MSG
	for v.hwnd != 0 && win.GetMessage(&msg, v.hwnd, 0, 0) > 0 {
		if err := ctx.Err(); err != nil {
			return nil
		}

		win.DispatchMessage(&msg)
	}

	return nil
}

// ToggleFullscreen toggles window fullscreen.
func (v *View) ToggleFullscreen() error {
	if err := v.setFullscreen(!v.getFullscreen()); err != nil {
		return fmt.Errorf("cannot toggle fullscreen: %w", err)
	}

	return nil
}

// Fullscreen returns current fullscreen state.
func (v *View) Fullscreen() bool {
	return v.getFullscreen()
}

// Maximize maximizes the window to the available work area.
func (v *View) Maximize() error {
	win.ShowWindow(v.hwnd, win.SW_SHOWMAXIMIZED)

	return nil
}

// SetTitle sets window title.
func (v *View) SetTitle(title string) error {
	ptr, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return err
	}

	ret := win.SendMessage(v.hwnd, win.WM_SETTEXT, 0, uintptr(unsafe.Pointer(ptr)))
	if ret == 0 {
		return fmt.Errorf("cannot set title")
	}

	return nil
}

// Raise brings the window to the front.
func (v *View) Raise() error {
	bringToForeground(v.hwnd)

	return nil
}

// SetIcon sets the window icon.
func (v *View) SetIcon(img image.Image) error {
	color, err := hBitmapFromImage(img, 96)
	if err != nil {
		return err
	}
	defer win.DeleteObject(win.HGDIOBJ(color))

	b := img.Bounds()
	mask := win.CreateBitmap(int32(b.Dx()), int32(b.Dy()), 1, 1, nil)
	if mask == 0 {
		return fmt.Errorf("cannot create icon mask")
	}
	defer win.DeleteObject(win.HGDIOBJ(mask))

	ii := win.ICONINFO{FIcon: 1, HbmMask: mask, HbmColor: color}
	hIcon := win.CreateIconIndirect(&ii)
	if hIcon == 0 {
		return fmt.Errorf("cannot create icon")
	}

	win.SendMessage(v.hwnd, win.WM_SETICON, uintptr(win.ICON_SMALL), uintptr(hIcon))
	win.SendMessage(v.hwnd, win.WM_SETICON, uintptr(win.ICON_BIG), uintptr(hIcon))

	if v.hIcon != 0 {
		win.DestroyIcon(v.hIcon)
	}
	v.hIcon = hIcon

	return nil
}

// ScreenSize returns screen dimensions.
func (v *View) ScreenSize() (int, int) {
	return v.screenWidth, v.screenHeight
}

// WindowSize returns window dimensions.
func (v *View) WindowSize() (int, int) {
	return v.winWidth, v.winHeight
}

// Close closes the window.
func (v *View) Close() error {
	var e error

	if v.hwnd == 0 {
		return nil
	}

	v.freeBackBuffer()
	if v.srcDC != 0 {
		win.DeleteDC(v.srcDC)
		v.srcDC = 0
	}

	if v.hIcon != 0 {
		win.DestroyIcon(v.hIcon)
		v.hIcon = 0
	}

	ret := win.DeleteObject(win.HGDIOBJ(v.background))
	if !ret {
		e = errors.Join(e, fmt.Errorf("cannot delete background"))
	}

	ret = win.KillTimer(v.hwnd, 313)
	if !ret {
		e = errors.Join(e, fmt.Errorf("cannot kill timer"))
	}

	ret = win.DestroyWindow(v.hwnd)
	if !ret {
		e = errors.Join(e, fmt.Errorf("cannot destroy window"))
	}

	ret = win.UnregisterClass(v.lpszClassName)
	if !ret {
		e = errors.Join(e, fmt.Errorf("cannot unregister class"))
	}

	return e
}

// Clear clears the window contents.
func (v *View) Clear() {
	if v.hBitmap != 0 {
		win.DeleteObject(win.HGDIOBJ(v.hBitmap))
		v.hBitmap = 0
	}
}

func (v *View) ensureBuffers(hdc win.HDC, w, h int32) {
	if v.srcDC == 0 {
		v.srcDC = win.CreateCompatibleDC(hdc)
	}

	if v.memDC != 0 && v.memW == w && v.memH == h {
		return
	}

	v.freeBackBuffer()

	v.memDC = win.CreateCompatibleDC(hdc)
	v.memBmp = win.CreateCompatibleBitmap(hdc, w, h)
	v.memOld = win.SelectObject(v.memDC, win.HGDIOBJ(v.memBmp))
	v.memW, v.memH = w, h
}

func (v *View) freeBackBuffer() {
	if v.memDC == 0 {
		return
	}

	win.SelectObject(v.memDC, v.memOld)
	win.DeleteObject(win.HGDIOBJ(v.memBmp))
	win.DeleteDC(v.memDC)

	v.memDC, v.memBmp, v.memOld = 0, 0, 0
	v.memW, v.memH = 0, 0
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

func (v *View) wndProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case win.WM_USER + 100:
		if v.createdHandler != nil {
			v.createdHandler()
		}

		return 0
	case win.WM_CREATE:
		win.PostMessage(hwnd, win.WM_USER+100, 0, 0)

		return 0
	case win.WM_DESTROY:
		win.PostQuitMessage(0)
		v.hwnd = 0

		if v.closedHandler != nil {
			v.closedHandler()
		}

		return 0
	case win.WM_SIZE:
		w := int(win.LOWORD(uint32(lParam)))
		h := int(win.HIWORD(uint32(lParam)))

		v.winWidth = w
		v.winHeight = h

		if v.resizeHandler != nil {
			v.resizeHandler(w, h)
		}

		return 0
	case win.WM_KEYDOWN:
		if v.keyPressHandler != nil {
			v.keyPressHandler(resolveKey(wParam, lParam))
		}

		return 0
	case win.WM_KEYUP:
		if v.keyReleaseHandler != nil {
			v.keyReleaseHandler(resolveKey(wParam, lParam))
		}

		return 0
	case win.WM_LBUTTONDOWN, win.WM_MBUTTONDOWN, win.WM_RBUTTONDOWN:
		var button int
		switch msg {
		case win.WM_LBUTTONDOWN:
			button = ButtonLeft

		case win.WM_RBUTTONDOWN:
			button = ButtonRight

		case win.WM_MBUTTONDOWN:
			button = ButtonMiddle
		}

		if v.buttonPressHandler != nil {
			v.buttonPressHandler(button)
		}

		return 0
	case win.WM_LBUTTONUP, win.WM_MBUTTONUP, win.WM_RBUTTONUP:
		var button int
		switch msg {
		case win.WM_LBUTTONUP:
			button = ButtonLeft

		case win.WM_RBUTTONUP:
			button = ButtonRight

		case win.WM_MBUTTONUP:
			button = ButtonMiddle
		}

		if v.buttonReleaseHandler != nil {
			v.buttonReleaseHandler(button)
		}

		return 0
	case win.WM_MOUSEWHEEL:
		delta := getWheelDeltaWparam(wParam)
		if v.scrollHandler != nil {
			if delta < 0 {
				v.scrollHandler(ScrollDown)
			} else if delta > 0 {
				v.scrollHandler(ScrollUp)
			}
		}
	case win.WM_MOUSELEAVE:
		v.mouseIn = false
		if v.leaveHandler != nil {
			v.leaveHandler()
		}

		return 0
	case win.WM_MOUSEMOVE:
		if !v.mouseIn {
			v.mouseIn = true
			if v.enterHandler != nil {
				v.enterHandler()
			}
		}

		x := win.GET_X_LPARAM(lParam)
		y := win.GET_Y_LPARAM(lParam)

		if v.motionHandler != nil {
			v.motionHandler(int(x), int(y))
		}

		return 0
	case win.WM_ERASEBKGND:
		return 1
	case win.WM_PAINT:
		var ps win.PAINTSTRUCT
		hdc := win.BeginPaint(v.hwnd, &ps)

		var rect win.RECT
		win.GetClientRect(v.hwnd, &rect)
		cw := rect.Right - rect.Left
		ch := rect.Bottom - rect.Top

		v.ensureBuffers(hdc, cw, ch)

		win.FillRect(v.memDC, &rect, v.background)

		if v.hBitmap != 0 {
			oldSrc := win.SelectObject(v.srcDC, win.HGDIOBJ(v.hBitmap))

			x := (cw - int32(v.imgWidth)) / 2
			y := (ch - int32(v.imgHeight)) / 2
			win.BitBlt(v.memDC, x, y, int32(v.imgWidth), int32(v.imgHeight), v.srcDC, 0, 0, win.SRCCOPY)

			win.SelectObject(v.srcDC, oldSrc)
		}

		win.BitBlt(hdc, 0, 0, cw, ch, v.memDC, 0, 0, win.SRCCOPY)

		win.EndPaint(v.hwnd, &ps)

		return 0
	}

	return win.DefWindowProc(hwnd, msg, wParam, lParam)
}

func (v *View) getFullscreen() bool {
	return win.GetWindowLong(v.hwnd, win.GWL_STYLE)&win.WS_OVERLAPPEDWINDOW == 0
}

func (v *View) setFullscreen(fullscreen bool) error {
	if fullscreen == v.getFullscreen() {
		return nil
	}

	if fullscreen {
		var mi win.MONITORINFO
		mi.CbSize = uint32(unsafe.Sizeof(mi))

		if v.winPlacement == nil {
			v.winPlacement = new(win.WINDOWPLACEMENT)
		}

		if !win.GetWindowPlacement(v.hwnd, v.winPlacement) {
			return fmt.Errorf("cannot get window placement")
		}

		if !win.GetMonitorInfo(win.MonitorFromWindow(v.hwnd, win.MONITOR_DEFAULTTOPRIMARY), &mi) {
			return fmt.Errorf("cannot get monitor info")
		}

		if err := ensureWindowLongBits(v.hwnd, win.GWL_STYLE, win.WS_OVERLAPPEDWINDOW, false); err != nil {
			return err
		}

		if r := mi.RcMonitor; !win.SetWindowPos(v.hwnd, win.HWND_TOP, r.Left, r.Top, r.Right-r.Left, r.Bottom-r.Top,
			win.SWP_FRAMECHANGED|win.SWP_NOOWNERZORDER) {
			return fmt.Errorf("cannot set window pos")
		}
	} else {
		if err := ensureWindowLongBits(v.hwnd, win.GWL_STYLE, win.WS_OVERLAPPEDWINDOW, true); err != nil {
			return err
		}

		if !win.SetWindowPlacement(v.hwnd, v.winPlacement) {
			return fmt.Errorf("cannot set window placement")
		}
	}

	return nil
}

func setAndClearWindowLongBits(hwnd win.HWND, index int32, set, clear uint32) error {
	value := uint32(win.GetWindowLong(hwnd, index))
	if value == 0 {
		return fmt.Errorf("cannot get window long")
	}

	if newValue := value&^clear | set; newValue != value {
		if win.SetWindowLong(hwnd, index, int32(newValue)) == 0 {
			return fmt.Errorf("cannot set window long")
		}
	}

	return nil
}

func ensureWindowLongBits(hwnd win.HWND, index int32, bits uint32, set bool) error {
	var setBits uint32
	var clearBits uint32

	if set {
		setBits = bits
	} else {
		clearBits = bits
	}

	return setAndClearWindowLongBits(hwnd, index, setBits, clearBits)
}

func hBitmapFromImage(im image.Image, dpi int) (win.HBITMAP, error) {
	var bi win.BITMAPV5HEADER
	bi.BiSize = uint32(unsafe.Sizeof(bi))
	bi.BiWidth = int32(im.Bounds().Dx())
	bi.BiHeight = -int32(im.Bounds().Dy())
	bi.BiPlanes = 1
	bi.BiBitCount = 32
	bi.BiCompression = win.BI_BITFIELDS

	inchesPerMeter := 39.37007874
	dpm := int32(math.Round(float64(dpi) * inchesPerMeter))
	bi.BiXPelsPerMeter = dpm
	bi.BiYPelsPerMeter = dpm

	// The following mask specification specifies a supported 32 BPP alpha format for Windows XP.
	bi.BV4RedMask = 0x00FF0000
	bi.BV4GreenMask = 0x0000FF00
	bi.BV4BlueMask = 0x000000FF
	bi.BV4AlphaMask = 0xFF000000

	hdc := win.GetDC(0)
	defer win.ReleaseDC(0, hdc)

	var lpBits unsafe.Pointer

	// Create the DIB section with an alpha channel.
	hBitmap := win.CreateDIBSection(hdc, &bi.BITMAPINFOHEADER, win.DIB_RGB_COLORS, &lpBits, 0, 0)
	switch hBitmap {
	case 0, win.ERROR_INVALID_PARAMETER:
		return 0, fmt.Errorf("cannot create DIB section")
	}

	bounds := im.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	stride := w * 4
	dst := (*[1 << 30]byte)(lpBits)[: stride*h : stride*h]

	if src, ok := im.(*image.RGBA); ok {
		for y := 0; y < h; y++ {
			so := y * src.Stride
			row := dst[y*stride : y*stride+stride]
			copy(row, src.Pix[so:so+stride])
			_ = swizzle.BGRA(row)
		}

		return hBitmap, nil
	}

	i := 0
	for y := bounds.Min.Y; y != bounds.Max.Y; y++ {
		for x := bounds.Min.X; x != bounds.Max.X; x++ {
			r, g, b, a := im.At(x, y).RGBA()
			dst[i+3] = byte(a >> 8)
			dst[i+2] = byte(r >> 8)
			dst[i+1] = byte(g >> 8)
			dst[i+0] = byte(b >> 8)
			i += 4
		}
	}

	return hBitmap, nil
}

func getWheelDeltaWparam(lp uintptr) int32 {
	return int32(int16(win.HIWORD(uint32(lp))))
}

// resolveKey maps the generic modifier VKs from WM_KEYDOWN to the left/right Key* constants.
func resolveKey(wParam, lParam uintptr) int {
	extended := lParam&(1<<24) != 0
	scancode := (lParam >> 16) & 0xff

	switch wParam {
	case win.VK_CONTROL:
		if extended {
			return win.VK_RCONTROL
		}
		return win.VK_LCONTROL
	case win.VK_MENU:
		if extended {
			return win.VK_RMENU
		}
		return win.VK_LMENU
	case win.VK_SHIFT:
		if scancode == 0x36 {
			return win.VK_RSHIFT
		}
		return win.VK_LSHIFT
	}

	return int(wParam)
}

func createSolidBrush(color win.COLORREF) win.HBRUSH {
	ret, _, _ := _createSolidBrush.Call(uintptr(color))
	return win.HBRUSH(ret)
}

const (
	KeySpace        = win.VK_SPACE
	KeyEscape       = win.VK_ESCAPE
	KeyEnter        = win.VK_RETURN
	KeyTab          = win.VK_TAB
	KeyBackspace    = win.VK_BACK
	KeyInsert       = win.VK_INSERT
	KeyDelete       = win.VK_DELETE
	KeyRight        = win.VK_RIGHT
	KeyLeft         = win.VK_LEFT
	KeyDown         = win.VK_DOWN
	KeyUp           = win.VK_UP
	KeyPageUp       = win.VK_PRIOR
	KeyPageDown     = win.VK_NEXT
	KeyHome         = win.VK_HOME
	KeyEnd          = win.VK_END
	KeyCapsLock     = win.VK_CAPITAL
	KeyScrollLock   = win.VK_SCROLL
	KeyNumLock      = win.VK_NUMLOCK
	KeyPrintScreen  = win.VK_PRINT
	KeyPause        = win.VK_PAUSE
	KeyF1           = win.VK_F1
	KeyF2           = win.VK_F2
	KeyF3           = win.VK_F3
	KeyF4           = win.VK_F4
	KeyF5           = win.VK_F5
	KeyF6           = win.VK_F6
	KeyF7           = win.VK_F7
	KeyF8           = win.VK_F8
	KeyF9           = win.VK_F9
	KeyF10          = win.VK_F10
	KeyF11          = win.VK_F11
	KeyF12          = win.VK_F12
	KeyLeftShift    = win.VK_LSHIFT
	KeyLeftControl  = win.VK_LCONTROL
	KeyLeftAlt      = win.VK_LMENU
	KeyLeftSuper    = win.VK_LWIN
	KeyRightShift   = win.VK_RSHIFT
	KeyRightControl = win.VK_RCONTROL
	KeyRightAlt     = win.VK_RMENU
	KeyRightSuper   = win.VK_RWIN
	KeyLeftBracket  = win.VK_OEM_4
	KeyBackSlash    = win.VK_OEM_5
	KeyRightBracket = win.VK_OEM_6
	KeyGrave        = win.VK_OEM_3
	KeyKp0          = win.VK_NUMPAD0
	KeyKp1          = win.VK_NUMPAD1
	KeyKp2          = win.VK_NUMPAD2
	KeyKp3          = win.VK_NUMPAD3
	KeyKp4          = win.VK_NUMPAD4
	KeyKp5          = win.VK_NUMPAD5
	KeyKp6          = win.VK_NUMPAD6
	KeyKp7          = win.VK_NUMPAD7
	KeyKp8          = win.VK_NUMPAD8
	KeyKp9          = win.VK_NUMPAD9
	KeyKpDecimal    = win.VK_DECIMAL
	KeyKpDivide     = win.VK_DIVIDE
	KeyKpMultiply   = win.VK_MULTIPLY
	KeyKpSubtract   = win.VK_SUBTRACT
	KeyKpAdd        = win.VK_ADD
	KeyKpEnter      = win.VK_RETURN
	KeyApostrophe   = win.VK_OEM_7
	KeyComma        = win.VK_OEM_COMMA
	KeyMinus        = win.VK_OEM_MINUS
	KeyPeriod       = win.VK_OEM_PERIOD
	KeySlash        = win.VK_OEM_2
	Key0            = 0x30
	Key1            = 0x31
	Key2            = 0x32
	Key3            = 0x33
	Key4            = 0x34
	Key5            = 0x35
	Key6            = 0x36
	Key7            = 0x37
	Key8            = 0x38
	Key9            = 0x39
	KeySemicolon    = win.VK_OEM_1
	KeyEqual        = win.VK_OEM_PLUS
	KeyA            = 0x41
	KeyB            = 0x42
	KeyC            = 0x43
	KeyD            = 0x44
	KeyE            = 0x45
	KeyF            = 0x46
	KeyG            = 0x47
	KeyH            = 0x48
	KeyI            = 0x49
	KeyJ            = 0x4A
	KeyK            = 0x4B
	KeyL            = 0x4C
	KeyM            = 0x4D
	KeyN            = 0x4E
	KeyO            = 0x4F
	KeyP            = 0x50
	KeyQ            = 0x51
	KeyR            = 0x52
	KeyS            = 0x53
	KeyT            = 0x54
	KeyU            = 0x55
	KeyV            = 0x56
	KeyW            = 0x57
	KeyX            = 0x58
	KeyY            = 0x59
	KeyZ            = 0x5A

	ButtonLeft   = win.VK_LBUTTON
	ButtonMiddle = win.VK_MBUTTON
	ButtonRight  = win.VK_RBUTTON

	ScrollUp   = 0x5
	ScrollDown = 0x6
)
