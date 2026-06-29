package main

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func absPaths(a []string) []string {
	out := make([]string, 0, len(a))
	for _, s := range a {
		if isURL(s) {
			out = append(out, s)
			continue
		}

		if abs, err := filepath.Abs(s); err == nil {
			out = append(out, abs)
		} else {
			out = append(out, s)
		}
	}

	return out
}

func sendToRunning(paths []string) bool {
	if len(paths) == 0 {
		return false
	}

	conn, err := net.DialTimeout("unix", singleSocketPath(), time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()

	_, err = conn.Write([]byte(strings.Join(paths, "\n") + "\n"))

	return err == nil
}

func (v *view) serveIPC() {
	path := singleSocketPath()

	ln, err := net.Listen("unix", path)
	if err != nil {
		if c, derr := net.DialTimeout("unix", path, time.Second); derr == nil {
			c.Close()
			return
		}

		_ = os.Remove(path)
		ln, err = net.Listen("unix", path)
		if err != nil {
			stderr(err)
			return
		}
	}

	v.listener = ln
	v.socketPath = path

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			v.handleConn(conn)
		}
	}()
}

func (v *view) handleConn(conn net.Conn) {
	defer conn.Close()

	var paths []string
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			paths = append(paths, line)
		}
	}

	if len(paths) == 0 {
		return
	}

	if items := infos(paths, v.opts.Sort); len(items) > 0 {
		v.push(items)
	}
}

func (v *view) closeIPC() {
	if v.listener != nil {
		v.listener.Close()
		v.listener = nil
	}

	if v.socketPath != "" {
		_ = os.Remove(v.socketPath)
		v.socketPath = ""
	}
}
