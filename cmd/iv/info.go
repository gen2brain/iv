package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"image"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/fvbommel/sortorder/casefolded"
)

const pngPeek = 1 << 13

const infoConcurrency = 16

// refinePNG distinguishes APNG from PNG by the presence of an acTL chunk.
func refinePNG(header []byte) (string, bool) {
	i, n := int64(8), int64(len(header))
	for i+8 <= n {
		switch string(header[i+4 : i+8]) {
		case "acTL":
			return "APNG", true
		case "IDAT":
			return "PNG", false
		}

		i += 8 + int64(binary.BigEndian.Uint32(header[i:i+4])) + 4
	}

	return "PNG", false
}

type info struct {
	Id          int
	Name        string
	Base        string
	Format      string
	Width       int
	Height      int
	Size        int64
	ModTime     int64
	IsURL       bool
	IsAnimation bool
}

type byId []info

func (n byId) Len() int           { return len(n) }
func (n byId) Swap(i, j int)      { n[i], n[j] = n[j], n[i] }
func (n byId) Less(i, j int) bool { return n[i].Id < n[j].Id }

type byName []info

func (n byName) Len() int           { return len(n) }
func (n byName) Swap(i, j int)      { n[i], n[j] = n[j], n[i] }
func (n byName) Less(i, j int) bool { return casefolded.NaturalLess(n[i].Name, n[j].Name) }

type byModTime []info

func (n byModTime) Len() int           { return len(n) }
func (n byModTime) Swap(i, j int)      { n[i], n[j] = n[j], n[i] }
func (n byModTime) Less(i, j int) bool { return n[i].ModTime < n[j].ModTime }

type bySize []info

func (n bySize) Len() int           { return len(n) }
func (n bySize) Swap(i, j int)      { n[i], n[j] = n[j], n[i] }
func (n bySize) Less(i, j int) bool { return n[i].Size < n[j].Size }

func fileInfo(fileName string) (info, error) {
	var i info

	f, err := os.Open(fileName)
	if err != nil {
		return i, err
	}

	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return i, err
	}

	br := bufio.NewReaderSize(f, pngPeek)
	header, _ := br.Peek(pngPeek)

	cfg, format, err := image.DecodeConfig(br)
	if err != nil {
		return i, err
	}

	i.Name = fileName
	i.Base = filepath.Base(fileName)
	i.Size = stat.Size()
	i.ModTime = stat.ModTime().Unix()
	i.Format = strings.ToUpper(format)
	i.Width = cfg.Width
	i.Height = cfg.Height
	i.IsAnimation = isAnimation(fileName)

	if format == "apng" {
		i.Format, i.IsAnimation = refinePNG(header)
	}

	return i, nil
}

func urlInfo(uri string) (info, error) {
	var i info

	res, err := httpClient.Get(uri)
	if err != nil {
		return i, err
	}

	defer res.Body.Close()

	if !isImageMIME(res) {
		return i, fmt.Errorf("unsupported mime type: %s", res.Header.Get("Content-Type"))
	}

	br := bufio.NewReaderSize(res.Body, pngPeek)
	header, _ := br.Peek(pngPeek)

	cfg, format, err := image.DecodeConfig(br)
	if err != nil {
		return i, err
	}

	u, err := url.Parse(uri)
	if err != nil {
		return i, err
	}

	i.Name = uri
	i.Base = filepath.Base(u.Path)

	if size, err := strconv.ParseInt(res.Header.Get("Content-Length"), 10, 64); err == nil {
		i.Size = size
	}

	if t, err := http.ParseTime(res.Header.Get("Last-Modified")); err == nil {
		i.ModTime = t.Unix()
	}

	i.Format = strings.ToUpper(format)
	i.Width = cfg.Width
	i.Height = cfg.Height
	i.IsURL = true
	i.IsAnimation = isAnimationMIME(res)

	if format == "apng" {
		i.Format, i.IsAnimation = refinePNG(header)
	}

	return i, nil
}

func infos(a []string, sortBy int) []info {
	var m sync.Mutex
	var wg sync.WaitGroup

	out := make([]info, 0, len(a))
	throttle := make(chan int, infoConcurrency)

	for idx, arg := range a {
		throttle <- 1
		wg.Add(1)

		go func(id int, arg string) {
			defer wg.Done()
			defer func() { <-throttle }()

			var i info
			var err error

			if isURL(arg) {
				i, err = urlInfo(arg)
			} else {
				i, err = fileInfo(arg)
			}

			if err != nil {
				stderr(arg, err)

				return
			}

			i.Id = id

			m.Lock()
			out = append(out, i)
			m.Unlock()
		}(idx, arg)
	}

	wg.Wait()

	sortInfos(out, sortBy)

	return out
}

func sortInfos(out []info, sortBy int) {
	switch sortBy {
	case 0:
		sort.Sort(byId(out))
	case 1:
		sort.Sort(byName(out))
	case 2:
		sort.Sort(byModTime(out))
	case 3:
		sort.Sort(bySize(out))
	case 4:
		rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	}
}

func humanize(s int64) string {
	if s < 10 {
		return fmt.Sprintf("%dB", s)
	}

	var base float64 = 1024
	e := math.Floor(math.Log(float64(s)) / math.Log(base))
	val := math.Floor(float64(s)/math.Pow(base, e)*10+0.5) / 10

	sizes := []string{"B", "K", "M", "G", "T", "P", "E"}
	suffix := sizes[int(e)]

	f := "%.0f%s"
	if val < 10 {
		f = "%.1f%s"
	}

	return fmt.Sprintf(f, val, suffix)
}
