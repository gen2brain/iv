package main

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

const urlCacheLimit = 64 << 20

var (
	urlMu    sync.Mutex
	urlData  = map[string][]byte{}
	urlOrder []string
	urlBytes int
)

func fetchURL(uri string) ([]byte, error) {
	urlMu.Lock()
	if b, ok := urlData[uri]; ok {
		urlOrder = append(slices.DeleteFunc(urlOrder, func(u string) bool { return u == uri }), uri)
		urlMu.Unlock()
		return b, nil
	}
	urlMu.Unlock()

	res, err := httpClient.Get(uri)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	urlMu.Lock()
	urlData[uri] = b
	urlOrder = append(urlOrder, uri)
	urlBytes += len(b)
	for urlBytes > urlCacheLimit && len(urlOrder) > 1 {
		old := urlOrder[0]
		urlOrder = urlOrder[1:]
		urlBytes -= len(urlData[old])
		delete(urlData, old)
	}
	urlMu.Unlock()

	return b, nil
}

var httpClient = &http.Client{
	Timeout: 60 * time.Second,
	Transport: &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
}

func isURL(arg string) bool {
	u, err := url.Parse(arg)
	if err != nil {
		return false
	}

	if u.Scheme == "http" || u.Scheme == "https" {
		return true
	}

	return false
}

func isHTML(res *http.Response) bool {
	return strings.HasPrefix(res.Header.Get("Content-Type"), "text/html")
}

func expandURL(arg string) []string {
	if res, err := httpClient.Head(arg); err == nil {
		ct := res.Header.Get("Content-Type")
		res.Body.Close()
		if res.StatusCode < 400 && ct != "" && !strings.HasPrefix(ct, "text/html") {
			return []string{arg}
		}
	}

	return scrapeImages(arg)
}

func scrapeImages(arg string) []string {
	res, err := httpClient.Get(arg)
	if err != nil {
		stderr(err)
		return []string{arg}
	}
	defer res.Body.Close()

	if !isHTML(res) {
		return []string{arg}
	}

	base, err := url.Parse(arg)
	if err != nil {
		stderr(err)
		return nil
	}

	out := make([]string, 0)
	seen := make(map[string]bool)

	z := html.NewTokenizer(res.Body)
	for {
		switch z.Next() {
		case html.ErrorToken:
			return out
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			if string(name) != "img" || !hasAttr {
				continue
			}

			var src, dataSrc, srcset, dataSrcset string
			for {
				key, val, more := z.TagAttr()
				switch strings.ToLower(string(key)) {
				case "src":
					src = string(val)
				case "data-src":
					dataSrc = string(val)
				case "srcset":
					srcset = string(val)
				case "data-srcset":
					dataSrcset = string(val)
				}

				if !more {
					break
				}
			}

			for _, cand := range []string{src, dataSrc, lastSrcset(srcset), lastSrcset(dataSrcset)} {
				if cand == "" {
					continue
				}

				u, err := base.Parse(cand)
				if err != nil || !isURL(u.String()) {
					continue
				}

				if !seen[u.String()] {
					seen[u.String()] = true
					out = append(out, u.String())
				}

				break
			}
		}
	}
}

func lastSrcset(s string) string {
	parts := strings.Split(s, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		if f := strings.Fields(parts[i]); len(f) > 0 {
			return f[0]
		}
	}

	return ""
}

func isImageMIME(res *http.Response) bool {
	if slices.Contains(formatsMime, res.Header.Get("Content-Type")) {
		return true
	}

	return false
}

func isAnimationMIME(res *http.Response) bool {
	if slices.Contains(animationsMime, res.Header.Get("Content-Type")) {
		return true
	}

	return false
}
