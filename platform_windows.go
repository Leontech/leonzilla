//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

const canPickDir = true

var (
	user32            = windows.NewLazySystemDLL("user32.dll")
	procMessageBox    = user32.NewProc("MessageBoxW")
	procFindWindow    = user32.NewProc("FindWindowW")
	procShowWindow    = user32.NewProc("ShowWindow")
	procSetForeground = user32.NewProc("SetForegroundWindow")
	procIsIconic      = user32.NewProc("IsIconic")
	mainHWND          uintptr
)

func msgBox(text string, flags uintptr) {
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString(appTitle)
	procMessageBox.Call(mainHWND, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), flags)
}

func fatalBox(text string) { msgBox(text, 0x10) }

// acquireSingleInstance: a second start just brings the running window to front.
func acquireSingleInstance() bool {
	name, _ := syscall.UTF16PtrFromString("Local\\Leonzilla-single-instance")
	_, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if startMinimized() {
			return false // autostart while Leonzilla already runs – don't pop up the window
		}
		title, _ := syscall.UTF16PtrFromString(appTitle)
		h, _, _ := procFindWindow.Call(0, uintptr(unsafe.Pointer(title)))
		if h != 0 {
			if ic, _, _ := procIsIconic.Call(h); ic != 0 {
				procShowWindow.Call(h, 9) // SW_RESTORE
			}
			procSetForeground.Call(h)
		}
		return false
	}
	return true
}

func runWindow(url string) bool {
	runtime.LockOSThread()
	// WebView2 profile must NOT live in Dropbox – keep it per PC.
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		local = os.TempDir()
	}
	saved := loadWindowState()
	width, height := defaultWindowSize()
	if saved != nil {
		width, height = uint(saved.W), uint(saved.H)
	}
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: !startMinimized(),
		DataPath:  filepath.Join(local, "Leonzilla", "WebView2"),
		WindowOptions: webview2.WindowOptions{
			Title: appTitle, Width: width, Height: height, IconId: 1, Center: true,
		},
	})
	if w == nil {
		msgBox(T("Chybí Microsoft Edge WebView2 Runtime – Leonzilla se otevře v prohlížeči.\n(WebView2 je součástí Windows 11, na Windows 10 ho lze doinstalovat.)"), 0x30)
		return false
	}
	defer w.Destroy()
	mainHWND = uintptr(w.Window())
	uiDispatch = w.Dispatch
	w.SetSize(900, 600, webview2.HintMin)
	if saved != nil {
		applyWindowState(mainHWND, saved)
	}
	if startMinimized() {
		procShowWindow.Call(mainHWND, 7) // SW_SHOWMINNOACTIVE – on the taskbar, without stealing focus
	}
	go watchWindowState(mainHWND)
	w.Navigate(url)
	w.Run()
	return true
}

func pickFolder(title, start string) (string, error) {
	if title == "" {
		title = T("Vyber složku")
	}
	return showFileDialog(title, start, true, "", "")
}

func pickFile(title, start, filterName, filterSpec string) (string, error) {
	if title == "" {
		title = T("Vyber soubor")
	}
	return showFileDialog(title, start, false, filterName, filterSpec)
}

func openWithShell(target string) error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

const createNoWindow = 0x08000000

// shellCommand runs a command line through cmd.exe exactly as written (like a Delphi build event).
func shellCommand(c string) *exec.Cmd {
	comspec := os.Getenv("ComSpec")
	if comspec == "" {
		comspec = "cmd.exe"
	}
	cmd := exec.Command(comspec)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /S /C "` + c + `"`}
	return cmd
}

// prepareCmd hides the console window of a child process.
func prepareCmd(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}

// killTree stops the process and all its children (cmd → py → …).
func killTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	k := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	prepareCmd(k)
	_ = k.Run()
	_ = cmd.Process.Kill()
}

// radStudioRunning reports whether RAD Studio (bds.exe) is running.
func radStudioRunning() bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if strings.EqualFold(windows.UTF16ToString(pe.ExeFile[:]), "bds.exe") {
			return true
		}
	}
	return false
}

// lockHolders lists programs that have the file open (Windows Restart Manager).
func lockHolders(path string) []string {
	rm := windows.NewLazySystemDLL("rstrtmgr.dll")
	start, reg, list, end := rm.NewProc("RmStartSession"), rm.NewProc("RmRegisterResources"), rm.NewProc("RmGetList"), rm.NewProc("RmEndSession")
	if start.Find() != nil {
		return nil
	}
	var session uint32
	key := make([]uint16, 64)
	if r, _, _ := start.Call(uintptr(unsafe.Pointer(&session)), 0, uintptr(unsafe.Pointer(&key[0]))); r != 0 {
		return nil
	}
	defer end.Call(uintptr(session))
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil
	}
	files := []*uint16{p}
	if r, _, _ := reg.Call(uintptr(session), 1, uintptr(unsafe.Pointer(&files[0])), 0, 0, 0, 0); r != 0 {
		return nil
	}
	type rmProcessInfo struct {
		PID         uint32
		StartTime   windows.Filetime
		AppName     [256]uint16
		ServiceName [64]uint16
		AppType     int32
		AppStatus   uint32
		TSSessionID uint32
		Restartable int32
	}
	var needed, count uint32 = 0, 16
	infos := make([]rmProcessInfo, 16)
	var reason uint32
	if r, _, _ := list.Call(uintptr(session), uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)),
		uintptr(unsafe.Pointer(&infos[0])), uintptr(unsafe.Pointer(&reason))); r != 0 {
		return nil
	}
	runtime.KeepAlive(files)
	var out []string
	for i := 0; i < int(count) && i < len(infos); i++ {
		name := windows.UTF16ToString(infos[i].AppName[:])
		if name == "" {
			name = "?"
		}
		out = append(out, fmt.Sprintf("%s (PID %d)", name, infos[i].PID))
	}
	return out
}

// osLangCzech: Windows display language is Czech or Slovak (primary language IDs 0x05 / 0x1B).
func osLangCzech() bool {
	r, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage").Call()
	p := r & 0x3ff
	return p == 0x05 || p == 0x1b
}
