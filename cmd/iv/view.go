package main

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"text/template"
	"time"

	"github.com/anthonynsimon/bild/adjust"
	"github.com/anthonynsimon/bild/clone"
	"github.com/anthonynsimon/bild/transform"
	"github.com/fsnotify/fsnotify"

	"github.com/gen2brain/iv"
)

//go:embed resources/icon.png
var iconPNG []byte

const doubleClickInterval = 400 * time.Millisecond

func appIcon() image.Image {
	img, err := png.Decode(bytes.NewReader(iconPNG))
	if err != nil {
		return nil
	}

	return img
}

type options struct {
	Width             int
	Height            int
	Device            int
	Filter            int
	Title             bool
	TitleFormat       string
	TitleLoading      string
	Slideshow         bool
	SlideshowInterval float64
	Recursive         bool
	Fullscreen        bool
	Maximize          bool
	Browse            bool
	Loop              bool
	Preload           bool
	Sort              int
	TextColor         string
	BackgroundColor   string
	Zoom              int
	Contrast          int
	Brightness        int
	Gamma             int
	Saturation        int
	Single            bool
	Wait              bool
}

type title struct {
	App        string
	Version    string
	Index      int
	Count      int
	Name       string
	Basename   string
	Width      int
	Height     int
	Size       string
	Format     string
	Zoom       int
	Contrast   int
	Brightness int
	Gamma      int
	Saturation int
}

type view struct {
	idx    int
	frame  int
	width  int
	height int

	args []info
	opts options
	view *iv.View

	bounds    image.Rectangle
	srcBounds image.Rectangle

	fx, fy         float64
	mouseX, mouseY int
	lastClick      time.Time
	count          int

	origs  []*image.RGBA
	frames []*image.RGBA
	delays []time.Duration

	decodeMu sync.Mutex
	cache    map[int]*decoded
	inflight map[int]bool
	cacheGen int

	filter transform.ResampleFilter

	zoom       int
	contrast   int
	brightness int
	gamma      int
	saturation int
	rotation   int
	flipH      bool
	flipV      bool

	tmplFormat  *template.Template
	tmplLoading *template.Template

	modAlt   bool
	modCtrl  bool
	modShift bool

	loop bool

	slideShow         bool
	slideShowInterval time.Duration
	slideDeadline     time.Time

	needDecode bool
	needBuild  bool
	decodeAt   time.Time

	closed bool

	ctx    context.Context
	cancel func()

	mu         sync.Mutex
	pending    []info
	listener   net.Listener
	socketPath string

	named         []string
	watcher       *fsnotify.Watcher
	watched       map[string]bool
	currentPath   string
	reloadCurrent bool
	reloadList    bool
}

func newView(opts options, args []info) (*view, error) {
	v := &view{}
	v.opts = opts
	v.cache = map[int]*decoded{}
	v.inflight = map[int]bool{}

	vw, err := iv.New(iv.Options{
		AppID:           "iv",
		Width:           opts.Width,
		Height:          opts.Height,
		Device:          opts.Device,
		BackgroundColor: opts.BackgroundColor,
	})
	if err != nil {
		return v, err
	}

	v.view = vw

	if icon := appIcon(); icon != nil {
		_ = vw.SetIcon(icon)
	}

	v.tmplFormat, err = template.New("format").Parse(v.opts.TitleFormat)
	if err != nil {
		return v, err
	}

	v.tmplLoading, err = template.New("loading").Parse(v.opts.TitleLoading)
	if err != nil {
		return v, err
	}

	vw.SetCreatedHandler(v.onCreated)
	vw.SetClosedHandler(v.onClosed)
	vw.SetResizeHandler(v.onResize)
	vw.SetKeyPressHandler(v.onKeyPress)
	vw.SetKeyReleaseHandler(v.onKeyRelease)
	vw.SetScrollHandler(v.onScroll)
	vw.SetMotionHandler(v.onMotion)
	vw.SetButtonPressHandler(v.onButtonPress)

	v.zoom = opts.Zoom
	v.contrast = opts.Contrast
	v.brightness = opts.Brightness
	v.gamma = opts.Gamma
	v.saturation = opts.Saturation

	v.filter = transform.NearestNeighbor
	if opts.Filter == 1 {
		v.filter = transform.Linear
	} else if opts.Filter == 2 {
		v.filter = transform.CatmullRom
	}

	v.width, v.height = opts.Width, opts.Height
	if v.view.Driver() == "drm" {
		v.width, v.height = v.view.WindowSize()
	}

	v.mouseX, v.mouseY = v.width/2, v.height/2

	v.loop = opts.Loop

	v.slideShowInterval = time.Duration(opts.SlideshowInterval) * time.Second
	v.slideShow = opts.Slideshow

	v.args = args
	if len(v.args) == 1 && !v.args[0].IsURL && v.opts.Browse {
		err = v.browse()
		if err != nil {
			return v, err
		}
	}

	return v, nil
}

