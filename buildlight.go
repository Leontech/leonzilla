package main

// "Build light": a card runs its own Python script (e.g. a build / post-process script of a web project)
// instead of (or after) the RAD Studio builds; on success the card uploads to the server as after a build.
// On the card it looks and behaves like a build (Build + Synchronizovat, Jen build, Jen nahrát).

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type BuildLight struct {
	Script string `json:"script"`          // path to the .py script (another file type runs via cmd)
	Args   string `json:"args,omitempty"`  // parameters for the script
	Label  string `json:"label,omitempty"` // own text of the main card button, e.g. "Build and upload to my web" (empty = default)
}

func (b *BuildLight) normalize() {
	b.Script = strings.Trim(strings.TrimSpace(b.Script), `"`)
	b.Args = strings.TrimSpace(b.Args)
	b.Label = strings.Join(strings.Fields(b.Label), " ")
	if r := []rune(b.Label); len(r) > 60 {
		b.Label = string(r[:60])
	}
}

func (b *BuildLight) name() string { return filepath.Base(b.Script) }

// findPython returns the Python interpreter (+ its leading arguments):
// python from PATH (not the Microsoft Store stub), then the py launcher.
func findPython() (string, []string) {
	var stub string
	if p, err := exec.LookPath("python"); err == nil {
		if runtime.GOOS == "windows" && strings.Contains(strings.ToLower(p), `\windowsapps\`) {
			stub = p
		} else {
			return p, nil
		}
	}
	if runtime.GOOS != "windows" {
		if p, err := exec.LookPath("python3"); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath("py"); err == nil {
		return p, []string{"-3"}
	}
	if stub != "" {
		return stub, nil
	}
	return "", nil
}

func (r *Runner) runBuildLight(ctx context.Context, j *Job, p *buildPlan, fail func(int, string) (string, string)) (string, string, bool) {
	b := p.light
	s := p.steps[1]
	t0 := time.Now()
	j.logf("info", "══ Build light: %s ══", b.name())
	j.setStep(s, "running", "")
	if st, err := os.Stat(b.Script); err != nil || st.IsDir() {
		res, msg := fail(s, fmt.Sprintf("Skript %s nenalezen", b.Script))
		return res, msg, false
	}
	dir := filepath.Dir(b.Script)
	args := splitArgs(b.Args)
	var cmd *exec.Cmd
	switch strings.ToLower(filepath.Ext(b.Script)) {
	case ".py", ".pyw":
		py, pre := findPython()
		if py == "" {
			res, msg := fail(s, T("Nenalezen Python (python ani py v PATH) – nainstaluj Python a zaškrtni „Add to PATH“"))
			return res, msg, false
		}
		j.logf("info", "> %s %s", strings.TrimSpace(filepath.Base(py)+" "+strings.Join(pre, " ")), strings.TrimSpace(filepath.Base(b.Script)+" "+b.Args))
		cmd = exec.Command(py, append(append(pre, b.Script), args...)...)
	default:
		line := `"` + b.Script + `"`
		if b.Args != "" {
			line += " " + b.Args
		}
		j.logf("info", "> %s", line)
		cmd = shellCommand(line)
	}
	cmd.Dir = dir
	cmd.Env = []string{"PYTHONUNBUFFERED=1"}
	code, _, err := runProcess(ctx, j, cmd, postKind)
	if ctx.Err() != nil {
		res, msg := fail(s, T("Přerušeno"))
		return res, msg, false
	}
	if err != nil {
		res, msg := fail(s, fmt.Sprintf(T("Skript %s nejde spustit: %v"), b.name(), err))
		return res, msg, false
	}
	if code != 0 {
		res, msg := fail(s, fmt.Sprintf(T("Skript %s skončil chybou (kód %d)"), b.name(), code))
		return res, msg, false
	}
	j.setStep(s, "ok", humanDur(time.Since(t0).Seconds()))
	j.logf("info", T("Build light „%s“ hotový za %s"), b.name(), humanDur(time.Since(t0).Seconds()))
	return "", "", true
}
