package main

// Builds of RAD Studio / TMS WEB Core projects before synchronization.
//
// A build (defined once in the app settings, picked by cards) runs:
//   1. optional conversion of line endings to CRLF in the project folder,
//   2. TMSWebCompiler.exe /ParseDprojFile /ProjectFile:… /Config:… /Defines:…,
//   3. the project's own Post-build event from the .dproj (e.g. a script that packs or versions the output),
//   4. an optional extra command.
//
// Switches (e.g. PRODUCTION_VERSION, DEMO_MODE …) come from the top part of Predefines.inc.
// That part is wrapped in {$IFNDEF LEONZILLA} … {$ENDIF}: RAD Studio builds use the file as it is,
// a Leonzilla build skips it and passes LEONZILLA + the switches chosen in the build via /Defines.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

type BuildSwitch struct {
	Name    string `json:"name"`
	Comment string `json:"comment,omitempty"`
}

type Build struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Project   string        `json:"project"`   // path to .dproj
	Config    string        `json:"config"`    // build configuration, e.g. Release
	Platform  string        `json:"platform"`  // Win32 (default)
	IncFile   string        `json:"incFile"`   // switches file, default Predefines.inc next to the .dproj
	Switches  []BuildSwitch `json:"switches"`  // switches known from the file (display order)
	Defines   []string      `json:"defines"`   // switches turned on for this build
	CRLF      bool          `json:"crlf"`      // convert line endings to CRLF before compiling
	CRLFMask  string        `json:"crlfMask"`  // files for CRLF, e.g. *.pas;*.dpr;*.inc;*.dfm
	PostBuild bool          `json:"postBuild"` // run the project's Post-build event
	ExtraCmd  string        `json:"extraCmd"`  // extra command after the build (cmd)
	ExtraArgs string        `json:"extraArgs"` // extra compiler parameters
	// "Info k verzi" (version info): a one-line text passed to the post-build / extra command in the
	// environment variable LEONZILLA_VERSION_INFO (+ an optional custom name, Settings.VersionInfoVar).
	// nil = not set yet → the build name; "" = no info
	VersionInfo *string `json:"versionInfo,omitempty"`
}

// versionInfo returns the version info text (one line, max 80 characters).
func (b *Build) versionInfo() string {
	v := b.Name
	if b.VersionInfo != nil {
		v = *b.VersionInfo
	}
	return cleanVersionInfo(v)
}

func cleanVersionInfo(v string) string {
	v = strings.Join(strings.Fields(v), " ")
	if r := []rune(v); len(r) > 80 {
		v = strings.TrimSpace(string(r[:80]))
	}
	return v
}

const defaultCRLFMask = "*.pas;*.dpr;*.inc;*.dfm"

func (b *Build) normalize() {
	if b.ID == "" {
		b.ID = newID()
	}
	b.Name = strings.TrimSpace(b.Name)
	b.Project = strings.TrimSpace(b.Project)
	b.Config = strings.TrimSpace(b.Config)
	if b.Platform == "" {
		b.Platform = "Win32"
	}
	if b.VersionInfo != nil {
		v := cleanVersionInfo(*b.VersionInfo)
		b.VersionInfo = &v
	}
	if b.Switches == nil {
		b.Switches = []BuildSwitch{}
	}
	if b.Defines == nil {
		b.Defines = []string{}
	}
	if strings.TrimSpace(b.CRLFMask) == "" {
		b.CRLFMask = defaultCRLFMask
	}
}

func (b *Build) incPath() string {
	f := strings.TrimSpace(b.IncFile)
	if f == "" {
		f = "Predefines.inc"
	}
	if !filepath.IsAbs(f) {
		f = filepath.Join(filepath.Dir(b.Project), f)
	}
	return f
}

// ---------------------------------------------------------------------------
// .dproj
// ---------------------------------------------------------------------------

type DprojInfo struct {
	Configs   []string // build configurations (without Base)
	Defines   []string // TMSWebDefines of the chosen config
	PostBuild string   // Post-build event of the chosen config (macros not expanded)
	OutputDir string
}

type dprojGroup struct {
	cond  string
	props [][2]string
}

