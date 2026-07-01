package main

import (
	"image"
	"testing"
	"text/template"
	"time"

	"github.com/gen2brain/iv"
)

func TestTransformZoomBounds(t *testing.T) {
	v := &view{width: 100, height: 100, zoom: 400, fx: -1, fy: -1, filter: 0}

	out := v.transform(image.NewRGBA(image.Rect(0, 0, 500, 500)))

	// Displayed crop is window-sized, not the full 2000x2000 zoomed image.
	if out.Bounds().Dx() != 100 || out.Bounds().Dy() != 100 {
		t.Fatalf("zoom output %dx%d, want 100x100", out.Bounds().Dx(), out.Bounds().Dy())
	}
	if v.bounds.Dx() != 2000 {
		t.Fatalf("bounds.Dx=%d, want 2000", v.bounds.Dx())
	}
}

func TestJumpTo(t *testing.T) {
	v := &view{cancel: func() {}}
	v.args = make([]info, 20)

	// "12G" jumps to image 12 (idx 11).
	v.onKeyPress(iv.Key1)
	v.onKeyPress(iv.Key2)
	v.modShift = true
	v.onKeyPress(iv.KeyG)
	v.modShift = false
	if v.idx != 11 {
		t.Fatalf("12G: idx=%d, want 11", v.idx)
	}

	// "5G" jumps to image 5 (idx 4).
	v.onKeyPress(iv.Key5)
	v.modShift = true
	v.onKeyPress(iv.KeyG)
	v.modShift = false
	if v.idx != 4 {
		t.Fatalf("5G: idx=%d, want 4", v.idx)
	}

	// "99G" clamps to the last image (idx 19).
	v.onKeyPress(iv.Key9)
	v.onKeyPress(iv.Key9)
	v.modShift = true
	v.onKeyPress(iv.KeyG)
	v.modShift = false
	if v.idx != 19 {
		t.Fatalf("99G: idx=%d, want 19 (clamped)", v.idx)
	}

	// Shift+digit is an adjustment, not a count.
	v.modShift = true
	v.onKeyPress(iv.Key1)
	v.modShift = false
	if v.contrast != -1 {
		t.Fatalf("Shift+1: contrast=%d, want -1", v.contrast)
	}
	if v.count != 0 {
		t.Fatalf("Shift+digit should not build a count, got %d", v.count)
	}
}

func TestDoubleClick(t *testing.T) {
	v := &view{}
	base := time.Now()

	if v.doubleClick(base) {
		t.Fatal("first click should not be a double click")
	}
	if !v.doubleClick(base.Add(100 * time.Millisecond)) {
		t.Fatal("second click within the interval should be a double click")
	}
	if v.doubleClick(base.Add(200 * time.Millisecond)) {
		t.Fatal("third click should not re-trigger after a double click")
	}
	if v.doubleClick(base.Add(2 * time.Second)) {
		t.Fatal("slow click should not be a double click")
	}
}

func TestTitleSize(t *testing.T) {
	tmpl, err := template.New("f").Parse("{{.Width}}x{{.Height}}{{if .Size}}, {{.Size}}{{end}}, {{.Format}}")
	if err != nil {
		t.Fatal(err)
	}

	v := &view{opts: options{Title: true}, tmplFormat: tmpl}

	v.args = []info{{Width: 100, Height: 50, Size: 0, Format: "PNG"}}
	if got := v.formatTitle(false); got != "100x50, PNG" {
		t.Fatalf("size 0: got %q, want %q", got, "100x50, PNG")
	}

	v.args = []info{{Width: 100, Height: 50, Size: 2048, Format: "PNG"}}
	want := "100x50, " + humanize(2048) + ", PNG"
	if got := v.formatTitle(false); got != want {
		t.Fatalf("size set: got %q, want %q", got, want)
	}
}

func TestZoomLevels(t *testing.T) {
	v := &view{srcBounds: image.Rect(0, 0, 100, 100)}

	cases := []struct {
		current int
		key     int
		want    int
	}{
		{100, iv.KeyEqual, 125},
		{100, iv.KeyMinus, 75},
		{40, iv.KeyEqual, 50},
		{40, iv.KeyMinus, 25},
		{1000, iv.KeyEqual, 1000},
		{1000, iv.KeyMinus, 800},
		{5, iv.KeyEqual, 10},
		{5, iv.KeyMinus, 10},
	}

	for _, c := range cases {
		v.bounds = image.Rect(0, 0, c.current, 1)
		v.handleZoom(c.key)
		if v.zoom != c.want {
			t.Errorf("current=%d%% key=%d: zoom=%d, want %d", c.current, c.key, v.zoom, c.want)
		}
	}
}

func TestZoomAt(t *testing.T) {
	// 1000x1000 source shown at 50% in a 500x500 window; zoom in toward (400,250).
	v := &view{
		width:     500,
		height:    500,
		srcBounds: image.Rect(0, 0, 1000, 1000),
		bounds:    image.Rect(0, 0, 500, 500),
		zoom:      50,
		fx:        -1,
		fy:        -1,
	}

	v.zoomAt(iv.KeyEqual, 400, 250)

	if v.zoom != 75 {
		t.Fatalf("zoom=%d, want 75", v.zoom)
	}
	if int(v.fx) != 600 || int(v.fy) != 500 {
		t.Fatalf("focus=(%.0f,%.0f), want (600,500)", v.fx, v.fy)
	}
}

func TestPanBy(t *testing.T) {
	// 1000x1000 source at 200% in a 500x500 window: zoomed exceeds window, so pannable.
	v := &view{
		width:     500,
		height:    500,
		srcBounds: image.Rect(0, 0, 1000, 1000),
		bounds:    image.Rect(0, 0, 2000, 2000),
		zoom:      200,
		fx:        -1,
		fy:        -1,
		cancel:    func() {},
	}

	if !v.pannable() {
		t.Fatal("expected pannable when zoomed beyond the window")
	}

	v.panBy(100, 60)
	if v.fx != 450 || v.fy != 470 {
		t.Fatalf("focus=(%.0f,%.0f), want (450,470)", v.fx, v.fy)
	}

	v.fx, v.fy = 10, 10
	v.panBy(1000, 1000)
	if v.fx != 0 || v.fy != 0 {
		t.Fatalf("focus=(%.0f,%.0f), want (0,0)", v.fx, v.fy)
	}

	v.zoom = 0
	if v.pannable() {
		t.Fatal("expected not pannable at fit")
	}
}
