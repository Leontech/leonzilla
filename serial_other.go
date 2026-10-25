//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const osNeedsIdfProfile = false

type serialPort interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Reset() error
	Close() error
}

type fileSerial struct{ f *os.File }

func openSerial(name string, _ int) (serialPort, error) {
	f, err := os.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return &fileSerial{f}, nil
}

func (s *fileSerial) Read(b []byte) (int, error)  { return s.f.Read(b) }
func (s *fileSerial) Write(b []byte) (int, error) { return s.f.Write(b) }
func (s *fileSerial) Reset() error                { return nil }
func (s *fileSerial) Close() error                { return s.f.Close() }

type serialInfo struct {
	Name string `json:"name"`
	Desc string `json:"desc"`
}

func listSerialPorts() []serialInfo {
	var out []serialInfo
	var names []string
	for _, p := range []string{"/dev/ttyUSB*", "/dev/ttyACM*"} {
		m, _ := filepath.Glob(p)
		names = append(names, m...)
	}
	if t := os.Getenv("LEONZILLA_TEST_PORTS"); t != "" {
		names = append(names, strings.Split(t, ",")...)
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, serialInfo{Name: n})
	}
	return out
}

// fwCommand: sh with the (test) profile sourced, script run from the project folder.
func fwCommand(profile, project, script string, args []string) *exec.Cmd {
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	c := ""
	if profile != "" {
		c = ". " + q(profile) + " >/dev/null; "
	}
	c += "cd " + q(project) + " && sh " + q(strings.ReplaceAll(script, `\`, "/"))
	for _, a := range args {
		c += " " + q(a)
	}
	cmd := exec.Command("sh", "-c", c)
	cmd.Dir = project
	return cmd
}

func openIdfTerminal(_, _ string) error {
	return os.ErrInvalid
}
