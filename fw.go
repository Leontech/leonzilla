package main

// FW build (ESP-IDF): a card can build / flash an ESP-IDF project through its tools\build_*.cmd scripts
// (activated ESP-IDF environment = the EIM PowerShell profile) and open a serial monitor with input.
// The monitor is Leonzilla's own (idf.py monitor needs a real console).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type FwConfig struct {
	Project    string       `json:"project"`    // ESP-IDF project folder (CMakeLists.txt)
	IdfProfile string       `json:"idfProfile"` // EIM PowerShell profile that activates ESP-IDF; "" = found automatically
	Variants   []*FwVariant `json:"variants"`
	Baud       int          `json:"baud,omitempty"`    // monitor speed, 0 = 115200
	Monitor    bool         `json:"monitor,omitempty"` // "Monitor" button on the card (serial console, also without build)
	// last choice in the FW window (per card)
	LastVariant string `json:"lastVariant,omitempty"`
	LastAction  string `json:"lastAction,omitempty"`
	LastPort    string `json:"lastPort,omitempty"`
	LastClean   bool   `json:"lastClean,omitempty"`       // "Clean config" checked last time
	LastReconf  *bool  `json:"lastReconfigure,omitempty"` // "Reconfigure" last time (nil = default on)
}

type FwVariant struct {
	ID       string `json:"id"`
	Name     string `json:"name"`     // e.g. DEV, PROD, BOARD_B …
	Script   string `json:"script"`   // relative to the project, e.g. tools\build_dev.cmd
	BuildDir string `json:"buildDir"` // e.g. build_dev (clean config = delete <buildDir>\sdkconfig)
	NoFlash  bool   `json:"noFlash"`  // PROD: flashing forbidden
}

func (c *FwConfig) normalize() {
	c.Project = strings.TrimSpace(c.Project)
	c.IdfProfile = strings.TrimSpace(c.IdfProfile)
	if c.Variants == nil {
		c.Variants = []*FwVariant{}
	}
	for _, v := range c.Variants {
		if v.ID == "" {
			v.ID = newID()
		}
		v.Name = strings.TrimSpace(v.Name)
		v.Script = strings.TrimSpace(v.Script)
		v.BuildDir = strings.Trim(strings.TrimSpace(v.BuildDir), `\/`)
	}
}

