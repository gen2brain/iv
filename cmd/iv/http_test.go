package main

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestExpandURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body>
			<img src="/a.jpg">
			<img src="sub/b.png"/>
			<img src="https://example.com/c.gif">
			<img src="/a.jpg">
			<img src="data:image/png;base64,iVBORw0KGgo=">
			<img src="data:image/gif;base64,AAAA" data-src="/lazy.jpg">
			<img srcset="small.jpg 480w, large.jpg 1200w">
			<p>not an image</p>
		</body></html>`))
	})
	mux.HandleFunc("/img.jpg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	got := expandURL(srv.URL + "/page")
	want := []string{
		srv.URL + "/a.jpg",
		srv.URL + "/sub/b.png",
		"https://example.com/c.gif",
		srv.URL + "/lazy.jpg",
		srv.URL + "/large.jpg",
	}

	if len(got) != len(want) {
		t.Fatalf("scraped %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("scraped %v, want %v", got, want)
		}
	}

	img := expandURL(srv.URL + "/img.jpg")
	if len(img) != 1 || img[0] != srv.URL+"/img.jpg" {
		t.Fatalf("non-HTML url not passed through: %v", img)
	}
}

func TestURLInfoMissingHeaders(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		// No Content-Length or Last-Modified; the flush forces a chunked response.
		w.Write(data[:len(data)/2])
		w.(http.Flusher).Flush()
		w.Write(data[len(data)/2:])
	}))
	defer srv.Close()

	i, err := urlInfo(srv.URL + "/x.png")
	if err != nil {
		t.Fatalf("urlInfo: %v", err)
	}
	if i.Width != 3 || i.Height != 2 {
		t.Fatalf("dimensions %dx%d, want 3x2", i.Width, i.Height)
	}
	if i.Format == "" {
		t.Fatal("format not detected")
	}
}

func TestFetchURLCache(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	for i := 0; i < 3; i++ {
		b, err := fetchURL(srv.URL + "/x")
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != "payload" {
			t.Fatalf("got %q", b)
		}
	}

	if got := hits.Load(); got != 1 {
		t.Fatalf("server hit %d times, want 1 (cache miss)", got)
	}
}