func parseDproj(path, config, platform string) (*DprojInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf(T("projekt nejde otevřít: %v"), err)
	}
	defer f.Close()
	dec := xml.NewDecoder(f)
	dec.Strict = false
	var groups []dprojGroup
	keys := map[string]string{}    // config name -> key (Cfg_2)
	parents := map[string]string{} // key -> parent key
	var configs []string
	var stack []string
	var cur *dprojGroup
	var curBC struct{ include, key, parent string }
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf(T("projekt nejde přečíst (XML): %v"), err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			stack = append(stack, t.Name.Local)
			text.Reset()
			switch {
			case t.Name.Local == "PropertyGroup":
				cond := ""
				for _, a := range t.Attr {
					if a.Name.Local == "Condition" {
						cond = a.Value
					}
				}
				groups = append(groups, dprojGroup{cond: cond})
				cur = &groups[len(groups)-1]
			case t.Name.Local == "BuildConfiguration":
				curBC.include, curBC.key, curBC.parent = "", "", ""
				for _, a := range t.Attr {
					if a.Name.Local == "Include" {
						curBC.include = a.Value
					}
				}
			}
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			name := t.Name.Local
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			parent := ""
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			switch {
			case name == "PropertyGroup":
				cur = nil
			case parent == "PropertyGroup" && cur != nil:
				cur.props = append(cur.props, [2]string{name, text.String()})
			case parent == "BuildConfiguration" && name == "Key":
				curBC.key = strings.TrimSpace(text.String())
			case parent == "BuildConfiguration" && name == "CfgParent":
				curBC.parent = strings.TrimSpace(text.String())
			case name == "BuildConfiguration":
				if curBC.include != "" && curBC.key != "" {
					keys[strings.ToLower(curBC.include)] = curBC.key
					parents[curBC.key] = curBC.parent
					if !strings.EqualFold(curBC.include, "Base") {
						configs = append(configs, curBC.include)
					}
				}
			}
			text.Reset()
		}
	}
	info := &DprojInfo{Configs: configs}
	if config == "" {
		return info, nil
	}
	key, ok := keys[strings.ToLower(config)]
	if !ok {
		return info, fmt.Errorf(T("projekt nemá konfiguraci %q"), config)
	}
	if platform == "" {
		platform = "Win32"
	}
	// chain of keys from Base to the config
	chain := []string{}
	for k := key; k != "" && k != "Base" && len(chain) < 10; k = parents[k] {
		chain = append([]string{k}, chain...)
	}
	order := []string{"Base", "Base_" + platform}
	for _, k := range chain {
		order = append(order, k, k+"_"+platform)
	}
	props := map[string]string{}
	for _, k := range order {
		want := "'$(" + k + ")'!=''"
		for _, g := range groups {
			if strings.ReplaceAll(g.cond, " ", "") != want {
				continue
			}
			for _, p := range g.props {
				self := "$(" + p[0] + ")"
				props[p[0]] = strings.ReplaceAll(p[1], self, props[p[0]])
			}
		}
	}
	for _, d := range strings.Split(props["TMSWebDefines"], ";") {
		if d = strings.TrimSpace(d); d != "" {
			info.Defines = append(info.Defines, d)
		}
	}
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(props["PostBuildEvent"], "\r", ""), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	info.PostBuild = strings.Join(lines, "\n")
	info.OutputDir = props["TMSWebOutputPath"]
	return info, nil
}

// expandMacros replaces Delphi build-event macros.
func expandMacros(s string, b *Build) string {
	dir := filepath.Dir(b.Project)
	base := filepath.Base(b.Project)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	repl := map[string]string{
		"PROJECTDIR": dir, "PROJECTPATH": b.Project, "PROJECTFILENAME": base, "PROJECTNAME": name,
		"PROJECTEXT": filepath.Ext(base), "CONFIG": b.Config, "PLATFORM": b.Platform,
		"INPUTDIR": dir, "INPUTPATH": b.Project, "INPUTFILENAME": base, "INPUTNAME": name, "INPUTEXT": filepath.Ext(base),
	}
	re := regexp.MustCompile(`\$\(([A-Za-z_]+)\)`)
	return re.ReplaceAllStringFunc(s, func(m string) string {
		k := strings.ToUpper(m[2 : len(m)-1])
		if v, ok := repl[k]; ok {
			return v
		}
		return m
	})
}

// ---------------------------------------------------------------------------
// Predefines.inc
// ---------------------------------------------------------------------------

type IncInfo struct {
	Path      string        `json:"path"`
	Exists    bool          `json:"exists"`
	HasBlock  bool          `json:"hasBlock"` // top part wrapped in {$IFNDEF LEONZILLA}
	Switches  []BuildSwitch `json:"switches"`
	ActiveNow []string      `json:"activeNow"` // switches active in the file (used by RAD Studio builds)
}

