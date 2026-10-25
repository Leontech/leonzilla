//go:build windows

package main

import (
	"errors"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const osNeedsIdfProfile = true

type serialPort interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Reset() error
	Close() error
}

type winSerial struct {
	h      windows.Handle
	closed bool
}

func openSerial(name string, baud int) (serialPort, error) {
	p, err := windows.UTF16PtrFromString(`\\.\` + name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return nil, errors.New(T("port používá jiný program (VS Code monitor, jiná Leonzilla…)"))
		}
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			return nil, errors.New(T("port neexistuje – je zařízení připojené?"))
		}
		return nil, err
	}
	var dcb windows.DCB
	dcb.DCBlength = uint32(unsafe.Sizeof(dcb))
	if err := windows.GetCommState(h, &dcb); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	dcb.BaudRate = uint32(baud)
	dcb.ByteSize = 8
	dcb.Parity = windows.NOPARITY
	dcb.StopBits = windows.ONESTOPBIT
	// fBinary=1; DTR and RTS disabled (low) – the ESP32 keeps running (no reset, no bootloader)
	dcb.Flags = 0x0001
	if err := windows.SetCommState(h, &dcb); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	// Read returns what is there, or after 100 ms
	to := windows.CommTimeouts{ReadIntervalTimeout: 0xFFFFFFFF, ReadTotalTimeoutMultiplier: 0xFFFFFFFF, ReadTotalTimeoutConstant: 100, WriteTotalTimeoutConstant: 2000}
	if err := windows.SetCommTimeouts(h, &to); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	_ = windows.EscapeCommFunction(h, windows.CLRDTR)
	_ = windows.EscapeCommFunction(h, windows.CLRRTS)
	return &winSerial{h: h}, nil
}

func (s *winSerial) Read(b []byte) (int, error) {
	var n uint32
	err := windows.ReadFile(s.h, b, &n, nil)
	if s.closed {
		return 0, errors.New(T("zavřeno"))
	}
	return int(n), err
}

func (s *winSerial) Write(b []byte) (int, error) {
	var n uint32
	err := windows.WriteFile(s.h, b, &n, nil)
	return int(n), err
}

func (s *winSerial) Reset() error {
	if err := windows.EscapeCommFunction(s.h, windows.SETRTS); err != nil {
		return err
	}
	time.Sleep(120 * time.Millisecond)
	return windows.EscapeCommFunction(s.h, windows.CLRRTS)
}

func (s *winSerial) Close() error {
	s.closed = true
	return windows.CloseHandle(s.h)
}

type serialInfo struct {
	Name string `json:"name"`
	Desc string `json:"desc"`
}

// listSerialPorts: COM ports from HARDWARE\DEVICEMAP\SERIALCOMM, with the device name (USB enum) when known.
func listSerialPorts() []serialInfo {
	desc := map[string]string{}
	for _, bus := range []string{`SYSTEM\CurrentControlSet\Enum\USB`, `SYSTEM\CurrentControlSet\Enum\FTDIBUS`} {
		root, err := registry.OpenKey(registry.LOCAL_MACHINE, bus, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		devs, _ := root.ReadSubKeyNames(-1)
		root.Close()
		for _, d := range devs {
			dk, err := registry.OpenKey(registry.LOCAL_MACHINE, bus+`\`+d, registry.ENUMERATE_SUB_KEYS)
			if err != nil {
				continue
			}
			insts, _ := dk.ReadSubKeyNames(-1)
			dk.Close()
			for _, in := range insts {
				base := bus + `\` + d + `\` + in
				pk, err := registry.OpenKey(registry.LOCAL_MACHINE, base+`\Device Parameters`, registry.QUERY_VALUE)
				if err != nil {
					continue
				}
				port, _, _ := pk.GetStringValue("PortName")
				pk.Close()
				if port == "" {
					continue
				}
				if ik, err := registry.OpenKey(registry.LOCAL_MACHINE, base, registry.QUERY_VALUE); err == nil {
					fn, _, _ := ik.GetStringValue("FriendlyName")
					ik.Close()
					if fn != "" {
						desc[strings.ToUpper(port)] = strings.TrimSpace(strings.Split(fn, " (COM")[0])
					}
				}
			}
		}
	}
	var out []serialInfo
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DEVICEMAP\SERIALCOMM`, registry.QUERY_VALUE)
	if err == nil {
		names, _ := k.ReadValueNames(-1)
		for _, n := range names {
			if v, _, err := k.GetStringValue(n); err == nil && v != "" {
				out = append(out, serialInfo{Name: v, Desc: desc[strings.ToUpper(v)]})
			}
		}
		k.Close()
	}
	sort.Slice(out, func(i, j int) bool { return comNum(out[i].Name) < comNum(out[j].Name) })
	return out
}

func comNum(s string) int {
	n := 0
	for _, c := range strings.TrimPrefix(strings.ToUpper(s), "COM") {
		if c < '0' || c > '9' {
			return 1 << 20
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// fwCommand runs a build script in PowerShell with ESP-IDF activated by the EIM profile.
func fwCommand(profile, project, script string, args []string) *exec.Cmd {
	var sb strings.Builder
	sb.WriteString("$ErrorActionPreference='Continue'; ")
	if profile != "" {
		sb.WriteString(". " + psQuote(profile) + " | Out-Null; ")
	}
	sb.WriteString("Set-Location -LiteralPath " + psQuote(project) + "; & " + psQuote(script))
	for _, a := range args {
		sb.WriteString(" " + psQuote(a))
	}
	sb.WriteString("; exit $LASTEXITCODE")
	cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", sb.String())
	cmd.Dir = project
	cmd.Env = []string{"PYTHONUNBUFFERED=1"} // idf.py output line by line (added to the environment by runProcess)
	return cmd
}

// openIdfTerminal opens a normal PowerShell window with ESP-IDF activated in the project folder (menuconfig …).
func openIdfTerminal(profile, project string) error {
	c := ""
	if profile != "" {
		c = ". " + psQuote(profile) + "; "
	}
	c += "Set-Location -LiteralPath " + psQuote(project)
	cmd := exec.Command("powershell.exe", "-NoExit", "-ExecutionPolicy", "Bypass", "-Command", c)
	cmd.Dir = project
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000010} // CREATE_NEW_CONSOLE
	return cmd.Start()
}
