//go:build freebsd || netbsd || openbsd || dragonfly || x11

package iv

import (
	"context"
	"fmt"
	"image"
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

	v.viewer, err = newX11(opts)
	if err != nil {
		return v, err
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

// SetTitle sets window title.
func (v *View) SetTitle(title string) error {
	return v.viewer.SetTitle(title)
}

// SetIcon sets the window icon.
func (v *View) SetIcon(img image.Image) error {
	return v.viewer.SetIcon(img)
}

// Raise brings the window to the front.
func (v *View) Raise() error {
	return v.viewer.Raise()
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
