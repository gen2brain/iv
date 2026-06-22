//go:build !linux && !windows && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

package iv

import (
	"context"
	"image"
)

type View struct {
}

func New(args ...any) (*View, error) {
	return nil, ErrUnsupported
}

func (v *View) Driver() string {
	return "null"
}

func (v *View) Display(ctx context.Context, img image.Image, args ...any) error {
	return nil
}

func (v *View) ToggleFullscreen() error {
	return nil
}

func (v *View) SetTitle(title string) error {
	return nil
}

func (v *View) SetIcon(img image.Image) error {
	return nil
}

func (v *View) Raise() error {
	return nil
}

func (v *View) Fullscreen() bool {
	return false
}

func (v *View) ScreenSize() (int, int) {
	return 0, 0
}

func (v *View) WindowSize() (int, int) {
	return 0, 0
}

func (v *View) Close() error {
	return nil
}

func (v *View) Clear() {
}

func (v *View) SetKeyPressHandler(handler KeyPressHandler) {
}

func (v *View) SetKeyReleaseHandler(handler KeyReleaseHandler) {
}

func (v *View) SetButtonPressHandler(handler ButtonPressHandler) {
}

func (v *View) SetButtonReleaseHandler(handler ButtonReleaseHandler) {
}

func (v *View) SetMotionHandler(handler MotionHandler) {
}

func (v *View) SetScrollHandler(handler ScrollHandler) {
}

func (v *View) SetEnterHandler(handler EnterHandler) {
}

func (v *View) SetLeaveHandler(handler LeaveHandler) {
}

func (v *View) SetResizeHandler(handler ResizeHandler) {
}

func (v *View) SetCreatedHandler(handler CreatedHandler) {
}

func (v *View) SetClosedHandler(handler ClosedHandler) {
}

const (
	KeySpace        = 0
	KeyEscape       = 0
	KeyEnter        = 0
	KeyTab          = 0
	KeyBackspace    = 0
	KeyInsert       = 0
	KeyDelete       = 0
	KeyRight        = 0
	KeyLeft         = 0
	KeyDown         = 0
	KeyUp           = 0
	KeyPageUp       = 0
	KeyPageDown     = 0
	KeyHome         = 0
	KeyEnd          = 0
	KeyCapsLock     = 0
	KeyScrollLock   = 0
	KeyNumLock      = 0
	KeyPrintScreen  = 0
	KeyPause        = 0
	KeyF1           = 0
	KeyF2           = 0
	KeyF3           = 0
	KeyF4           = 0
	KeyF5           = 0
	KeyF6           = 0
	KeyF7           = 0
	KeyF8           = 0
	KeyF9           = 0
	KeyF10          = 0
	KeyF11          = 0
	KeyF12          = 0
	KeyLeftShift    = 0
	KeyLeftControl  = 0
	KeyLeftAlt      = 0
	KeyLeftSuper    = 0
	KeyRightShift   = 0
	KeyRightControl = 0
	KeyRightAlt     = 0
	KeyRightSuper   = 0
	KeyLeftBracket  = 0
	KeyBackSlash    = 0
	KeyRightBracket = 0
	KeyGrave        = 0
	KeyKp0          = 0
	KeyKp1          = 0
	KeyKp2          = 0
	KeyKp3          = 0
	KeyKp4          = 0
	KeyKp5          = 0
	KeyKp6          = 0
	KeyKp7          = 0
	KeyKp8          = 0
	KeyKp9          = 0
	KeyKpDecimal    = 0
	KeyKpDivide     = 0
	KeyKpMultiply   = 0
	KeyKpSubtract   = 0
	KeyKpAdd        = 0
	KeyKpEnter      = 0
	KeyApostrophe   = 0
	KeyComma        = 0
	KeyMinus        = 0
	KeyPeriod       = 0
	KeySlash        = 0
	Key0            = 0
	Key1            = 0
	Key2            = 0
	Key3            = 0
	Key4            = 0
	Key5            = 0
	Key6            = 0
	Key7            = 0
	Key8            = 0
	Key9            = 0
	KeySemicolon    = 0
	KeyEqual        = 0
	KeyA            = 0
	KeyB            = 0
	KeyC            = 0
	KeyD            = 0
	KeyE            = 0
	KeyF            = 0
	KeyG            = 0
	KeyH            = 0
	KeyI            = 0
	KeyJ            = 0
	KeyK            = 0
	KeyL            = 0
	KeyM            = 0
	KeyN            = 0
	KeyO            = 0
	KeyP            = 0
	KeyQ            = 0
	KeyR            = 0
	KeyS            = 0
	KeyT            = 0
	KeyU            = 0
	KeyV            = 0
	KeyW            = 0
	KeyX            = 0
	KeyY            = 0
	KeyZ            = 0

	ButtonLeft   = 0
	ButtonMiddle = 0
	ButtonRight  = 0

	ScrollUp   = 0
	ScrollDown = 0
)
