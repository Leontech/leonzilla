package main

// "Build light": a card runs its own script – Python (.py), batch (.bat / .cmd) or PowerShell (.ps1) – e.g. a build /
// post-process script of a web project or a release script
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
	Script string `json:"script"`          // path to the script: .py via Python, .ps1 via PowerShell, others (.bat, .cmd, .exe) via cmd
	Args   string `json:"args,omitempty"`  // parameters for the script
	Label  string `json:"label,omitempty"` // own text of the main card button, e.g. "Build and upload to my web" (empty = default)
	// more variants of the parameters (Args = the first one, may be empty); with more variants the confirmation asks which one
	MoreArgs []string `json:"moreArgs,omitempty"`
	LastArgs *string  `json:"lastArgs,omitempty"` // variant chosen last time (only with more variants; may be "")
	// notes – what the script / a variant of the parameters does; shown in the card settings and when choosing what to run.
	// ArgNotes go with argVariants() (0 = Args, 1… = MoreArgs). Optional: cards without them stay as they were.
	Note     string   `json:"note,omitempty"`
	ArgNotes []string `json:"argNotes,omitempty"`
}

func cleanScriptNote(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200])
	}
	return s
}

func (b *BuildLight) normalize() {
	b.Script = strings.Trim(strings.TrimSpace(b.Script), `"`)
	b.Args = strings.TrimSpace(b.Args)
	b.Label = strings.Join(strings.Fields(b.Label), " ")
	if r := []rune(b.Label); len(r) > 60 {
		b.Label = string(r[:60])
	}
	b.Note = cleanScriptNote(b.Note)
	note := func(i int) string { // note of the i-th variant as it came (0 = Args)
		if i < len(b.ArgNotes) {
			return cleanScriptNote(b.ArgNotes[i])
		}
		return ""
	}
	// variants of the parameters without duplicates (the first one stays in Args); their notes move with them
	seen := map[string]int{b.Args: 0}
	notes := []string{note(0)}
	var more []string
	for i, a := range b.MoreArgs {
		a = strings.TrimSpace(a)
		if k, dup := seen[a]; dup {
			if notes[k] == "" {
				notes[k] = note(i + 1)
			}
			continue
		}
		seen[a] = len(notes)
		notes = append(notes, note(i+1))
		more = append(more, a)
	}
	b.MoreArgs = more
	for len(notes) > 0 && notes[len(notes)-1] == "" { // no notes = no field in the config
		notes = notes[:len(notes)-1]
	}
	b.ArgNotes = notes
	if len(notes) == 0 {
		b.ArgNotes = nil
	}
	if b.LastArgs != nil {
		v := strings.TrimSpace(*b.LastArgs)
		b.LastArgs = &v
		if _, ok := seen[v]; len(b.MoreArgs) == 0 || !ok {
			b.LastArgs = nil
		}
	}
}

// argVariants returns all variants of the parameters (the first = Args).
func (b *BuildLight) argVariants() []string { return append([]string{b.Args}, b.MoreArgs...) }

func (b *BuildLight) name() string { return filepath.Base(b.Script) }

// key identifies a script in the choice of scripts (path + first parameters).
func (b *BuildLight) key() string { return b.Script + "|" + b.Args }

// normalizeScripts drops scripts without a path; when the first one (BuildLight) is gone, the next one takes its place.
func (t *Tab) normalizeScripts() {
	var more []*BuildLight
	for _, m := range t.MoreScripts {
		if m == nil {
			continue
		}
		m.normalize()
		if m.Script != "" {
			more = append(more, m)
		}
	}
	t.MoreScripts = more
	if t.BuildLight != nil {
		t.BuildLight.normalize()
		if t.BuildLight.Script == "" {
			t.BuildLight = nil
		}
	}
	if t.ScriptPick != "" {
		ok := false
		for _, s := range t.scripts() {
			ok = ok || s.key() == t.ScriptPick
		}
		if !ok {
			t.ScriptPick = ""
		}
	}
	if t.BuildLight == nil && len(t.MoreScripts) > 0 {
		t.BuildLight, t.MoreScripts = t.MoreScripts[0], t.MoreScripts[1:]
	}
	if len(t.MoreScripts) == 0 {
		t.MoreScripts = nil
		t.ScriptPick = "" // one script = no choice of scripts
	}
}

// scripts returns all own scripts of the card in their order.
func (t *Tab) scripts() []*BuildLight {
	var out []*BuildLight
	if t.BuildLight != nil && t.BuildLight.Script != "" {
		out = append(out, t.BuildLight)
	}
	for _, m := range t.MoreScripts {
		if m != nil && m.Script != "" {
			out = append(out, m)
		}
	}
	return out
}

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
	j.logf("info", "══ "+T("Skript")+": %s ══", b.name())
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
	case ".ps1":
		ps := "powershell.exe"
		if runtime.GOOS != "windows" {
			ps = "pwsh"
		}
		j.logf("info", "> powershell -File %s", strings.TrimSpace(filepath.Base(b.Script)+" "+b.Args))
		cmd = exec.Command(ps, append([]string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", b.Script}, args...)...)
	default: // .bat, .cmd, .exe … through cmd (call = also batch files that end with exit /b)
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
	j.logf("info", T("Skript „%s“ hotový za %s"), b.name(), humanDur(time.Since(t0).Seconds()))
	return "", "", true
}
