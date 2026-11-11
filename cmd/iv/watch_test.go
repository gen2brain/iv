package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func writePNG(t *testing.T, path string) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
}

func TestRebuildArgsDeleteCurrent(t *testing.T) {
	dir := t.TempDir()
	names := []string{"a.png", "b.png", "c.png", "d.png", "e.png"}
	for _, n := range names {
		writePNG(t, filepath.Join(dir, n))
	}

	v := &view{named: []string{dir}, opts: options{Sort: 1}}
	v.rebuildArgs()

	if len(v.args) != 5 {
		t.Fatalf("initial args=%d, want 5", len(v.args))
	}

	// Deleting the current image advances to the one that slid into its slot.
	v.idx = 2
	if err := os.Remove(filepath.Join(dir, "c.png")); err != nil {
		t.Fatal(err)
	}
	if gone := v.rebuildArgs(); !gone {
		t.Fatal("delete current: rebuildArgs returned false, want true")
	}
	if len(v.args) != 4 || v.idx != 2 || filepath.Base(v.args[v.idx].Name) != "d.png" {
		t.Fatalf("after delete current: idx=%d name=%s len=%d, want idx=2 d.png len=4",
			v.idx, filepath.Base(v.args[v.idx].Name), len(v.args))
	}

	// Deleting the last image clamps to the new last.
	v.idx = 3
	if err := os.Remove(filepath.Join(dir, "e.png")); err != nil {
		t.Fatal(err)
	}
	v.rebuildArgs()
	if v.idx != 2 || filepath.Base(v.args[v.idx].Name) != "d.png" {
		t.Fatalf("after delete last: idx=%d name=%s, want idx=2 d.png", v.idx, filepath.Base(v.args[v.idx].Name))
	}

	// Deleting a sibling keeps the shown image by name.
	v.idx = 1
	if err := os.Remove(filepath.Join(dir, "a.png")); err != nil {
		t.Fatal(err)
	}
	if gone := v.rebuildArgs(); gone {
		t.Fatal("delete sibling: rebuildArgs returned true, want false")
	}
	if filepath.Base(v.args[v.idx].Name) != "b.png" {
		t.Fatalf("after delete sibling: name=%s, want b.png", filepath.Base(v.args[v.idx].Name))
	}
}

func TestRebuildArgsBrowse(t *testing.T) {
	dir := t.TempDir()
	cur := filepath.Join(dir, "a.png")
	writePNG(t, cur)

	items := infos([]string{cur}, 1, false)
	v := &view{named: []string{cur}, opts: options{Sort: 1, Browse: true}, args: items}
	if err := v.browse(); err != nil {
		t.Fatal(err)
	}

	for _, n := range []string{"b.png", "c.png"} {
		writePNG(t, filepath.Join(dir, n))
		if gone := v.rebuildArgs(); gone {
			t.Fatalf("add %s: rebuildArgs returned true, want false", n)
		}
	}

	if len(v.args) != 3 || v.args[v.idx].Name != cur {
		t.Fatalf("after adds: len=%d current=%s, want 3 and %s", len(v.args), v.args[v.idx].Name, cur)
	}

	v.idx = 2
	if err := os.Remove(filepath.Join(dir, "b.png")); err != nil {
		t.Fatal(err)
	}
	v.rebuildArgs()
	if len(v.args) != 2 || filepath.Base(v.args[v.idx].Name) != "c.png" {
		t.Fatalf("after sibling delete: len=%d current=%s, want 2 and c.png", len(v.args), filepath.Base(v.args[v.idx].Name))
	}
}

func TestClassifyWriteUnlisted(t *testing.T) {
	dir := t.TempDir()
	listed := filepath.Join(dir, "a.png")
	writePNG(t, listed)

	v := &view{args: infos([]string{listed}, 0, false)}
	v.syncListed()

	var rc, rl bool
	if v.classify(fsnotify.Event{Name: listed, Op: fsnotify.Write}, &rc, &rl) || rl {
		t.Fatal("write to a listed sibling triggered a list rebuild")
	}

	if !v.classify(fsnotify.Event{Name: filepath.Join(dir, "b.png"), Op: fsnotify.Write}, &rc, &rl) || !rl {
		t.Fatal("write to an unlisted image did not trigger a list rebuild")
	}
}
