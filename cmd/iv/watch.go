package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

const watchDebounce = 100 * time.Millisecond

// watchDirs returns the unique parent directories of the local named arguments.
func watchDirs(named []string) []string {
	seen := make(map[string]bool)
	var dirs []string

	for _, n := range named {
		if isURL(n) {
			continue
		}

		abs, err := filepath.Abs(n)
		if err != nil {
			continue
		}

		dir := abs
		if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
			dir = filepath.Dir(abs)
		}

		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}

	return dirs
}

func (v *view) startWatch(dirs []string) {
	if len(dirs) == 0 {
		return
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		stderr(err)
		return
	}

	v.watcher = w
	v.watched = make(map[string]bool)
	for _, d := range dirs {
		if err := w.Add(d); err == nil {
			v.watched[d] = true
		}
	}

	go v.watchLoop()
}

// watchPath adds a directory to the watch set if not already watched.
func (v *view) watchPath(path string) {
	if v.watcher == nil || isURL(path) {
		return
	}

	dir := filepath.Dir(path)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}

	if v.watched[dir] {
		return
	}

	if err := v.watcher.Add(dir); err == nil {
		v.watched[dir] = true
	}
}

func (v *view) watchLoop() {
	var timer <-chan time.Time
	var reloadCurrent, reloadList bool

	for {
		select {
		case ev, ok := <-v.watcher.Events:
			if !ok {
				return
			}
			if v.classify(ev, &reloadCurrent, &reloadList) {
				timer = time.After(watchDebounce)
			}
		case <-v.watcher.Errors:
		case <-timer:
			timer = nil

			v.mu.Lock()
			v.reloadCurrent = v.reloadCurrent || reloadCurrent
			v.reloadList = v.reloadList || reloadList
			cancel := v.cancel
			v.mu.Unlock()

			reloadCurrent, reloadList = false, false
			if cancel != nil {
				cancel()
			}
		}
	}
}

func (v *view) classify(ev fsnotify.Event, reloadCurrent, reloadList *bool) bool {
	name := ev.Name
	if abs, err := filepath.Abs(name); err == nil {
		name = abs
	}

	v.mu.Lock()
	cur := v.currentPath
	v.mu.Unlock()

	hit := false

	if name == cur && ev.Op.Has(fsnotify.Write|fsnotify.Create|fsnotify.Rename) {
		*reloadCurrent = true
		hit = true
	}

	if isImage(name) && ev.Op.Has(fsnotify.Create|fsnotify.Remove|fsnotify.Rename) {
		*reloadList = true
		hit = true
	}

	return hit
}

func (v *view) closeWatch() {
	if v.watcher != nil {
		v.watcher.Close()
		v.watcher = nil
	}
}
