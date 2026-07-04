package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
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
