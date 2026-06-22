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

	if useColor(os.Stdout) {
		t.Fatal("useColor should be false for non-terminal stdout")
	}
}