func (v *view) run() error {
	v.needDecode = true
	v.needBuild = true

	for {
		if v.closed {
			return nil
		}

		if len(v.args) == 0 {
			if err := v.waitBlank(); err != nil {
				return err
			}
			if v.closed {
				return nil
			}
			if v.drainPending() {
				_ = v.view.Raise()
			}
			v.drainWatch()

			continue
		}

		changed := false

		if v.needDecode {
			if !v.decodeAt.IsZero() && time.Now().Before(v.decodeAt) && len(v.frames) > 0 {
				if v.opts.Title {
					_ = v.view.SetTitle(v.formatTitle(true))
				}

				v.settleContext(time.Until(v.decodeAt))

				fr := v.frame
				if fr >= len(v.frames) {
					fr = 0
				}

				if err := v.view.Display(v.ctx, v.frames[fr], "", true); err != nil {
					return err
				}

				if v.closed {
					return nil
				}

				if v.drainPending() {
					_ = v.view.Raise()
				}

				v.drainWatch()

				continue
			}

			v.decodeAt = time.Time{}

			if v.opts.Title {
				_ = v.view.SetTitle(v.formatTitle(true))
			}

			if err := v.decodeCurrent(); err != nil {
				stderr(err)

				if len(v.args) <= 1 {
					return err
				}

				v.advance(1)

				continue
			}

			v.needDecode = false
			v.needBuild = true
			v.frame = 0

			if v.slideShow {
				v.slideDeadline = time.Now().Add(v.slideShowInterval)
			}

			changed = true
		}

		if v.needBuild {
			v.buildFrames()
			v.needBuild = false

			if v.frame >= len(v.frames) {
				v.frame = 0
			}

			changed = true
		}

		var t string
		if changed {
			t = v.formatTitle(false)
		}

		v.handleContext()

		if err := v.view.Display(v.ctx, v.frames[v.frame], t, true); err != nil {
			return err
		}

		if v.closed {
			return nil
		}

		if v.drainPending() {
			_ = v.view.Raise()

			continue
		}

		if v.drainWatch() {
			continue
		}

		if errors.Is(v.ctx.Err(), context.DeadlineExceeded) {
			switch {
			case v.slideShow && !time.Now().Before(v.slideDeadline):
				if v.idx == len(v.args)-1 && !v.loop {
					v.slideShow = false
				} else {
					v.advance(1)
				}
			case len(v.frames) > 1:
				v.frame++
				if v.frame >= len(v.frames) {
					v.frame = 0
				}
			}
		}
	}
}

func (v *view) advance(n int) {
	v.idx += n

	if v.idx >= len(v.args) {
		v.idx = 0
	} else if v.idx < 0 {
		v.idx = len(v.args) - 1
	}

	v.needDecode = true
}

// decoded holds the decoded frames of one image, ready for buildFrames.
type decoded struct {
	origs  []*image.RGBA
	delays []time.Duration
	anim   bool
}

// decodeInfo decodes a single image without touching view state, so preload goroutines can use it.
func decodeInfo(a info) (*decoded, error) {
	if a.IsAnimation {
		ret, delay, err := decodeAll(a)
		if err == nil && len(ret) > 1 {
			origs := make([]*image.RGBA, 0, len(ret))
			for _, r := range ret {
				origs = append(origs, clone.AsShallowRGBA(r))
			}

			return &decoded{origs: origs, delays: delay, anim: true}, nil
		}
	}

	img, err := decode(a)
	if err != nil {
		return nil, err
	}

	return &decoded{origs: []*image.RGBA{img}}, nil
}

// decodeLocked serializes decode calls so a background preload never runs a decoder concurrently with the main one.
func (v *view) decodeLocked(a info) (*decoded, error) {
	v.decodeMu.Lock()
	defer v.decodeMu.Unlock()

	return decodeInfo(a)
}

