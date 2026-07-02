package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"

	"github.com/adrg/xdg"
	"go.senan.xyz/flagconf"
)

var (
	appVersion string
	appName    = filepath.Base(os.Args[0])
)

func main() {
	runtime.LockOSThread()

	var opts options

	titleFormat := "{{.App}} {{.Version}} [{{.Index}}/{{.Count}}] - {{if .Marked}}* {{end}}{{.Name}} ({{.Width}}x{{.Height}}{{if .Size}}, {{.Size}}{{end}}, {{.Format}}, {{.Zoom}}% " +
		"{{- if .Contrast}} C:{{.Contrast}}{{end}} {{- if .Brightness}} B:{{.Brightness}}{{end}} {{- if ne .Gamma 100}} G:{{.Gamma}}{{end}} {{- if .Saturation}} S:{{.Saturation}}{{end}})"
	titleLoading := "{{.App}} {{.Version}} [{{.Index}}/{{.Count}}] - Loading..."

	flag.IntVar(&opts.Width, "width", 1024, "Window width [IV_WIDTH].")
	flag.IntVar(&opts.Height, "height", 768, "Window height [IV_HEIGHT].")
	flag.IntVar(&opts.Device, "device", 0, "DRM device index [IV_DEVICE].")
	flag.IntVar(&opts.Filter, "filter", 0, "0=NearestNeighbor, 1=Linear, 2=Bicubic [IV_FILTER].")
	flag.BoolVar(&opts.Title, "title", true, "Show window title [IV_TITLE].")
	flag.StringVar(&opts.TitleFormat, "title-format", titleFormat, "Window title format [IV_TITLE_FORMAT].")
	flag.StringVar(&opts.TitleLoading, "title-loading", titleLoading, "Window title loading [IV_TITLE_LOADING].")
	flag.BoolVar(&opts.Slideshow, "slideshow", false, "Start slideshow [IV_SLIDESHOW].")
	flag.Float64Var(&opts.SlideshowInterval, "slideshow-interval", 4, "Slideshow interval (in seconds) [IV_SLIDESHOW_INTERVAL].")
	flag.BoolVar(&opts.Recursive, "recursive", false, "Process subdirectories recursively [IV_RECURSIVE].")
	flag.BoolVar(&opts.Fullscreen, "fullscreen", false, "Start in fullscreen [IV_FULLSCREEN].")
	flag.BoolVar(&opts.Maximize, "maximize", false, "Start maximized [IV_MAXIMIZE].")
	flag.BoolVar(&opts.Browse, "browse", true, "Load all images from the image directory [IV_BROWSE].")
	flag.BoolVar(&opts.Loop, "loop", false, "Wrap around at the first/last image [IV_LOOP].")
	flag.BoolVar(&opts.Preload, "preload", false, "Preload adjacent images (uses more memory) [IV_PRELOAD].")
	flag.IntVar(&opts.Sort, "sort", 0, "0=No sort, 1=Name (natural order), 2=Modification time, 3=Size, 4=Shuffle [IV_SORT].")
	flag.StringVar(&opts.TextColor, "text-color", "#FFFFFF", "Text color [IV_TEXT_COLOR].")
	flag.StringVar(&opts.BackgroundColor, "background-color", "#000000", "Window background color [IV_BACKGROUND_COLOR].")
	flag.IntVar(&opts.Zoom, "zoom", 0, "Initial zoom level (1, 1000) [IV_ZOOM].")
	flag.IntVar(&opts.Contrast, "contrast", 0, "Adjust contrast (-100, 100) [IV_CONTRAST].")
	flag.IntVar(&opts.Brightness, "brightness", 0, "Adjust brightness (-100, 100) [IV_BRIGHTNESS].")
	flag.IntVar(&opts.Gamma, "gamma", 100, "Adjust gamma (1, 100) [IV_GAMMA].")
	flag.IntVar(&opts.Saturation, "saturation", 0, "Adjust saturation (-100, 100) [IV_SATURATION].")
	flag.BoolVar(&opts.Single, "single", false, "Single instance; send files to a running window [IV_SINGLE].")
	flag.BoolVar(&opts.Wait, "wait", false, "Open a blank window and wait for images (use with --single) [IV_WAIT].")

	var helpKeys bool
	flag.BoolVar(&helpKeys, "help-keys", false, "Show keybindings and exit.")

	flag.Usage = func() {
		color := useColor(os.Stderr)

		stderrf("%s %s [<flags>] [file1 dir1 url1 ... fileOrDirN]\n", colorize(color, colorBold, "Usage:"), appName)
		order := []string{"width", "height", "device", "filter", "title", "slideshow", "slideshow-interval", "recursive",
			"fullscreen", "maximize", "browse", "loop", "preload", "sort", "text-color", "background-color", "zoom", "contrast", "brightness", "gamma", "saturation",
			"single", "wait", "help-keys"}

		for _, name := range order {
			f := flag.Lookup(name)
			if f != nil {
				stderrf("  %s\n    \t%v (default %q)\n", colorize(color, colorCyan, "--"+f.Name), f.Usage, f.DefValue)
			}
		}
	}

	flag.Parse()
	_ = flagconf.ParseEnv()
	_ = flagconf.ParseConfig(filepath.Join(xdg.ConfigHome, appName, "config"))

	if helpKeys {
		printKeys()
		os.Exit(0)
	}

	a := args(flag.Args(), opts.Recursive)
	if len(a) == 0 && !opts.Wait {
		arg := ""
		if len(flag.Args()) > 0 {
			arg = flag.Args()[0]
		}

		switch {
		case arg == "":
			flag.Usage()
		case isURL(arg):
			stderr("no images found")
		default:
			if _, err := os.Stat(arg); err != nil {
				stderr(err)
			} else {
				stderr("no images found")
			}
		}

		os.Exit(1)
	}

	if opts.Single && sendToRunning(absPaths(a)) {
		os.Exit(0)
	}

	items := infos(a, opts.Sort)
	if len(items) == 0 && !opts.Wait {
		stderr("no images found")
		os.Exit(1)
	}

	v, err := newView(opts, items)
	if err != nil {
		stderr(err)
		os.Exit(1)
	}

	v.named = flag.Args()

	if opts.Single {
		v.serveIPC()
	}

	v.startWatch(watchDirs(flag.Args()))

	if err = v.run(); err != nil {
		stderr(err)
	}

	v.closeWatch()
	v.closeIPC()

	if err = v.view.Close(); err != nil {
		stderr(err)
	}
}

