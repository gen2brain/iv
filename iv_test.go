package iv_test

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"image/jpeg"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/gen2brain/iv"
)

//go:embed testdata/test.jpg
var data []byte

func init() {
	runtime.LockOSThread()
}

func TestMain(m *testing.M) {
	code := m.Run()

	// Window smoke test on the main goroutine; opt-in via IV_TEST_WINDOW, needs a display.
	if os.Getenv("IV_TEST_WINDOW") != "" {
		if err := showWindow(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}

	os.Exit(code)
}

func showWindow() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}

	view, err := iv.New(iv.Options{Width: 700, Height: 700})
	if err != nil {
		return err
	}
	defer view.Close()

	return view.Display(ctx, img, "test.jpg")
}
