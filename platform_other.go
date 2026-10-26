//go:build !windows

package main

import (
	"errors"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
)

const canPickDir = false

func fatalBox(text string)        { log.Fatal(text) }
func acquireSingleInstance() bool { return true }
func runWindow(url string) bool   { log.Printf(T("otevři %s"), url); return false }
func pickFolder(_, _ string) (string, error) {
	return "", errors.New(T("výběr složky je jen ve Windows – vlož cestu ručně"))
}
func pickFile(_, _, _, _ string) (string, error) {
	return "", errors.New(T("výběr souboru je jen ve Windows – napiš vzor ručně"))
}
func openWithShell(target string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", target).Start()
	}
	return exec.Command("xdg-open", target).Start()
}

func shellCommand(c string) *exec.Cmd { return exec.Command("sh", "-c", c) }

func prepareCmd(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func killTree(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Process.Kill()
	}
}

func radStudioRunning() bool { return false }

func lockHolders(string) []string { return nil }

func stopDropbox(func(kind, format string, a ...any)) bool { return false }
func startDropbox(func(kind, format string, a ...any))     {}
func restoreDropboxAfterCrash()                            {}

// dropboxStatus: outside Windows only for tests – LEONZILLA_FAKE_DROPBOX=running | paused | "running:<tray text>"
func dropboxStatus() DropboxStatus {
	f := os.Getenv("LEONZILLA_FAKE_DROPBOX")
	if f == "" {
		return DropboxStatus{}
	}
	st := DropboxStatus{Running: true}
	switch {
	case f == "paused":
		st.Tray = "Dropbox | Syncing paused"
	case strings.HasPrefix(f, "running:"):
		st.Tray = strings.TrimPrefix(f, "running:")
	}
	if st.Tray != "" {
		st.Known, st.Paused = true, dropboxPausedRe.MatchString(st.Tray)
	}
	return st
}

func dropboxIgnored(string) bool     { return true }
func setDropboxIgnored(string) error { return nil }

const autostartKnown = false

func autostartState() (bool, bool) { return false, false }
func setAutostart(bool) error {
	return errors.New(T("spouštění při startu jde nastavit jen ve Windows"))
}
func startMinimized() bool { return false }

func osLangCzech() bool { return envLangCzech() }
