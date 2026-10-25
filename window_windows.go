//go:build windows

package main

// Remembers the size, position and maximized state of the main window.
// Stored per PC in %LOCALAPPDATA%\Leonzilla\window.json (not in Dropbox:
// every PC has different monitors).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
	"unsafe"
)

var (
	procGetWindowPlacement = user32.NewProc("GetWindowPlacement")
	procSetWindowPlacement = user32.NewProc("SetWindowPlacement")
	procMonitorFromRect    = user32.NewProc("MonitorFromRect")
	procIsWindow           = user32.NewProc("IsWindow")
	procGetDpiForSystem    = user32.NewProc("GetDpiForSystem")
)

type winRect struct{ Left, Top, Right, Bottom int32 }

type winPlacement struct {
	Length   uint32
	Flags    uint32
	ShowCmd  uint32
	MinPos   [2]int32
	MaxPos   [2]int32
	NormalRc winRect
}

type savedWindow struct {
	X, Y, W, H int32
	Max        bool
}

const (
	swShowNormal    = 1
	swShowMinimized = 2
	swShowMaximized = 3
)

func windowStatePath() string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		local = os.TempDir()
	}
	return filepath.Join(local, "Leonzilla", "window.json")
}

func loadWindowState() *savedWindow {
	b, err := os.ReadFile(windowStatePath())
	if err != nil {
		return nil
	}
	var s savedWindow
	if json.Unmarshal(b, &s) != nil || s.W < 300 || s.H < 200 {
		return nil
	}
	// still on a connected monitor?
	rc := winRect{s.X, s.Y, s.X + s.W, s.Y + s.H}
	if m, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&rc)), 0 /* MONITOR_DEFAULTTONULL */); m == 0 {
		return nil
	}
	return &s
}

// defaultWindowSize scales the default size by the system DPI (125 %, 150 % …).
func defaultWindowSize() (uint, uint) {
	dpi := uintptr(96)
	if procGetDpiForSystem.Find() == nil {
		if d, _, _ := procGetDpiForSystem.Call(); d >= 96 {
			dpi = d
		}
	}
	return uint(1180 * dpi / 96), uint(800 * dpi / 96)
}

func applyWindowState(hwnd uintptr, s *savedWindow) {
	wp := winPlacement{ShowCmd: swShowNormal, NormalRc: winRect{s.X, s.Y, s.X + s.W, s.Y + s.H}}
	wp.Length = uint32(unsafe.Sizeof(wp))
	if s.Max {
		wp.ShowCmd = swShowMaximized
	}
	procSetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&wp)))
}

func readWindowState(hwnd uintptr) *savedWindow {
	var wp winPlacement
	wp.Length = uint32(unsafe.Sizeof(wp))
	if r, _, _ := procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&wp))); r == 0 {
		return nil
	}
	if wp.ShowCmd == swShowMinimized {
		return nil // keep the last normal / maximized state
	}
	rc := wp.NormalRc
	return &savedWindow{X: rc.Left, Y: rc.Top, W: rc.Right - rc.Left, H: rc.Bottom - rc.Top, Max: wp.ShowCmd == swShowMaximized}
}

// watchWindowState saves the window state whenever it changes (checked every second).
// The last change before closing is therefore always saved.
func watchWindowState(hwnd uintptr) {
	var last savedWindow
	if s := readWindowState(hwnd); s != nil {
		last = *s
	}
	for range time.Tick(time.Second) {
		if ok, _, _ := procIsWindow.Call(hwnd); ok == 0 {
			return
		}
		s := readWindowState(hwnd)
		if s == nil || *s == last || s.W < 300 || s.H < 200 {
			continue
		}
		last = *s
		b, _ := json.Marshal(s)
		_ = os.MkdirAll(filepath.Dir(windowStatePath()), 0o755)
		_ = writeFileAtomic(windowStatePath(), b)
	}
}
