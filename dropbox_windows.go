//go:build windows

package main

// Stopping Dropbox during a build: Dropbox opens freshly written files in the build output
// (TMSWeb\…) to upload them, and the TMS compiler then fails with "file is being used by
// another process". Dropbox is stopped only when it is running, and started again afterwards.
// A marker file makes sure it is started again even if Leonzilla is closed / crashes meanwhile.

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type dropboxState struct {
	Cmd string `json:"cmd"` // command line used to start Dropbox again
}

func dropboxMarker() string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		local = os.TempDir()
	}
	return filepath.Join(local, "Leonzilla", "dropbox_stopped.json")
}

// dropboxPIDs returns PIDs and the exe path of running Dropbox.exe processes.
func dropboxPIDs() ([]uint32, string) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, ""
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	var pids []uint32
	exe := ""
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if !strings.EqualFold(windows.UTF16ToString(pe.ExeFile[:]), "Dropbox.exe") {
			continue
		}
		pids = append(pids, pe.ProcessID)
		if exe == "" {
			if h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pe.ProcessID); err == nil {
				buf := make([]uint16, 1024)
				n := uint32(len(buf))
				if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) == nil {
					exe = windows.UTF16ToString(buf[:n])
				}
				windows.CloseHandle(h)
			}
		}
	}
	return pids, exe
}

// dropboxStartCmd: the autostart command from the registry (starts quietly in the tray), or the exe.
func dropboxStartCmd(exe string) string {
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		k, err := registry.OpenKey(root, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := k.GetStringValue("Dropbox")
		k.Close()
		if err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if exe != "" {
		return `"` + exe + `" /systemstartup`
	}
	return ""
}

// stopDropbox stops Dropbox if it runs. Returns true when it was stopped (and must be started again).
func stopDropbox(logf func(kind, format string, a ...any)) bool {
	pids, exe := dropboxPIDs()
	if len(pids) == 0 {
		logf("info", T("Dropbox neběží – nechávám ho, jak je"))
		return false
	}
	cmd := dropboxStartCmd(exe)
	if cmd == "" {
		logf("warn", T("Dropbox běží, ale nevím, jak ho potom znovu spustit – nechávám ho běžet"))
		return false
	}
	b, _ := json.Marshal(dropboxState{Cmd: cmd})
	_ = os.MkdirAll(filepath.Dir(dropboxMarker()), 0o755)
	if err := writeFileAtomic(dropboxMarker(), b); err != nil {
		logf("warn", T("Nejde zapsat %s – Dropbox nezastavuji: %v"), dropboxMarker(), err)
		return false
	}
	k := exec.Command("taskkill", "/F", "/T", "/IM", "Dropbox.exe")
	prepareCmd(k)
	_ = k.Run()
	for i := 0; i < 40; i++ { // up to 10 s
		if p, _ := dropboxPIDs(); len(p) == 0 {
			logf("info", T("Dropbox zastaven na dobu buildu (po buildu ho znovu spustím)"))
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	logf("warn", T("Dropbox se nepodařilo úplně zastavit – pokračuji"))
	return true
}

// startDropbox starts Dropbox again (from the marker) if it is not running.
func startDropbox(logf func(kind, format string, a ...any)) {
	b, err := os.ReadFile(dropboxMarker())
	if err != nil {
		return
	}
	var st dropboxState
	_ = json.Unmarshal(b, &st)
	if p, _ := dropboxPIDs(); len(p) == 0 && st.Cmd != "" {
		if err := runDetached(st.Cmd); err != nil {
			logf("err", T("Dropbox se nepodařilo znovu spustit (%v) – spusť ho prosím ručně"), err)
			return
		}
		logf("info", T("Dropbox znovu spuštěn"))
	}
	_ = os.Remove(dropboxMarker())
}

func runDetached(cmdline string) error {
	exe := cmdline
	if strings.HasPrefix(exe, `"`) {
		if i := strings.Index(exe[1:], `"`); i >= 0 {
			exe = exe[1 : i+1]
		}
	} else if i := strings.Index(exe, " "); i > 0 {
		exe = exe[:i]
	}
	if _, err := os.Stat(exe); err != nil {
		return errors.New("nenalezen " + exe)
	}
	c := exec.Command(exe)
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: cmdline, CreationFlags: 0x00000008 | 0x00000200} // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
	if err := c.Start(); err != nil {
		return err
	}
	return c.Process.Release()
}

// restoreDropboxAfterCrash starts Dropbox if a previous run stopped it and did not start it again.
func restoreDropboxAfterCrash() {
	if _, err := os.Stat(dropboxMarker()); err == nil {
		startDropbox(func(string, string, ...any) {})
	}
}

const dropboxIgnoreStream = ":com.dropbox.ignored"

// dropboxIgnored reports whether the folder carries Dropbox's "ignored" attribute (NTFS stream com.dropbox.ignored = 1).
func dropboxIgnored(p string) bool {
	b, err := os.ReadFile(p + dropboxIgnoreStream)
	return err == nil && strings.TrimSpace(string(b)) == "1"
}

func setDropboxIgnored(p string) error {
	return os.WriteFile(p+dropboxIgnoreStream, []byte("1"), 0o644)
}