func (v *view) decodeCurrent() error {
	v.frames = nil

	v.rotation = 0
	v.flipH = false
	v.flipV = false
	v.fx, v.fy = -1, -1

	runtime.GC()

	a := v.args[v.idx]

	cur := a.Name
	if !a.IsURL {
		if abs, err := filepath.Abs(cur); err == nil {
			cur = abs
		}
	}

	v.mu.Lock()
	v.currentPath = cur
	d := v.cache[v.idx]
	gen := v.cacheGen
	v.mu.Unlock()

	if d == nil {
		v.origs = nil
		v.delays = nil

		var err error
		d, err = v.decodeLocked(a)
		if err != nil {
			return err
		}

		if v.opts.Preload {
			v.mu.Lock()
			if gen == v.cacheGen {
				v.cache[v.idx] = d
			}
			v.mu.Unlock()
		}
	}

	v.origs = d.origs
	v.delays = d.delays
	v.args[v.idx].IsAnimation = d.anim

	v.preload()

	return nil
}

// preload decodes the immediate neighbors in the background and evicts everything outside the window.
func (v *view) preload() {
	if !v.opts.Preload || len(v.args) < 2 {
		return
	}

	keep := map[int]bool{v.idx: true}
	for _, idx := range v.neighborIdxs() {
		keep[idx] = true
		v.preloadOne(idx)
	}

	v.mu.Lock()
	for k := range v.cache {
		if !keep[k] {
			delete(v.cache, k)
		}
	}
	v.mu.Unlock()
}

func (v *view) neighborIdxs() []int {
	n := len(v.args)

	next := v.idx + 1
	if next >= n {
		next = -1
		if v.loop {
			next = 0
		}
	}

	prev := v.idx - 1
	if prev < 0 {
		prev = -1
		if v.loop {
			prev = n - 1
		}
	}

	var out []int
	if next >= 0 && next != v.idx {
		out = append(out, next)
	}
	if prev >= 0 && prev != v.idx && prev != next {
		out = append(out, prev)
	}

	return out
}

func (v *view) preloadOne(idx int) {
	v.mu.Lock()
	if v.cache[idx] != nil || v.inflight[idx] {
		v.mu.Unlock()
		return
	}
	v.inflight[idx] = true
	gen := v.cacheGen
	a := v.args[idx]
	v.mu.Unlock()

	go func() {
		d, err := v.decodeLocked(a)

		v.mu.Lock()
		delete(v.inflight, idx)
		if err == nil && gen == v.cacheGen {
			v.cache[idx] = d
		}
		v.mu.Unlock()
	}()
}

// cacheClear drops all preloaded images; called when the argument list changes under the indexes.
func (v *view) cacheClear() {
	v.mu.Lock()
	v.cache = map[int]*decoded{}
	v.inflight = map[int]bool{}
	v.cacheGen++
	v.mu.Unlock()
}

func (v *view) buildFrames() {
	v.frames = make([]*image.RGBA, 0, len(v.origs))

	for _, o := range v.origs {
		img := v.orient(o)
		img = v.transform(img)
		img = v.adjust(img)
		v.frames = append(v.frames, img)
	}
}

func (v *view) orient(img *image.RGBA) *image.RGBA {
	if v.flipH {
		img = transform.FlipH(img)
	}

	if v.flipV {
		img = transform.FlipV(img)
	}

	if v.rotation != 0 {
		img = transform.Rotate(img, float64(v.rotation), &transform.RotationOptions{ResizeBounds: true})
	}

	return img
}

func (v *view) handleContext() {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.cancel != nil {
		v.cancel()
	}

	var ctx context.Context
	var cancel context.CancelFunc

	if d := v.frameTimeout(); d > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), d)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}

	v.ctx, v.cancel = ctx, cancel
}

const navDebounce = 150 * time.Millisecond

// navDecode defers the decode until the selection settles, so holding a key does not decode every step.
func (v *view) navDecode() {
	v.needDecode = true
	v.decodeAt = time.Now().Add(navDebounce)
	v.cancel()
}

func (v *view) settleContext(d time.Duration) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.cancel != nil {
		v.cancel()
	}

	v.ctx, v.cancel = context.WithTimeout(context.Background(), d)
}

