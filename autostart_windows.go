//go:build windows

package main

// Start with Windows (minimized): value "Leonzilla" in HKCU\Software\Microsoft\Windows\CurrentVersion\Run.
// It lives in the registry of this PC and user – every PC (Dropbox) decides on its own.

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	runKey         = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue       = "Leonzilla"
	minimizedFlag  = "--minimized"
	autostartKnown = true
)

func exePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe
}

// autostartState: on = the value exists; current = it points to this exe.
func autostartState() (on, current bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false, false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runValue)
	if err != nil || strings.TrimSpace(v) == "" {
		return false, false
	}
	return true, strings.Contains(strings.ToLower(v), strings.ToLower(exePath()))
}

func setAutostart(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue(runValue); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	return k.SetStringValue(runValue, `"`+exePath()+`" `+minimizedFlag)
}

// startMinimized: started by Windows at logon (Run value with --minimized).
func startMinimized() bool {
	for _, a := range os.Args[1:] {
		if strings.EqualFold(a, minimizedFlag) {
			return true
		}
	}
	return false
}
