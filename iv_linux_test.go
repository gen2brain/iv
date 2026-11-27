//go:build linux

package iv

import (
	"encoding/binary"
	"image"
	"image/color"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/NeowayLabs/drm/mode"
	"github.com/gen2brain/wl"
	"github.com/jezek/xgb/xproto"
	"github.com/pbnjay/pixfont"
)

func TestElideTitle(t *testing.T) {
	short := "a.jpg"
	if got := elideTitle(short, 10000); got != short {
		t.Errorf("short title changed: %q", got)
	}

	long := "/home/user/pictures/some-very-long-filename.jpg"
	for _, mw := range []int{400, 200, 120} {
		got := elideTitle(long, mw)

		if pixfont.MeasureString(got) > mw {
			t.Errorf("maxWidth=%d: %q measures %d > max", mw, got, pixfont.MeasureString(got))
		}
		if !strings.Contains(got, "...") {
			t.Errorf("maxWidth=%d: %q missing ellipsis", mw, got)
		}
		if !strings.HasPrefix(got, "/") || !strings.HasSuffix(got, ".jpg") {
			t.Errorf("maxWidth=%d: %q dropped start/end", mw, got)
		}
	}

	if got := elideTitle(long, 5); got != "" {
		t.Errorf("tiny width should yield empty, got %q", got)
	}
}

func TestScaleIconDown(t *testing.T) {
	small := image.NewRGBA(image.Rect(0, 0, 64, 64))
	if got := scaleIconDown(small, maxIconX11); got != small {
		t.Error("icon within max should be returned as-is")
	}

	big := image.NewRGBA(image.Rect(0, 0, 512, 256))
	got := scaleIconDown(big, maxIconX11)
	if got.Bounds().Dx() != maxIconX11 || got.Bounds().Dy() != maxIconX11/2 {
		t.Errorf("scaled to %dx%d, want %dx%d", got.Bounds().Dx(), got.Bounds().Dy(), maxIconX11, maxIconX11/2)
	}
}

func TestWLCanvas(t *testing.T) {
	data := make([]byte, 2*4)

	bgra := &wlCanvas{data: data, stride: 2 * 4, w: 2, h: 1, useXBGR: false}
	bgra.Set(0, 0, color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff})
	if data[0] != 0x33 || data[1] != 0x22 || data[2] != 0x11 || data[3] != 0xff {
		t.Errorf("BGRA order wrong: %v", data[:4])
	}

	rgba := &wlCanvas{data: data, stride: 2 * 4, w: 2, h: 1, useXBGR: true}
	rgba.Set(1, 0, color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff})
	if data[4] != 0x11 || data[5] != 0x22 || data[6] != 0x33 || data[7] != 0xff {
		t.Errorf("RGBA order wrong: %v", data[4:8])
	}

	bgra.Set(-1, 0, color.RGBA{R: 1, G: 2, B: 3, A: 4})
	bgra.Set(5, 5, color.RGBA{R: 1, G: 2, B: 3, A: 4})
}

func TestWaylandKeyRepeatStops(t *testing.T) {
	v := &viewWayland{repeatRate: 25, repeatDelay: 0}
	presses := 0
	v.SetKeyPressHandler(func(int) { presses++ })

	press := func() {
		_ = v.onKey(nil, wl.Keyboard{}, 0, 0, uint32(KeyRight-8), wl.KeyboardKeyStatePressed)
	}

	press()
	if v.repeatKey != KeyRight {
		t.Fatalf("repeatKey = %d after press, want %d", v.repeatKey, KeyRight)
	}

	_ = v.onKeyboardLeave(nil, wl.Keyboard{}, 0, wl.Surface{})
	if v.repeatKey != 0 {
		t.Fatalf("repeatKey = %d after keyboard leave, want 0", v.repeatKey)
	}

	press()
	_ = v.onRepeatInfo(nil, wl.Keyboard{}, 0, 600)
	if v.repeatKey != 0 {
		t.Fatalf("repeatKey = %d after repeat rate 0, want 0", v.repeatKey)
	}

	v.fireRepeat()

	press()
	if v.repeatKey != 0 {
		t.Fatalf("repeatKey = %d after press with repeat disabled, want 0", v.repeatKey)
	}

	if presses != 3 {
		t.Fatalf("presses = %d, want 3", presses)
	}
}