func (c *FwConfig) variant(id string) *FwVariant {
	for _, v := range c.Variants {
		if v.ID == id {
			return v
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// detection of variants (tools\build_*.cmd) and of the ESP-IDF profile

var reBuildDir = regexp.MustCompile(`(?i)\s-B\s+"?([^\s"]+)`)

func detectFwVariants(project string) ([]*FwVariant, error) {
	project = strings.TrimSpace(project)
	if st, err := os.Stat(project); err != nil || !st.IsDir() {
		return nil, errors.New(T("Složka projektu neexistuje"))
	}
	files, _ := filepath.Glob(filepath.Join(project, "tools", "build_*.cmd"))
	if len(files) == 0 {
		files, _ = filepath.Glob(filepath.Join(project, "build_*.cmd"))
	}
	sort.Slice(files, func(i, k int) bool { // DEV before PROD, then by name
		pi, pk := strings.Contains(strings.ToLower(files[i]), "prod"), strings.Contains(strings.ToLower(files[k]), "prod")
		if pi != pk {
			return !pi
		}
		return files[i] < files[k]
	})
	var out []*FwVariant
	for _, f := range files {
		b, _ := os.ReadFile(f)
		txt := string(b)
		base := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
		name := strings.ToUpper(strings.ReplaceAll(strings.TrimPrefix(strings.ToLower(base), "build_"), "_", " "))
		rel, _ := filepath.Rel(project, f)
		v := &FwVariant{ID: newID(), Name: name, Script: rel, BuildDir: "build"}
		if m := reBuildDir.FindStringSubmatch(txt); m != nil {
			v.BuildDir = strings.Trim(m[1], `\/`)
		}
		low := strings.ToLower(txt)
		// a PROD script that refuses "flash" (e.g. `echo %* | findstr /i "flash"` → exit) – no flashing from Leonzilla
		v.NoFlash = strings.Contains(low, `"flash" zakazano`) || strings.Contains(low, `"flash" forbidden`) || strings.Contains(low, `findstr /i "flash"`)
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil, errors.New(T("Ve složce tools nejsou skripty build_*.cmd – přidej variantu ručně"))
	}
	return out, nil
}

// idfProfile returns the profile from the card or the newest EIM profile in C:\Espressif\tools.
func idfProfile(c *FwConfig) string {
	if c.IdfProfile != "" {
		return c.IdfProfile
	}
	return findIdfProfile()
}

func findIdfProfile() string {
	var cand []string
	for _, root := range []string{`C:\Espressif\tools`, filepath.Join(os.Getenv("USERPROFILE"), ".espressif", "tools")} {
		m, _ := filepath.Glob(filepath.Join(root, "Microsoft.*.PowerShell_profile.ps1"))
		cand = append(cand, m...)
	}
	if len(cand) == 0 {
		return ""
	}
	sort.Strings(cand)
	return cand[len(cand)-1]
}

// projectBin guesses the produced image: <buildDir>\<project name>.bin (project() in CMakeLists.txt).
var reCmakeProject = regexp.MustCompile(`(?i)project\(\s*([A-Za-z0-9_\-]+)`)

func projectBin(project, buildDir string) string {
	b, err := os.ReadFile(filepath.Join(project, "CMakeLists.txt"))
	if err != nil {
		return ""
	}
	m := reCmakeProject.FindStringSubmatch(string(b))
	if m == nil {
		return ""
	}
	return filepath.Join(project, buildDir, m[1]+".bin")
}

// ---------------------------------------------------------------------------
// sessions

type fwSession struct {
	j        *Job
	mu       sync.Mutex
	port     serialPort
	portName string
	action   string
	variant  string
}

type FwManager struct {
	mu    sync.Mutex
	s     map[string]*fwSession // tab ID → last session
	store *Store
}

func newFwManager(s *Store) *FwManager { return &FwManager{s: map[string]*fwSession{}, store: s} }

func (m *FwManager) get(tabID string) *fwSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.s[tabID]
}

// Running reports the tab IDs with a running FW session (build or monitor).
func (m *FwManager) Running() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for id, s := range m.s {
		s.j.mu.Lock()
		if s.j.Phase != "done" {
			out[id] = s.j.Phase
		}
		s.j.mu.Unlock()
	}
	return out
}

type fwRunReq struct {
	Variant  string `json:"variant"`
	Action   string `json:"action"` // build | flash | flashmon | monitor
	Port     string `json:"port"`
	CleanCfg bool   `json:"cleanCfg"`
	Reconfig bool   `json:"reconfigure"` // run "<script> reconfigure" before the build
	StopDB   bool   `json:"stopDropbox"` // stop Dropbox during build + flash (chosen in the Dropbox check)
}

var fwActions = map[string][3]bool{ // build, flash, monitor
	"build":    {true, false, false},
	"flash":    {true, true, false},
	"flashmon": {true, true, true},
	"monitor":  {false, false, true},
}

func (m *FwManager) Start(tab *Tab, req fwRunReq) error {
	c := tab.Fw
	do, ok := fwActions[req.Action]
	if !ok {
		return errors.New(T("neznámá akce"))
	}
	if c == nil || (c.Project == "" && (do[0] || req.CleanCfg)) {
		return errors.New(T("Karta nemá nastavený FW build (složka projektu)"))
	}
	if !do[0] && !req.CleanCfg {
		req.Variant = "" // monitor only – no project needed
	}
	v := c.variant(req.Variant)
	if v == nil && (do[0] || req.CleanCfg) {
		return errors.New(T("Vyber variantu buildu"))
	}
	if do[1] && v.NoFlash {
		return fmt.Errorf(T("Varianta „%s“ nemá povolené nahrávání (PROD) – jen build"), v.Name)
	}
	if (do[1] || do[2]) && strings.TrimSpace(req.Port) == "" {
		return errors.New("Vyber COM port")
	}
	if do[0] || req.CleanCfg {
		if st, err := os.Stat(c.Project); err != nil || !st.IsDir() {
			return fmt.Errorf(T("Složka projektu neexistuje: %s"), c.Project)
		}
	}
	if v != nil && do[0] {
		if _, err := os.Stat(filepath.Join(c.Project, v.Script)); err != nil {
			return fmt.Errorf(T("Skript neexistuje: %s"), filepath.Join(c.Project, v.Script))
		}
	}
	profile := idfProfile(c)
	if do[0] && profile == "" && osNeedsIdfProfile {
		return errors.New(T("Nenašel jsem aktivaci ESP-IDF (C:\\Espressif\\tools\\Microsoft.*.PowerShell_profile.ps1) – vyplň ji v nastavení karty"))
	}

	m.mu.Lock()
	if old := m.s[tab.ID]; old != nil {
		old.j.mu.Lock()
		running := old.j.Phase != "done"
		old.j.mu.Unlock()
		if running {
			m.mu.Unlock()
			return errors.New(T("Na kartě už FW build / monitor běží"))
		}
	}
	port := strings.TrimSpace(req.Port)
	if do[1] || do[2] {
		for id, s := range m.s {
			s.mu.Lock()
			busy := s.port != nil && strings.EqualFold(s.portName, port)
			s.mu.Unlock()
			if busy && id != tab.ID {
				m.mu.Unlock()
				return fmt.Errorf(T("%s má otevřený monitor jiné karty – nejdřív ho zastav"), port)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &Job{TabID: tab.ID, Build: true, Phase: "building", Started: time.Now(), cancel: cancel,
		active: map[int]string{}, lastT: time.Now(), Log: []LogLine{}, Steps: []*Step{}}
	s := &fwSession{j: j, portName: port, action: req.Action}
	if v != nil {
		s.variant = v.Name
	}
	m.s[tab.ID] = s
	m.mu.Unlock()

	if lf, err := openLog(m.store.dir, tab.ID); err == nil {
		j.logFile = lf
	}
	steps := map[string]int{}
	add := func(key, name string) {
		steps[key] = len(j.Steps)
		j.Steps = append(j.Steps, &Step{Name: name, State: "pending"})
	}
	if req.CleanCfg && v != nil {
		add("clean", "Clean config ("+filepath.Join(v.BuildDir, "sdkconfig")+")")
	}
	if do[0] {
		if req.Reconfig {
			add("reconfigure", "Reconfigure "+v.Name)
		}
		add("build", "Build "+v.Name)
	}
	if do[1] {
		add("flash", T("Nahrát do ")+port)
	}
	if do[2] {
		add("monitor", "Monitor "+port)
	}
	go m.run(ctx, s, tab, c, v, profile, do, req.CleanCfg, req.StopDB, steps)
	return nil
}

func (m *FwManager) run(ctx context.Context, s *fwSession, tab *Tab, c *FwConfig, v *FwVariant, profile string, do [3]bool, clean, stopDB bool, steps map[string]int) {
	j := s.j
	result := "ok"
	defer func() {
		s.mu.Lock()
		if s.port != nil {
			s.port.Close()
			s.port = nil
		}
		s.mu.Unlock()
		j.mu.Lock()
		j.Result = result
		j.Phase = "done"
		j.Ended = time.Now()
		for _, st := range j.Steps {
			if st.State == "pending" || st.State == "running" {
				st.State = "skipped"
			}
		}
		lf := j.logFile
		j.logFile = nil
		j.mu.Unlock()
		j.logf("info", "FW %s – %s", map[string]string{"ok": T("hotovo"), "failed": T("nepovedlo se"), "cancelled": T("přerušeno")}[result], humanDur(time.Since(j.Started).Seconds()))
		if lf != nil {
			lf.Close()
		}
	}()
	fail := func(key, msg string) {
		if ctx.Err() != nil {
			result = "cancelled"
			j.setStep(steps[key], "failed", T("Přerušeno"))
			return
		}
		result = "failed"
		j.setStep(steps[key], "failed", msg)
		j.logf("err", "%s", msg)
	}
	if do[0] {
		j.logf("info", "FW build „%s“ · projekt %s", tab.Name, c.Project)
	}
	if profile != "" && do[0] {
		j.logf("info", "ESP-IDF: %s", profile)
	}

	// Dropbox stopped for build + flash on request (Dropbox check), started again before the monitor / at the end
	dbStopped := false
	restartDB := func() {
		if dbStopped {
			dbStopped = false
			startDropbox(j.logf)
		}
	}
	defer restartDB()
	if stopDB && do[0] {
		dbStopped = stopDropbox(j.logf)
	}

	if i, ok := steps["clean"]; ok {
		j.setStep(i, "running", "")
		p := filepath.Join(c.Project, v.BuildDir, "sdkconfig")
		switch err := os.Remove(p); {
		case err == nil:
			j.logf("ok", T("Smazáno %s – vytvoří se znovu ze sdkconfig.defaults"), p)
			j.setStep(i, "ok", T("smazáno"))
		case os.IsNotExist(err):
			j.logf("info", T("%s neexistuje – není co mazat"), p)
			j.setStep(i, "ok", T("nebyl"))
		default:
			fail("clean", T("sdkconfig nejde smazat: ")+err.Error())
			return
		}
	}
	runScript := func(key string, args ...string) bool {
		i := steps[key]
		j.setStep(i, "running", "")
		t0 := time.Now()
		cmd := fwCommand(profile, c.Project, v.Script, args)
		j.logf("info", "> %s %s", v.Script, strings.Join(args, " "))
		code, errLines, err := runProcess(ctx, j, cmd, fwKind)
		if ctx.Err() != nil {
			fail(key, T("Přerušeno"))
			return false
		}
		if err != nil || code != 0 {
			msg := fmt.Sprintf(T("%s selhal (kód %d)"), map[string]string{"build": "Build", "flash": T("Nahrání"), "reconfigure": "Reconfigure"}[key], code)
			if errLines > 0 {
				msg += fmt.Sprintf(", %d chyb", errLines)
			}
			fail(key, msg)
			return false
		}
		j.setStep(i, "ok", humanDur(time.Since(t0).Seconds()))
		return true
	}
	if _, ok := steps["reconfigure"]; ok {
		if !runScript("reconfigure", "reconfigure") {
			return
		}
	}
	if do[0] && !do[1] {
		if !runScript("build", "build") {
			return
		}
		if bin := projectBin(c.Project, v.BuildDir); bin != "" {
			if _, err := os.Stat(bin); err == nil {
				j.logf("ok", T("Výsledek: %s"), bin)
			}
		}
	}
	if do[1] {
		// "build" step = idf.py build, then flash (incremental, nothing is compiled twice)
		if !runScript("build", "build") {
			return
		}
		if !runScript("flash", "-p", s.portName, "flash") {
			return
		}
	}
	restartDB()
	if do[2] {
		i := steps["monitor"]
		j.setStep(i, "running", "")
		baud := c.Baud
		if baud <= 0 {
			baud = 115200
		}
		var p serialPort
		var err error
		for try := 0; try < 10; try++ { // after flashing the USB port comes back after a moment
			if p, err = openSerial(s.portName, baud); err == nil || ctx.Err() != nil {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if err != nil {
			fail("monitor", fmt.Sprintf(T("%s nejde otevřít: %v"), s.portName, err))
			return
		}
		s.mu.Lock()
		s.port = p
		s.mu.Unlock()
		j.setPhase("monitor")
		j.logf("ok", T("Monitor %s (%d Bd) – příkazy piš do řádku dole, Enter odešle"), s.portName, baud)
		readSerial(ctx, j, p)
		j.setStep(i, "ok", T("ukončen"))
	}
}

// fwKind classifies idf.py / esptool output.
var (
	reFwErr  = regexp.MustCompile(`(?i)(\berror\b[: ]|\bFAILED\b|CMake Error|fatal error|A fatal error occurred)`)
	reFwWarn = regexp.MustCompile(`(?i)\bwarning\b[: ]`)
)

func fwKind(l string) string {
	switch {
	case reFwErr.MatchString(l) && !strings.Contains(l, "-Werror"):
		return "err"
	case reFwWarn.MatchString(l):
		return "warn"
	}
	return "out"
}

var reAnsi = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// monKind: ESP-IDF log colours (E red, W yellow, I green), console answers start with "@".
func monKind(raw string) (string, string) {
	kind := "mon"
	if strings.Contains(raw, "\x1b[0;31m") || strings.Contains(raw, "\x1b[31m") {
		kind = "err"
	} else if strings.Contains(raw, "\x1b[0;33m") || strings.Contains(raw, "\x1b[33m") {
		kind = "warn"
	}
	txt := strings.TrimRight(reAnsi.ReplaceAllString(raw, ""), " \t")
	if strings.HasPrefix(strings.TrimSpace(txt), "@") {
		kind = "resp"
	} else if kind == "mon" && len(txt) > 2 && txt[1] == ' ' && txt[2] == '(' {
		switch txt[0] { // "E (123) tag: …" without colours
		case 'E':
			kind = "err"
		case 'W':
			kind = "warn"
		}
	}
	return kind, txt
}

func readSerial(ctx context.Context, j *Job, p serialPort) {
	buf := make([]byte, 4096)
	var line []byte
	flush := func() {
		if len(line) == 0 {
			return
		}
		raw := string(line)
		if !utf8.ValidString(raw) {
			raw = strings.ToValidUTF8(raw, "?")
		}
		line = line[:0]
		k, txt := monKind(raw)
		if strings.TrimSpace(txt) != "" {
			j.logf(k, "%s", txt)
		}
	}
	lastData := time.Now()
	for ctx.Err() == nil {
		n, err := p.Read(buf)
		if n > 0 {
			lastData = time.Now()
			for _, c := range buf[:n] {
				switch c {
				case '\n':
					flush()
				case '\r':
				default:
					line = append(line, c)
					if len(line) > 4000 {
						flush()
					}
				}
			}
		} else if len(line) > 0 && time.Since(lastData) > 300*time.Millisecond {
			flush() // prompt without newline
		}
		if err != nil {
			if ctx.Err() == nil {
				j.logf("err", T("Port se odpojil: %v"), err)
			}
			flush()
			return
		}
	}
	flush()
}

// Input sends a line to the monitor.
func (m *FwManager) Input(tabID, text string) error {
	s := m.get(tabID)
	if s == nil {
		return errors.New(T("Monitor neběží"))
	}
	s.mu.Lock()
	p := s.port
	s.mu.Unlock()
	if p == nil {
		return errors.New(T("Monitor neběží"))
	}
	s.j.logf("in", "› %s", text)
	_, err := p.Write([]byte(text + "\r\n"))
	return err
}

// Reset restarts the chip (RTS pulse – like esptool / idf.py monitor Ctrl+T Ctrl+R).
func (m *FwManager) Reset(tabID string) error {
	s := m.get(tabID)
	if s == nil {
		return errors.New(T("Monitor neběží"))
	}
	s.mu.Lock()
	p := s.port
	s.mu.Unlock()
	if p == nil {
		return errors.New(T("Monitor neběží"))
	}
	s.j.logf("info", "%s", T("Reset zařízení (RTS)"))
	return p.Reset()
}

func (m *FwManager) Stop(tabID string) {
	if s := m.get(tabID); s != nil {
		s.j.Cancel()
	}
}

func (m *FwManager) Snapshot(tabID string, since int) map[string]any {
	s := m.get(tabID)
	if s == nil {
		return map[string]any{"none": true}
	}
	snap := s.j.Snapshot(since)
	s.mu.Lock()
	snap["port"] = s.portName
	snap["portOpen"] = s.port != nil
	snap["action"] = s.action
	snap["variant"] = s.variant
	s.mu.Unlock()
	return snap
}

// AnyMonitor: a FW session is running (Leonzilla must not quit silently).
func (m *FwManager) AnyRunning() bool { return len(m.Running()) > 0 }
