package main

import (
	"os"
	"testing"
)

func TestColorize(t *testing.T) {
	if got := colorize(false, colorBold, "x"); got != "x" {
		t.Fatalf("colorize(off) = %q, want %q", got, "x")
	}

	want := colorCyan + "x" + "\033[0m"
	if got := colorize(true, colorCyan, "x"); got != want {
		t.Fatalf("colorize(on) = %q, want %q", got, want)
	}

	f, err := os.CreateTemp(t.TempDir(), "iv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if useColor(f) {
		t.Fatal("useColor should be false for a regular file")
	}

	t.Setenv("NO_COLOR", "1")
	if useColor(f) {
		t.Fatal("useColor should be false when NO_COLOR is set")
	}
}
