//go:build windows

package main

// Native Windows "Open" dialog (IFileOpenDialog) for picking a folder or a file.
//
// The dialog always runs on the UI thread of the main window (the thread that owns
// the parent window and is a COM single-threaded apartment). Running it from an HTTP
// goroutine with a parent window on another thread crashed the app.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	shell32              = windows.NewLazySystemDLL("shell32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")
	procSHCreateItem     = shell32.NewProc("SHCreateItemFromParsingName")

	clsidFileOpenDialog = windows.GUID{Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4dde, Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidFileOpenDialog   = windows.GUID{Data1: 0xd57c7288, Data2: 0xd4ad, Data3: 0x4768, Data4: [8]byte{0xbe, 0x02, 0x9d, 0x96, 0x95, 0x32, 0xd9, 0x60}}
	iidShellItem        = windows.GUID{Data1: 0x43826d1e, Data2: 0xe718, Data3: 0x42ee, Data4: [8]byte{0xbc, 0x55, 0xa1, 0xe2, 0x61, 0xc3, 0x7b, 0xfe}}

	uiDispatch func(func()) // set by runWindow
	dialogMu   sync.Mutex
)

const (
	vtRelease         = 2
	vtShow            = 3
	vtSetFileTypes    = 4
	vtSetOptions      = 9
	vtGetOptions      = 10
	vtSetFolder       = 12
	vtSetTitle        = 17
	vtGetResult       = 20
	vtGetDisplayName  = 5 // IShellItem
	fosNoChangeDir    = 0x8
	fosPickFolders    = 0x20
	fosForceFS        = 0x40
	fosPathMustExist  = 0x800
	fosFileMustExist  = 0x1000
	sigdnFileSysPath  = 0x80058000
	hrCancelled       = 0x800704C7 // HRESULT_FROM_WIN32(ERROR_CANCELLED)
	clsctxInprocSrv   = 0x1
	coinitApartment   = 0x2
	coinitDisableOLE1 = 0x4
)

// comCall calls method number idx of a COM object.
// obj points to COM-owned memory (never Go memory).
func comCall(obj unsafe.Pointer, idx int, args ...uintptr) uint32 {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, uintptr(idx)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return uint32(r)
}

func failed(hr uint32) bool { return int32(hr) < 0 }

type comFilterSpec struct {
	name *uint16
	spec *uint16
}

// showFileDialog shows the dialog on the UI thread and returns the chosen path ("" = cancelled).
func showFileDialog(title, start string, folder bool, filterName, filterSpec string) (string, error) {
	if !dialogMu.TryLock() {
		return "", errors.New(T("Dialog pro výběr už je otevřený"))
	}
	defer dialogMu.Unlock()

	type result struct {
		path string
		err  error
	}
	ch := make(chan result, 1)
	run := func() {
		defer func() {
			if p := recover(); p != nil {
				ch <- result{"", fmt.Errorf(T("vnitřní chyba dialogu: %v"), p)}
			}
		}()
		p, err := fileDialogOnThisThread(title, start, folder, filterName, filterSpec)
		ch <- result{p, err}
	}
	if uiDispatch != nil {
		uiDispatch(run)
	} else {
		// no window (browser fallback): own OS thread, never unlocked so it is discarded afterwards
		go func() {
			runtime.LockOSThread()
			procCoInitializeEx.Call(0, coinitApartment|coinitDisableOLE1)
			run()
		}()
	}
	r := <-ch
	return r.path, r.err
}

func fileDialogOnThisThread(title, start string, folder bool, filterName, filterSpec string) (string, error) {
	var dlg unsafe.Pointer
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, clsctxInprocSrv,
		uintptr(unsafe.Pointer(&iidFileOpenDialog)), uintptr(unsafe.Pointer(&dlg)))
	if failed(uint32(hr)) || dlg == nil {
		return "", fmt.Errorf(T("Windows dialog nejde otevřít (0x%08X)"), uint32(hr))
	}
	defer comCall(dlg, vtRelease)

	var opts uint32
	comCall(dlg, vtGetOptions, uintptr(unsafe.Pointer(&opts)))
	opts |= fosForceFS | fosPathMustExist | fosNoChangeDir
	if folder {
		opts |= fosPickFolders
	} else {
		opts |= fosFileMustExist
		var filters []comFilterSpec
		if filterSpec != "" {
			n, _ := windows.UTF16PtrFromString(filterName)
			sp, _ := windows.UTF16PtrFromString(filterSpec)
			filters = append(filters, comFilterSpec{n, sp})
		}
		name, _ := windows.UTF16PtrFromString(T("Všechny soubory (*.*)"))
		spec, _ := windows.UTF16PtrFromString("*.*")
		filters = append(filters, comFilterSpec{name, spec})
		comCall(dlg, vtSetFileTypes, uintptr(len(filters)), uintptr(unsafe.Pointer(&filters[0])))
		runtime.KeepAlive(filters)
	}
	if hr := comCall(dlg, vtSetOptions, uintptr(opts)); failed(hr) {
		return "", fmt.Errorf("Windows dialog: SetOptions 0x%08X", hr)
	}
	if t, err := windows.UTF16PtrFromString(title); err == nil {
		comCall(dlg, vtSetTitle, uintptr(unsafe.Pointer(t)))
		runtime.KeepAlive(t)
	}
	if st, err := os.Stat(start); err == nil && !st.IsDir() {
		start = filepath.Dir(start)
	}
	if st, err := os.Stat(start); err == nil && st.IsDir() {
		if p, err := windows.UTF16PtrFromString(start); err == nil {
			var item unsafe.Pointer
			r, _, _ := procSHCreateItem.Call(uintptr(unsafe.Pointer(p)), 0,
				uintptr(unsafe.Pointer(&iidShellItem)), uintptr(unsafe.Pointer(&item)))
			runtime.KeepAlive(p)
			if !failed(uint32(r)) && item != nil {
				comCall(dlg, vtSetFolder, uintptr(item))
				comCall(item, vtRelease)
			}
		}
	}

	hr32 := comCall(dlg, vtShow, mainHWND)
	if hr32 == hrCancelled {
		return "", nil
	}
	if failed(hr32) {
		return "", fmt.Errorf(T("Windows dialog selhal (0x%08X)"), hr32)
	}

	var item unsafe.Pointer
	if hr := comCall(dlg, vtGetResult, uintptr(unsafe.Pointer(&item))); failed(hr) || item == nil {
		return "", fmt.Errorf(T("Windows dialog nevrátil výsledek (0x%08X)"), hr)
	}
	defer comCall(item, vtRelease)
	var psz *uint16
	if hr := comCall(item, vtGetDisplayName, sigdnFileSysPath, uintptr(unsafe.Pointer(&psz))); failed(hr) || psz == nil {
		return "", errors.New(T("Vybraná položka není složka/soubor na disku"))
	}
	path := windows.UTF16PtrToString(psz)
	procCoTaskMemFree.Call(uintptr(unsafe.Pointer(psz)))
	return path, nil
}