var (
	reDefine   = regexp.MustCompile(`(?i)^\s*(//)?\s*\{\$define\s+([A-Za-z_]\w*)\s*\}\s*(?://+\s*(.*))?$`)
	reIfndefLZ = regexp.MustCompile(`(?i)^\s*\{\$ifndef\s+LEONZILLA\s*\}`)
	reIfAny    = regexp.MustCompile(`(?i)^\s*\{\$if`)
	reEndif    = regexp.MustCompile(`(?i)^\s*\{\$(endif|ifend)\b`)
)

func parseInc(path string) *IncInfo {
	info := &IncInfo{Path: path, Switches: []BuildSwitch{}, ActiveNow: []string{}}
	b, err := os.ReadFile(path)
	if err != nil {
		return info
	}
	info.Exists = true
	text := decodeText(b)
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	start, end := -1, -1
	for i, l := range lines {
		if reIfndefLZ.MatchString(l) {
			start = i + 1
			depth := 0
			for k := i + 1; k < len(lines); k++ {
				if reIfAny.MatchString(lines[k]) {
					depth++
				} else if reEndif.MatchString(lines[k]) {
					if depth == 0 {
						end = k
						break
					}
					depth--
				}
			}
			break
		}
	}
	if start >= 0 && end > start {
		info.HasBlock = true
	} else {
		start, end = 0, len(lines) // no {$IFNDEF LEONZILLA} block yet: offer all switches of the file
	}
	seen := map[string]bool{}
	for _, l := range lines[start:end] {
		m := reDefine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		name := m[2]
		key := strings.ToUpper(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		info.Switches = append(info.Switches, BuildSwitch{Name: name, Comment: strings.TrimSpace(m[3])})
		if m[1] == "" {
			info.ActiveNow = append(info.ActiveNow, name)
		}
	}
	return info
}

// decodeText returns UTF-8 text from UTF-8 (with or without BOM) or Windows-1250 bytes.
func decodeText(b []byte) string {
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(b) {
		return string(b)
	}
	s, err := charmap.Windows1250.NewDecoder().Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(s)
}

// ---------------------------------------------------------------------------
// CRLF
// ---------------------------------------------------------------------------

// convertCRLF converts line endings of matching files in dir (not recursive) to CRLF.
// Only files that actually contain a bare LF are rewritten; bytes and BOM are kept.
func convertCRLF(dir, mask string) (changed []string, checked int, err error) {
	var pats []string
	for _, p := range strings.FieldsFunc(mask, func(r rune) bool { return r == ';' || r == ',' || r == ' ' }) {
		pats = append(pats, strings.ToLower(p))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		name := strings.ToLower(e.Name())
		match := false
		for _, p := range pats {
			if ok, _ := filepath.Match(p, name); ok {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		fp := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(fp)
		if err != nil || len(b) > 50<<20 {
			continue
		}
		checked++
		head := b
		if len(head) > 8192 {
			head = head[:8192]
		}
		if bytes.IndexByte(head, 0) >= 0 {
			continue // binary (e.g. binary .dfm)
		}
		if !hasBareLF(b) {
			continue
		}
		out := bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
		out = bytes.ReplaceAll(out, []byte("\n"), []byte("\r\n"))
		fi, _ := os.Stat(fp)
		mode := os.FileMode(0o644)
		if fi != nil {
			mode = fi.Mode().Perm()
		}
		tmp := fp + ".lz_tmp"
		if err := os.WriteFile(tmp, out, mode); err != nil {
			return changed, checked, err
		}
		if err := os.Rename(tmp, fp); err != nil {
			os.Remove(tmp)
			return changed, checked, err
		}
		changed = append(changed, e.Name())
	}
	return changed, checked, nil
}

func hasBareLF(b []byte) bool {
	for i, c := range b {
		if c == '\n' && (i == 0 || b[i-1] != '\r') {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Compiler
// ---------------------------------------------------------------------------

func compilerCandidates() []string {
	c := []string{
		`C:\TMS_smart_setup\Products\tms.webcore\Bin\Win32\TMSWebCompiler.exe`,
		`C:\TMS_smart_setup\Products\tms.webcore\Compiler\TMSWebCompiler.exe`,
	}
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "PUBLIC"} {
		if base := os.Getenv(env); base != "" {
			m, _ := filepath.Glob(filepath.Join(base, "tmssoftware", "TMS WEB Core*", "Bin", "Win32", "TMSWebCompiler.exe"))
			c = append(c, m...)
			m, _ = filepath.Glob(filepath.Join(base, "Documents", "tmssoftware", "TMS WEB Core*", "Bin", "Win32", "TMSWebCompiler.exe"))
			c = append(c, m...)
		}
	}
	return c
}

// resolveCompiler returns the configured compiler or the first one found.
func resolveCompiler(configured string) string {
	if p := strings.TrimSpace(configured); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
		return ""
	}
	for _, p := range compilerCandidates() {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

var (
	reCompErr  = regexp.MustCompile(`(?i)\[(error|fatal)\]|\)\s*(error|fatal)\s*:|^\s*(fatal|error)\s*:`)
	reCompWarn = regexp.MustCompile(`(?i)\[warning\]|\)\s*warning\s*:|^\s*warning\s*:`)
	rePostErr  = regexp.MustCompile(`(?i)\berror\b|\bchyba\b|traceback|selhal`)
	reLocked   = regexp.MustCompile(`(?i)cannot (?:create|open|write) file\s+"([^"]+)".*(?:another process|jiný proces|being used|využívá)`)
)

// splitArgs splits extra arguments, honoring "double quotes".
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inQ, has := false, false
	for _, r := range s {
		switch {
		case r == '"':
			inQ = !inQ
			has = true
		case (r == ' ' || r == '\t' || r == '\n' || r == '\r') && !inQ:
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if has {
		out = append(out, cur.String())
	}
	return out
}

// ---------------------------------------------------------------------------
// Running
// ---------------------------------------------------------------------------

type Step struct {
	Name  string `json:"name"`
	State string `json:"state"` // pending | running | ok | failed | skipped
	Note  string `json:"note,omitempty"`
}

func (j *Job) setStep(i int, state, note string) {
	j.mu.Lock()
	if i >= 0 && i < len(j.Steps) {
		j.Steps[i].State = state
		if note != "" {
			j.Steps[i].Note = note
		}
	}
	j.mu.Unlock()
}

type buildPlan struct {
	b     *Build
	light *BuildLight // card's "Build light" (Python script) instead of b
	dproj *DprojInfo
	post  string // expanded post-build command
	info  string // version info for the post-build (environment variable)
	steps []int  // indexes into Job.Steps: crlf, compile, post, extra (-1 = not used)
}

// prepareBuilds resolves the builds of a card and creates the step list.
func (r *Runner) prepareBuilds(j *Job, tab *Tab) ([]*buildPlan, error) {
	r.store.mu.Lock()
	all := map[string]*Build{}
	for _, b := range r.store.cfg.Settings.Builds {
		c := *b
		c.Switches = append([]BuildSwitch{}, b.Switches...)
		c.Defines = append([]string{}, b.Defines...)
		all[b.ID] = &c
	}
	r.store.mu.Unlock()
	var plans []*buildPlan
	for _, id := range tab.Builds {
		b := all[id]
		if b == nil {
			continue
		}
		p := &buildPlan{b: b, steps: []int{-1, -1, -1, -1}, info: b.versionInfo()}
		if v, ok := tab.BuildInfo[b.ID]; ok { // remembered on the card
			p.info = cleanVersionInfo(v)
		}
		if v, ok := j.buildInfo[b.ID]; ok { // changed in the confirmation for this run only
			p.info = cleanVersionInfo(v)
		}
		add := func(slot int, name string) {
			p.steps[slot] = len(j.Steps)
			j.Steps = append(j.Steps, &Step{Name: name, State: "pending"})
		}
		if b.CRLF {
			add(0, b.Name+T(": konce řádků CRLF"))
		}
		add(1, b.Name+T(": kompilace"))
		if b.PostBuild {
			if info, err := parseDproj(b.Project, b.Config, b.Platform); err == nil && info.PostBuild != "" {
				p.post = expandMacros(info.PostBuild, b)
				add(2, b.Name+T(": post-build"))
			}
		}
		if strings.TrimSpace(b.ExtraCmd) != "" {
			add(3, b.Name+T(": příkaz po buildu"))
		}
		plans = append(plans, p)
	}
	if tab.BuildLight != nil && tab.BuildLight.Script != "" {
		l := *tab.BuildLight
		p := &buildPlan{light: &l, steps: []int{-1, len(j.Steps), -1, -1}}
		j.Steps = append(j.Steps, &Step{Name: T("Skript") + ": " + l.name(), State: "pending"})
		plans = append(plans, p)
	}
	if len(plans) == 0 {
		return nil, errors.New(T("Karta nemá vybraný žádný build"))
	}
	return plans, nil
}

func (r *Runner) runBuilds(ctx context.Context, j *Job, plans []*buildPlan) (string, string) {
	r.store.mu.Lock()
	compiler := resolveCompiler(r.store.cfg.Settings.Compiler)
	stopDB := r.store.cfg.Settings.StopDropbox
	ignoreOut := r.store.cfg.Settings.IgnoreOutput == nil || *r.store.cfg.Settings.IgnoreOutput
	infoVar := r.store.cfg.Settings.VersionInfoVar
	r.store.mu.Unlock()

	heavy := false // a RAD Studio build (kept for logging; Dropbox is stopped for any build since 1.11.12)
	for _, p := range plans {
		heavy = heavy || p.b != nil
	}
	_ = heavy
	if stopDB || j.stopDB { // the setting or the choice in the Dropbox check – any build (RAD, own script) since 1.11.12
		if stopDropbox(j.logf) {
			defer startDropbox(j.logf) // also after a failure, cancel or panic
		}
	}

	fail := func(step int, msg string) (string, string) {
		j.setStep(step, "failed", msg)
		for _, s := range j.Steps {
			if s.State == "pending" {
				j.mu.Lock()
				s.State = "skipped"
				j.mu.Unlock()
			}
		}
		return "failed", msg
	}
	for bi, p := range plans {
		if p.light != nil {
			if res, msg, ok := r.runBuildLight(ctx, j, p, fail); !ok {
				return res, msg
			}
			continue
		}
		b := p.b
		t0 := time.Now()
		j.logf("info", "══ Build %d/%d: %s (konfigurace %s) ══", bi+1, len(plans), b.Name, b.Config)
		if _, err := os.Stat(b.Project); err != nil {
			return fail(p.steps[1], fmt.Sprintf("%s: projekt %s nenalezen", b.Name, b.Project))
		}
		dir := filepath.Dir(b.Project)
		if ignoreOut {
			ignoreBuildOutput(j, dir)
		}

		// 1) CRLF
		if s := p.steps[0]; s >= 0 {
			j.setStep(s, "running", "")
			changed, checked, err := convertCRLF(dir, b.CRLFMask)
			if err != nil {
				return fail(s, fmt.Sprintf(T("Převod CRLF selhal: %v"), err))
			}
			for _, f := range changed {
				j.logf("info", "CRLF: %s", f)
			}
			note := fmt.Sprintf(T("převedeno %d z %d souborů"), len(changed), checked)
			if len(changed) == 0 {
				note = fmt.Sprintf(T("vše už CRLF (%d souborů)"), checked)
			}
			j.logf("info", T("Konce řádků: %s"), note)
			j.setStep(s, "ok", note)
		}
		if ctx.Err() != nil {
			return "cancelled", T("Přerušeno")
		}

		// 2) compile
		s := p.steps[1]
		j.setStep(s, "running", "")
		if compiler == "" {
			return fail(s, T("Nenalezen TMSWebCompiler.exe – nastav cestu v Nastavení aplikace → Buildy"))
		}
		info, err := parseDproj(b.Project, b.Config, b.Platform)
		if err != nil {
			return fail(s, err.Error())
		}
		inc := parseInc(b.incPath())
		if inc.Exists && !inc.HasBlock {
			return fail(s, fmt.Sprintf(T("%s nemá horní část v bloku {$IFNDEF LEONZILLA} … {$ENDIF} – přepínače z buildu by se neuplatnily"), filepath.Base(inc.Path)))
		}
		if inc.Exists {
			known := map[string]bool{}
			for _, sw := range b.Switches {
				known[strings.ToUpper(sw.Name)] = true
			}
			for _, sw := range inc.Switches {
				if !known[strings.ToUpper(sw.Name)] {
					j.logf("warn", T("V %s je nový přepínač %s – v buildu „%s“ není nastavený (Nastavení aplikace → Buildy → Načíst přepínače)"), filepath.Base(inc.Path), sw.Name, b.Name)
				}
			}
		}
		defs := []string{}
		seen := map[string]bool{}
		for _, d := range append(append(append([]string{}, info.Defines...), "LEONZILLA"), b.Defines...) {
			if k := strings.ToUpper(d); d != "" && !seen[k] {
				seen[k] = true
				defs = append(defs, d)
			}
		}
		args := []string{"/ParseDprojFile", "/ProjectFile:" + b.Project, "/Config:" + b.Config, "/Platform:" + b.Platform,
			"/Defines:" + strings.Join(defs, ";")}
		args = append(args, splitArgs(b.ExtraArgs)...)
		j.logf("info", T("Přepínače: %s"), strings.Join(defs, ", "))
		j.logf("info", "> %s %s", filepath.Base(compiler), strings.Join(args, " "))
		var code, errLines int
		const maxAttempts = 4
		for attempt := 1; ; attempt++ {
			locked := ""
			cmd := exec.Command(compiler, args...)
			cmd.Dir = dir
			code, errLines, err = runProcess(ctx, j, cmd, func(l string) string {
				if m := reLocked.FindStringSubmatch(l); m != nil {
					locked = m[1]
					return "err"
				}
				switch {
				case reCompErr.MatchString(l):
					return "err"
				case reCompWarn.MatchString(l):
					return "warn"
				}
				return "out"
			})
			if ctx.Err() != nil || err != nil || (code == 0 && errLines == 0) || locked == "" || attempt >= maxAttempts {
				if locked != "" && code != 0 {
					if who := lockHolders(locked); len(who) > 0 {
						j.logf("err", T("Soubor drží: %s"), strings.Join(who, ", "))
					}
				}
				break
			}
			// a file in the output folder is locked for a moment (Dropbox, antivirus, RAD Studio web server …): wait and compile again
			who := lockHolders(locked)
			holder := T("jiný program")
			if len(who) > 0 {
				holder = strings.Join(who, ", ")
			}
			wait := time.Duration(attempt*3) * time.Second
			j.logf("warn", T("Soubor %s je zamčený (%s) – za %d s kompiluji znovu (pokus %d/%d)"), filepath.Base(locked), holder, int(wait.Seconds()), attempt+1, maxAttempts)
			j.setStep(s, "running", fmt.Sprintf(T("soubor zamčený, pokus %d/%d"), attempt+1, maxAttempts))
			select {
			case <-time.After(wait):
			case <-ctx.Done():
			}
		}
		if ctx.Err() != nil {
			return fail(s, T("Přerušeno"))
		}
		if err != nil {
			return fail(s, fmt.Sprintf(T("Kompilátor nejde spustit: %v"), err))
		}
		if code != 0 || errLines > 0 {
			msg := fmt.Sprintf("Kompilace „%s“ selhala", b.Name)
			if code != 0 {
				msg += fmt.Sprintf(T(" (návratový kód %d)"), code)
			}
			if errLines > 0 {
				msg += fmt.Sprintf(", %d chyb", errLines)
			}
			return fail(s, msg)
		}
		j.setStep(s, "ok", humanDur(time.Since(t0).Seconds()))

		// 3) post-build of the project
		if s := p.steps[2]; s >= 0 {
			j.setStep(s, "running", "")
			for _, line := range strings.Split(p.post, "\n") {
				j.logf("info", "> %s", line)
			}
			cmd := shellCommand(strings.ReplaceAll(p.post, "\n", " & "))
			cmd.Dir = dir
			cmd.Env = postEnv(j, infoVar, p.info)
			code, _, err := runProcess(ctx, j, cmd, postKind)
			if ctx.Err() != nil {
				return fail(s, T("Přerušeno"))
			}
			if err != nil || code != 0 {
				return fail(s, fmt.Sprintf(T("Post-build „%s“ selhal (kód %d)"), b.Name, code))
			}
			j.setStep(s, "ok", "")
		}

		// 4) extra command
		if s := p.steps[3]; s >= 0 {
			j.setStep(s, "running", "")
			c := expandMacros(strings.TrimSpace(b.ExtraCmd), b)
			j.logf("info", "> %s", c)
			cmd := shellCommand(strings.ReplaceAll(c, "\n", " & "))
			cmd.Dir = dir
			cmd.Env = postEnv(j, infoVar, p.info)
			code, _, err := runProcess(ctx, j, cmd, postKind)
			if ctx.Err() != nil {
				return fail(s, T("Přerušeno"))
			}
			if err != nil || code != 0 {
				return fail(s, fmt.Sprintf(T("Příkaz po buildu „%s“ selhal (kód %d)"), b.Name, code))
			}
			j.setStep(s, "ok", "")
		}
		j.logf("info", T("Build „%s“ hotový za %s"), b.Name, humanDur(time.Since(t0).Seconds()))
	}
	if len(plans) == 1 && plans[0].light != nil {
		// only the card's own script: Leonzilla does not know what it does (it may upload by itself), so just say it went fine
		return "ok", fmt.Sprintf(T("Skript „%s“ doběhl bez chyby"), filepath.Base(strings.Trim(strings.TrimSpace(plans[0].light.Script), `"`)))
	}
	return "ok", fmt.Sprintf(T("Build hotový (%d)"), len(plans))
}

// versionInfoEnv is always set for the post-build and the extra command when the version info is not empty.
const versionInfoEnv = "LEONZILLA_VERSION_INFO"

// postEnv: variables for the post-build / extra command: the version info in LEONZILLA_VERSION_INFO
// and, when configured (Settings → Builds), also in a variable with a custom name that the
// project's own post-build script expects. Nothing is set when the info is empty.
func postEnv(j *Job, varName, info string) []string {
	names := []string{versionInfoEnv}
	if v := strings.TrimSpace(varName); v != "" && !strings.EqualFold(v, versionInfoEnv) {
		names = append(names, v)
	}
	if info == "" {
		j.logf("info", T("Info k verzi: prázdné – %s se nenastavuje"), strings.Join(names, ", "))
		return nil
	}
	var env []string
	for _, n := range names {
		env = append(env, n+"="+info)
	}
	j.logf("info", T("Info k verzi → %s=%s"), strings.Join(names, ", "), info)
	return env
}

func postKind(l string) string {
	if rePostErr.MatchString(l) {
		return "err"
	}
	return "out"
}

// runProcess runs cmd, streams its output line by line into the job log and returns the exit code.
// classify returns the log kind of a line ("err" lines are counted).
func runProcess(ctx context.Context, j *Job, cmd *exec.Cmd, classify func(string) string) (int, int, error) {
	extra := cmd.Env // variables set by the caller (post-build: version info)
	cmd.Env = append(append(os.Environ(), "PYTHONUTF8=1", "PYTHONIOENCODING=utf-8", "LEONZILLA=1"), extra...)
	prepareCmd(cmd)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		pw.Close()
		return -1, 0, err
	}
	done := make(chan struct{})
	errLines := 0
	go func() {
		defer close(done)
		rd := bufio.NewReaderSize(pr, 64*1024)
		var buf []byte
		flush := func() {
			line := strings.TrimRight(decodeText(buf), " \t")
			buf = buf[:0]
			if strings.TrimSpace(line) == "" {
				return
			}
			k := classify(line)
			if k == "err" {
				errLines++
			}
			j.logf(k, "%s", line)
		}
		for {
			c, err := rd.ReadByte()
			if err != nil {
				if len(buf) > 0 {
					flush()
				}
				return
			}
			if c == '\n' || c == '\r' {
				if len(buf) > 0 {
					flush()
				}
				continue
			}
			buf = append(buf, c)
			if len(buf) > 8000 {
				flush()
			}
		}
	}()
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait(); pw.Close() }()
	var err error
	select {
	case err = <-waitErr:
	case <-ctx.Done():
		killTree(cmd)
		err = <-waitErr
	}
	<-done
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		err = nil
	}
	return code, errLines, err
}

// ignoreBuildOutput excludes <project>\TMSWeb from Dropbox sync (same attribute as "Ignore" in Dropbox):
// Dropbox then never opens the freshly compiled files, so it cannot lock them during a build.
func ignoreBuildOutput(j *Job, projectDir string) {
	out := filepath.Join(projectDir, "TMSWeb")
	if err := os.MkdirAll(out, 0o755); err != nil {
		j.logf("warn", T("Složku %s nejde vytvořit: %v"), out, err)
		return
	}
	if dropboxIgnored(out) {
		j.logf("info", T("Výstup buildu %s je vyřazený z Dropboxu"), out)
		return
	}
	if err := setDropboxIgnored(out); err != nil {
		j.logf("warn", T("Složku %s nejde vyřadit z Dropboxu: %v"), out, err)
		return
	}
	j.logf("info", T("Výstup buildu %s vyřazen z Dropboxu (nesynchronizuje se – Dropbox ho nebude zamykat)"), out)
}
