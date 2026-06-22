//go:build windows

package main

func absPaths(a []string) []string { return a }

func sendToRunning(paths []string) bool { return false }

func (v *view) serveIPC() {}

func (v *view) closeIPC() {}