func init() {
	if appVersion != "" {
		return
	}

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	if buildInfo.Main.Version != "" {
		appVersion = buildInfo.Main.Version
	}

	for _, kv := range buildInfo.Settings {
		if kv.Value == "" {
			continue
		}

		if kv.Key == "vcs.revision" {
			appVersion = kv.Value
			if len(appVersion) > 7 {
				appVersion = kv.Value[:7]
			}
		}
	}
}

func printKeys() {
	keys := [][2]string{
		{"Next image", "j / Right / Space / Scroll Down"},
		{"Previous image", "k / Left / BackSpace / Scroll Up"},
		{"Go 10 images back", "[ / PageUp"},
		{"Go 10 images forward", "] / PageDown"},
		{"First image", ", / Home"},
		{"Last image", ". / End"},
		{"Jump to image number", "<number>G  (e.g. 12G)"},
		{"Zoom in", "+ / Ctrl+Scroll Up"},
		{"Zoom out", "- / Ctrl+Scroll Down"},
		{"Original size", "Shift+9"},
		{"Fit", "Shift+0"},
		{"Pan zoomed image", "Left / Middle drag"},
		{"Adjust contrast", "Shift+1 / Shift+2"},
		{"Adjust brightness", "Shift+3 / Shift+4"},
		{"Adjust gamma", "Shift+5 / Shift+6"},
		{"Adjust saturation", "Shift+7 / Shift+8"},
		{"Rotate clockwise / counterclockwise", "r / Shift+r"},
		{"Flip horizontal / vertical", "h / v"},
		{"Cycle filter (nearest / linear / bicubic)", "a"},
		{"Cycle sort (none / name / time / size / shuffle)", "o"},
		{"Toggle fullscreen", "f / F11 / Double-click"},
		{"Toggle slideshow", "s"},
		{"Mark / unmark image", "m"},
		{"Toggle EXIF overlay", "i"},
		{"Copy image path to clipboard", "Ctrl+c"},
		{"Copy image to clipboard", "Ctrl+Shift+c"},
		{"Print current image path to stdout", "Enter"},
		{"Quit", "q / Escape"},
	}

	color := useColor(os.Stdout)

	fmt.Println(colorize(color, colorBold, "Keybindings:"))
	for _, k := range keys {
		fmt.Printf("  %-38s%s\n", k[0], colorize(color, colorCyan, k[1]))
	}
}

func args(in []string, recursive bool) []string {
	out := make([]string, 0)

	if piped() {
		in = append(in, lines(os.Stdin)...)
	}

	for _, arg := range in {
		stat, err := os.Stat(arg)
		if err != nil {
			if isURL(arg) {
				out = append(out, expandURL(arg)...)
			} else {
				stderr(err)
			}

			continue
		}

		if !stat.IsDir() {
			out = append(out, arg)
			continue
		}

		if recursive {
			_ = filepath.WalkDir(arg, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					stderr(err)
					return nil
				}

				if !d.IsDir() {
					out = append(out, p)
				}

				return nil
			})

			continue
		}

		g, err := filepath.Glob(filepath.Join(arg, "*"))
		if err != nil {
			stderr(err)
			continue
		}

		for _, p := range g {
			i, err := os.Stat(p)
			if err != nil {
				stderr(err)
				continue
			}

			if !i.IsDir() {
				out = append(out, p)
			}
		}
	}

	return out
}

func lines(r io.Reader) []string {
	out := make([]string, 0)

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		out = append(out, scanner.Text())
	}

	return out
}

func piped() bool {
	f, err := os.Stdin.Stat()
	if err != nil {
		return false
	}

	if f.Mode()&os.ModeNamedPipe == 0 {
		return false
	} else {
		return true
	}
}

const (
	colorBold = "\033[1m"
	colorCyan = "\033[36m"
)

func useColor(f *os.File) bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}

	stat, err := f.Stat()
	if err != nil {
		return false
	}

	return stat.Mode()&os.ModeCharDevice != 0
}

func colorize(on bool, code, s string) string {
	if !on {
		return s
	}

	return code + s + "\033[0m"
}

func stderr(a ...any) {
	_, _ = fmt.Fprintln(os.Stderr, a...)
}

func stderrf(format string, a ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format, a...)
}