// waitBlank shows an empty window and blocks until a push arrives or the window closes.
func (v *view) waitBlank() error {
	if v.opts.Title {
		_ = v.view.SetTitle(appName)
	}

	v.handleContext()

	return v.view.Display(v.ctx, image.NewRGBA(image.Rect(0, 0, 1, 1)), "", true)
}

// push queues IPC-received images and wakes the running Display.
func (v *view) push(items []info) {
	v.mu.Lock()
	v.pending = append(v.pending, items...)
	cancel := v.cancel
	v.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// drainPending appends pushed images and jumps to the first.
func (v *view) drainPending() bool {
	v.mu.Lock()
	if len(v.pending) == 0 {
		v.mu.Unlock()
		return false
	}

	start := len(v.args)
	pushed := v.pending
	v.args = append(v.args, pushed...)
	v.pending = nil
	v.idx = start
	v.needDecode = true
	v.decodeAt = time.Time{}
	v.mu.Unlock()

	for _, it := range pushed {
		v.named = append(v.named, it.Name)
		v.watchPath(it.Name)
	}

	return true
}

// drainWatch applies pending file-change notifications; re-decode only on current modify/remove, else title-only refresh.
func (v *view) drainWatch() bool {
	v.mu.Lock()
	rc, rl := v.reloadCurrent, v.reloadList
	v.reloadCurrent, v.reloadList = false, false
	v.mu.Unlock()

	if !rc && !rl {
		return false
	}

	reload := false

	if rc {
		v.refreshCurrentInfo()
		reload = true
	}

	if rl && v.rebuildArgs() {
		reload = true
	}

	switch {
	case len(v.args) == 0:
	case reload:
		v.cacheClear()
		v.needDecode = true
		v.decodeAt = time.Time{}
	case v.opts.Title:
		_ = v.view.SetTitle(v.formatTitle(false))
	}

	return true
}

// refreshCurrentInfo re-reads metadata for the current file after it was modified.
func (v *view) refreshCurrentInfo() {
	if v.idx >= len(v.args) || v.args[v.idx].IsURL {
		return
	}

	ni, err := fileInfo(v.args[v.idx].Name)
	if err != nil {
		return
	}

	ni.Id = v.args[v.idx].Id
	v.args[v.idx] = ni
}

// rebuildArgs re-expands local named args (keeping position, carrying over URLs); returns true if the current image is gone.
func (v *view) rebuildArgs() bool {
	var named []string
	for _, n := range v.named {
		if !isURL(n) {
			named = append(named, n)
		}
	}

	items := infos(args(named, v.opts.Recursive), v.opts.Sort)

	for _, it := range v.args {
		if it.IsURL {
			items = append(items, it)
		}
	}

	if len(items) == 0 {
		v.args = nil
		v.idx = 0

		return true
	}

	cur := ""
	if v.idx < len(v.args) {
		cur = v.args[v.idx].Name
	}

	v.args = items
	v.idx = 0
	for i := range items {
		if items[i].Name == cur {
			v.idx = i

			return false
		}
	}

	return true
}

func (v *view) frameTimeout() time.Duration {
	var d time.Duration

	anim := len(v.frames) > 1
	if anim && v.frame < len(v.delays) {
		d = v.delays[v.frame]
	}

	if v.slideShow {
		rem := time.Until(v.slideDeadline)
		if rem < 0 {
			rem = 0
		}

		if !anim || rem < d {
			d = rem
		}
	}

	return d
}

func (v *view) onCreated() {
	if v.opts.Fullscreen {
		if err := v.view.ToggleFullscreen(); err != nil {
			stderr(err)
		}
	}

	if v.opts.Maximize {
		if err := v.view.Maximize(); err != nil {
			stderr(err)
		}
	}
}

func (v *view) onClosed() {
	v.closed = true

	if v.cancel != nil {
		v.cancel()
	}
}

func (v *view) onResize(w, h int) {
	v.width = w
	v.height = h

	v.needBuild = true

	if v.cancel != nil {
		v.cancel()
	}
}

func (v *view) onKeyPress(key int) {
	switch key {
	case iv.KeyLeftAlt, iv.KeyRightAlt:
		v.modAlt = true
		return
	case iv.KeyLeftControl, iv.KeyRightControl:
		v.modCtrl = true
		return
	case iv.KeyLeftShift, iv.KeyRightShift:
		v.modShift = true
		return
	}

	if d, ok := digitKey(key); ok {
		if v.modShift {
			v.count = 0
			v.digitAction(key)
		} else {
			v.count = v.count*10 + d
		}

		return
	}

	if key == iv.KeyG && v.modShift {
		v.jumpTo()
		return
	}

	v.count = 0

	if key == iv.KeyF || key == iv.KeyF11 {
		if err := v.view.ToggleFullscreen(); err != nil {
			stderr(err)
		}

		return
	}

	if key == iv.KeyEnter {
		fmt.Println(v.args[v.idx].Name)

		return
	}

	if key == iv.KeyEscape || key == iv.KeyQ {
		v.closed = true
		v.cancel()

		if err := v.view.Close(); err != nil {
			stderr(err)
		}

		return
	}

	if key == iv.KeyS {
		v.slideShow = !v.slideShow
		if v.slideShow {
			v.slideDeadline = time.Now().Add(v.slideShowInterval)
		}

		v.cancel()

		return
	}

	if key == iv.KeyMinus && !v.modShift || key == iv.KeyEqual && v.modShift {
		v.zoomAt(key, v.width/2, v.height/2)
		v.rebuild()

		return
	}

	if key == iv.KeyR {
		if v.modShift {
			v.rotation = (v.rotation + 270) % 360
		} else {
			v.rotation = (v.rotation + 90) % 360
		}

		v.rebuild()

		return
	}

	if key == iv.KeyH {
		v.flipH = !v.flipH
		v.rebuild()

		return
	}

	if key == iv.KeyV {
		v.flipV = !v.flipV
		v.rebuild()

		return
	}

	v.handleIndex(key)
}

func digitKey(key int) (int, bool) {
	switch key {
	case iv.Key0:
		return 0, true
	case iv.Key1:
		return 1, true
	case iv.Key2:
		return 2, true
	case iv.Key3:
		return 3, true
	case iv.Key4:
		return 4, true
	case iv.Key5:
		return 5, true
	case iv.Key6:
		return 6, true
	case iv.Key7:
		return 7, true
	case iv.Key8:
		return 8, true
	case iv.Key9:
		return 9, true
	}

	return 0, false
}

func (v *view) digitAction(key int) {
	switch key {
	case iv.Key1, iv.Key2:
		v.handleContrast(key)
	case iv.Key3, iv.Key4:
		v.handleBrightness(key)
	case iv.Key5, iv.Key6:
		v.handleGamma(key)
	case iv.Key7, iv.Key8:
		v.handleSaturation(key)
	case iv.Key9:
		v.zoom = 100
		v.fx, v.fy = -1, -1
	case iv.Key0:
		v.zoom = 0
		v.fx, v.fy = -1, -1
	}

	v.rebuild()
}

func (v *view) jumpTo() {
	idx := len(v.args) - 1
	if v.count > 0 {
		idx = min(v.count-1, len(v.args)-1)
	}

	v.count = 0

	if idx != v.idx {
		v.idx = idx
		v.navDecode()
	}
}

func (v *view) onKeyRelease(key int) {
	if key == iv.KeyLeftAlt || key == iv.KeyRightAlt {
		v.modAlt = false
	}

	if key == iv.KeyLeftControl || key == iv.KeyRightControl {
		v.modCtrl = false
	}

	if key == iv.KeyLeftShift || key == iv.KeyRightShift {
		v.modShift = false
	}
}

func (v *view) onScroll(direction int) {
	if v.modCtrl {
		key := iv.KeyEqual
		if direction == iv.ScrollDown {
			key = iv.KeyMinus
		}

		v.zoomAt(key, v.mouseX, v.mouseY)
		v.rebuild()

		return
	}

	old := v.idx

	if direction == iv.ScrollDown {
		if v.idx != len(v.args)-1 {
			v.idx += 1
		} else if v.loop {
			v.idx = 0
		}
	} else if direction == iv.ScrollUp {
		if v.idx != 0 {
			v.idx -= 1
		} else if v.loop {
			v.idx = len(v.args) - 1
		}
	}

	if v.idx != old {
		v.navDecode()
	}
}

func (v *view) rebuild() {
	v.needBuild = true
	v.cancel()
}

func (v *view) transform(img *image.RGBA) *image.RGBA {
	v.srcBounds = img.Bounds()

	if v.zoom == 0 {
		img = fit(img, v.width, v.height, v.filter)
		v.bounds = img.Bounds()

		return img
	}

	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	s := float64(v.zoom) / 100

	zw, zh := int(float64(sw)*s), int(float64(sh)*s)
	v.bounds = image.Rect(0, 0, zw, zh)

	cw, ch := min(zw, v.width), min(zh, v.height)

	fx, fy := v.fx, v.fy
	if fx < 0 {
		fx = float64(sw) / 2
	}
	if fy < 0 {
		fy = float64(sh) / 2
	}

	ox := min(max(int(fx*s)-v.width/2, 0), zw-cw)
	oy := min(max(int(fy*s)-v.height/2, 0), zh-ch)

	// Crop the visible source region, then scale to display size.
	x0 := b.Min.X + int(float64(ox)/s)
	y0 := b.Min.Y + int(float64(oy)/s)
	x1 := b.Min.X + int(float64(ox+cw)/s)
	y1 := b.Min.Y + int(float64(oy+ch)/s)

	if x1 <= x0 {
		x1 = x0 + 1
	}
	if y1 <= y0 {
		y1 = y0 + 1
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}

	region := transform.Crop(img, image.Rect(x0, y0, x1, y1))

	return resize(region, cw, ch, v.filter)
}

func (v *view) zoomAt(key, cx, cy int) {
	sw, sh := v.srcBounds.Dx(), v.srcBounds.Dy()
	if sw == 0 || sh == 0 {
		v.handleZoom(key)
		return
	}

	fx, fy := v.fx, v.fy
	if fx < 0 {
		fx = float64(sw) / 2
	}
	if fy < 0 {
		fy = float64(sh) / 2
	}

	sOld := float64(v.bounds.Dx()) / float64(sw)
	zw, zh := v.bounds.Dx(), v.bounds.Dy()
	cw, ch := min(zw, v.width), min(zh, v.height)
	panX := min(max(int(fx*sOld)-v.width/2, 0), zw-cw)
	panY := min(max(int(fy*sOld)-v.height/2, 0), zh-ch)

	cox := float64(panX-(v.width-cw)/2+cx) / sOld
	coy := float64(panY-(v.height-ch)/2+cy) / sOld

	v.handleZoom(key)

	sNew := float64(v.zoom) / 100
	v.fx = min(max(cox-float64(cx-v.width/2)/sNew, 0), float64(sw))
	v.fy = min(max(coy-float64(cy-v.height/2)/sNew, 0), float64(sh))
}

func (v *view) onMotion(x, y int) {
	v.mouseX, v.mouseY = x, y
}

func (v *view) doubleClick(now time.Time) bool {
	if now.Sub(v.lastClick) < doubleClickInterval {
		v.lastClick = time.Time{}
		return true
	}

	v.lastClick = now

	return false
}

func (v *view) onButtonPress(button int) {
	if button != iv.ButtonLeft {
		return
	}

	if v.doubleClick(time.Now()) {
		if err := v.view.ToggleFullscreen(); err != nil {
			stderr(err)
		}
	}
}

func (v *view) adjust(img *image.RGBA) *image.RGBA {
	if v.contrast != 0 {
		img = adjust.Contrast(img, float64(v.contrast)/100)
	}

	if v.brightness != 0 {
		img = adjust.Brightness(img, float64(v.brightness)/100)
	}

	if v.gamma != 100 {
		img = adjust.Gamma(img, float64(v.gamma)/100)
	}

	if v.saturation != 0 {
		img = adjust.Saturation(img, float64(v.saturation)/100)
	}

	return img
}

var zoomLevels = []int{10, 25, 50, 75, 100, 125, 150, 175, 200, 250, 300, 400, 500, 600, 800, 1000}

func (v *view) handleZoom(key int) {
	current := v.zoomPercent()

	switch key {
	case iv.KeyEqual:
		v.zoom = zoomLevels[len(zoomLevels)-1]
		for _, l := range zoomLevels {
			if l > current {
				v.zoom = l
				break
			}
		}
	case iv.KeyMinus:
		v.zoom = zoomLevels[0]
		for i := len(zoomLevels) - 1; i >= 0; i-- {
			if zoomLevels[i] < current {
				v.zoom = zoomLevels[i]
				break
			}
		}
	}
}

func (v *view) handleContrast(key int) {
	var contrast int

	if key == iv.Key1 {
		contrast = v.contrast - 1
		if contrast < -100 {
			contrast = -100
		}
	} else if key == iv.Key2 {
		contrast = v.contrast + 1
		if contrast > 100 {
			contrast = 100
		}
	}

	v.contrast = contrast
}

func (v *view) handleBrightness(key int) {
	var brightness int

	if key == iv.Key3 {
		brightness = v.brightness - 1
		if brightness < -100 {
			brightness = -100
		}
	} else if key == iv.Key4 {
		brightness = v.brightness + 1
		if brightness > 100 {
			brightness = 100
		}
	}

	v.brightness = brightness
}

func (v *view) handleGamma(key int) {
	var gamma int

	if key == iv.Key5 {
		gamma = v.gamma - 1
		if gamma <= 0 {
			gamma = 1
		}
	} else if key == iv.Key6 {
		gamma = v.gamma + 1
		if gamma > 100 {
			gamma = 100
		}
	}

	v.gamma = gamma
}

func (v *view) handleSaturation(key int) {
	var saturation int

	if key == iv.Key7 {
		saturation = v.saturation - 1
		if saturation < -100 {
			saturation = -100
		}
	} else if key == iv.Key8 {
		saturation = v.saturation + 1
		if saturation > 100 {
			saturation = 100
		}
	}

	v.saturation = saturation
}

func (v *view) handleIndex(key int) {
	old := v.idx

	switch {
	case key == iv.KeyRight || key == iv.KeySpace || key == iv.KeyJ:
		if v.idx != len(v.args)-1 {
			v.idx += 1
		} else if v.loop {
			v.idx = 0
		}
	case key == iv.KeyLeft || key == iv.KeyBackspace || key == iv.KeyK:
		if v.idx != 0 {
			v.idx -= 1
		} else if v.loop {
			v.idx = len(v.args) - 1
		}
	case key == iv.KeyComma || key == iv.KeyHome:
		v.idx = 0
	case key == iv.KeyPeriod || key == iv.KeyEnd:
		v.idx = len(v.args) - 1
	case key == iv.KeyLeftBracket || key == iv.KeyPageUp:
		if v.idx-10 >= 0 {
			v.idx -= 10
		} else {
			v.idx = 0
		}
	case key == iv.KeyRightBracket || key == iv.KeyPageDown:
		if v.idx+10 <= len(v.args)-1 {
			v.idx += 10
		} else {
			v.idx = len(v.args) - 1
		}
	}

	if v.idx != old {
		v.navDecode()
	}
}

func (v *view) formatTitle(loading bool) string {
	var out string

	if !v.opts.Title {
		return out
	}

	a := v.args[v.idx]

	size := ""
	if a.Size > 0 {
		size = humanize(a.Size)
	}

	t := title{appName, appVersion, v.idx + 1, len(v.args), a.Name,
		a.Base, a.Width, a.Height, size,
		a.Format, v.zoomPercent(), v.contrast, v.brightness, v.gamma, v.saturation}

	if loading {
		var b bytes.Buffer
		_ = v.tmplLoading.Execute(&b, t)
		out = b.String()
	} else {
		var b bytes.Buffer
		_ = v.tmplFormat.Execute(&b, t)
		out = b.String()
	}

	return out
}

func (v *view) zoomPercent() int {
	var scale float64

	if v.srcBounds.Dx() != 0 {
		scale = math.Ceil(float64(v.bounds.Dx()) / float64(v.srcBounds.Dx()) * 100)
	}

	return int(scale)
}

func (v *view) browse() error {
	fileName := v.args[0].Name
	dirName := filepath.Dir(fileName)

	fs, err := os.ReadDir(dirName)
	if err != nil {
		return err
	}

	a := make([]string, 0, len(fs))

	for _, f := range fs {
		fp := filepath.Join(dirName, f.Name())
		if !f.IsDir() && (isImage(fp) || f.Name() == filepath.Base(fileName)) {
			a = append(a, fp)
		}
	}

	out := infos(a, v.opts.Sort)

	if len(out) > 1 {
		v.args = out

		for idx, arg := range v.args {
			if arg.Name == fileName {
				v.idx = idx
				break
			}
		}
	}

	return nil
}