func TestDRMFlipEvents(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	v := &viewDRM{dev: r, flips: make(chan uint32, 8)}
	v.msets = []mset{{mode: &mode.Modeset{Crtc: 41}, flipping: true}, {mode: &mode.Modeset{Crtc: 42}, flipping: true}}
	go v.readFlips()

	event := func(typ uint32, crtc uint64) []byte {
		b := make([]byte, 32)
		binary.NativeEndian.PutUint32(b, typ)
		binary.NativeEndian.PutUint32(b[4:], 32)
		binary.NativeEndian.PutUint64(b[8:], crtc)
		return b
	}

	buf := append(event(0x01, 41), event(drmEventFlipComplete, 42)...)
	if _, err := w.Write(append(buf, event(drmEventFlipComplete, 41)...)); err != nil {
		t.Fatal(err)
	}

	for _, want := range []uint32{42, 41} {
		select {
		case crtc, ok := <-v.flips:
			if !ok || crtc != want {
				t.Fatalf("flip crtc = %d, %v, want %d", crtc, ok, want)
			}
			v.flipDone(crtc, ok)
		case <-time.After(time.Second):
			t.Fatalf("no flip event for crtc %d", want)
		}
	}

	if v.flipping() {
		t.Fatal("still flipping after both flips completed")
	}

	v.msets[0].flipping = true
	w.Close()
	v.waitFlip(&v.msets[0])
	if v.msets[0].flipping || !v.msets[0].noFlip {
		t.Fatalf("after device close: flipping %v noFlip %v, want false true", v.msets[0].flipping, v.msets[0].noFlip)
	}
}

func TestX11FrameSyncMessages(t *testing.T) {
	v := &viewX11{atomProtocols: 10, atomSyncRequest: 11, atomFrameDrawn: 12}
	msg := func(typ xproto.Atom, d ...uint32) xproto.ClientMessageEvent {
		data := make([]uint32, 5)
		copy(data, d)
		return xproto.ClientMessageEvent{Format: 32, Type: typ, Data: xproto.ClientMessageDataUnionData32New(data)}
	}

	v.frameWait = 1<<32 | 6
	v.clientMessage(msg(12, 4, 1))
	if v.frameWait == 0 {
		t.Fatal("frame drawn for an older serial cleared the wait")
	}
	v.clientMessage(msg(12, 6, 1))
	if v.frameWait != 0 {
		t.Fatalf("frameWait = %d after frame drawn for the awaited serial, want 0", v.frameWait)
	}

	v.atomFrameTimings, v.frameTimings = 13, true
	v.frameWait = 7
	v.clientMessage(msg(12, 7, 0))
	if v.frameWait == 0 {
		t.Fatal("frame drawn cleared the wait although frame timings are supported")
	}
	v.clientMessage(msg(13, 7, 0, 0, 16667))
	if v.frameWait != 0 {
		t.Fatalf("frameWait = %d after frame timings for the awaited serial, want 0", v.frameWait)
	}

	v.clientMessage(msg(10, 11, 0, 8, 2, 1))
	if v.syncPending != 2<<32|8 || !v.syncPendingExt {
		t.Fatalf("sync request: pending %d ext %v, want %d true", v.syncPending, v.syncPendingExt, int64(2<<32|8))
	}

	v.syncPending = 0
	v.clientMessage(msg(10, 99, 0, 8, 2, 1))
	if v.syncPending != 0 {
		t.Fatal("WM_PROTOCOLS message for another protocol was taken as a sync request")
	}
}
