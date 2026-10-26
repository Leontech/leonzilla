package main

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed ui
var uiFS embed.FS

type API struct {
	store    *Store
	runner   *Runner
	token    string
	host     string // expected Host header (127.0.0.1:port)
	lastPing time.Time
	pingMu   sync.Mutex

	brMu   sync.Mutex
	browse map[string]*browseSess

	fw *FwManager
}

type browseSess struct {
	mu   sync.Mutex
	rem  Remote
	sig  string
	used time.Time
}

func newAPI(s *Store, r *Runner, token string) *API {
	a := &API{store: s, runner: r, token: token, browse: map[string]*browseSess{}, lastPing: time.Now(), fw: newFwManager(s)}
	go a.janitor()
	return a
}

func (a *API) janitor() {
	for range time.Tick(20 * time.Second) {
		a.brMu.Lock()
		for k, b := range a.browse {
			if b.mu.TryLock() {
				if time.Since(b.used) > 90*time.Second {
					if b.rem != nil {
						b.rem.Close()
					}
					delete(a.browse, k)
				}
				b.mu.Unlock()
			}
		}
		a.brMu.Unlock()
	}
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(uiFS, "ui")
	static := http.FileServer(http.FS(sub))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		static.ServeHTTP(w, r)
	}))

	api := http.NewServeMux()
	api.HandleFunc("GET /api/state", a.hState)
	api.HandleFunc("POST /api/unlock", a.hUnlock)
	api.HandleFunc("POST /api/tabs", a.hCreateTab)
	api.HandleFunc("PUT /api/tabs/{id}", a.hUpdateTab)
	api.HandleFunc("DELETE /api/tabs/{id}", a.hDeleteTab)
	api.HandleFunc("POST /api/tabs/{id}/duplicate", a.hDuplicate)
	api.HandleFunc("POST /api/order", a.hOrder)
	api.HandleFunc("POST /api/layout", a.hLayout)
	api.HandleFunc("POST /api/groups", a.hGroups)
	api.HandleFunc("POST /api/builds", a.hBuilds)
	api.HandleFunc("POST /api/builds/inspect", a.hBuildInspect)
	api.HandleFunc("GET /api/radstudio", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]bool{"running": radStudioRunning()})
	})
	api.HandleFunc("POST /api/tabs/{id}/test", a.hTest)
	api.HandleFunc("POST /api/tabs/{id}/browse", a.hBrowse)
	api.HandleFunc("POST /api/tabs/{id}/mkdir", a.hMkdir)
	api.HandleFunc("POST /api/tabs/{id}/trustkey", a.hTrustKey)
	api.HandleFunc("POST /api/tabs/{id}/sync", a.hSync)
	api.HandleFunc("POST /api/tabs/{id}/cancel", a.hCancel)
	api.HandleFunc("GET /api/tabs/{id}/job", a.hJob)
	api.HandleFunc("POST /api/tabs/{id}/openlog", a.hOpenLog)
	api.HandleFunc("POST /api/settings", a.hSettings)
	api.HandleFunc("GET /api/ui", a.hUIGet)
	api.HandleFunc("POST /api/ui", a.hUISet)
	api.HandleFunc("POST /api/master", a.hMaster)
	api.HandleFunc("POST /api/pickdir", a.hPickDir)
	api.HandleFunc("POST /api/pickexclude", a.hPickExclude)
	api.HandleFunc("POST /api/pickfile", a.hPickFile)
	api.HandleFunc("POST /api/openfolder", a.hOpenFolder)
	api.HandleFunc("POST /api/openurl", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ URL string }
		_ = readJSON(r, &req)
		if !strings.HasPrefix(req.URL, "mailto:") && !strings.HasPrefix(req.URL, "https://") {
			writeErr(w, 400, errors.New(T("nepodporovaný odkaz")))
			return
		}
		if err := openWithShell(req.URL); err != nil {
			writeErr(w, 500, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	api.HandleFunc("POST /api/localcheck", a.hLocalCheck)
	api.HandleFunc("GET /api/git/info", a.hGitInfo)
	api.HandleFunc("GET /api/dropbox", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, dropboxStatus()) })
	api.HandleFunc("GET /api/serialports", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"ports": listSerialPorts()}) })
	api.HandleFunc("POST /api/fw/detect", a.hFwDetect)
	api.HandleFunc("POST /api/tabs/{id}/fw/run", a.hFwRun)
	api.HandleFunc("GET /api/tabs/{id}/fw", a.hFwState)
	api.HandleFunc("POST /api/tabs/{id}/fw/input", a.hFwInput)
	api.HandleFunc("POST /api/tabs/{id}/fw/reset", a.hFwReset)
	api.HandleFunc("POST /api/tabs/{id}/fw/stop", a.hFwStop)
	api.HandleFunc("POST /api/tabs/{id}/fw/terminal", a.hFwTerminal)
	api.HandleFunc("POST /api/git/status", a.hGitStatus)
	api.HandleFunc("POST /api/git/diff", a.hGitDiff)
	api.HandleFunc("POST /api/git/commit", a.hGitCommit)
	api.HandleFunc("POST /api/tabs/{id}/gitdone", a.hGitDone)
	api.HandleFunc("POST /api/ping", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]bool{"ok": true}) })

	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.host != "" && r.Host != a.host { // DNS rebinding protection
			http.Error(w, "bad host", http.StatusForbidden)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Token")), []byte(a.token)) != 1 {
			http.Error(w, "bad token", http.StatusForbidden)
			return
		}
		a.pingMu.Lock()
		a.lastPing = time.Now()
		a.pingMu.Unlock()
		a.store.mu.Lock()
		locked := a.store.locked
		a.store.mu.Unlock()
		if locked && r.URL.Path != "/api/state" && r.URL.Path != "/api/unlock" && r.URL.Path != "/api/ping" {
			writeErr(w, http.StatusLocked, errors.New(T("Aplikace je zamčená hlavním heslem")))
			return
		}
		api.ServeHTTP(w, r)
	}))
	return mux
}

func (a *API) sinceLastPing() time.Duration {
	a.pingMu.Lock()
	defer a.pingMu.Unlock()
	return time.Since(a.lastPing)
}

// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}

// public view of a tab (no password)
type tabView struct {
	*Tab
	HasPass bool `json:"hasPass"`
}

func viewTab(t *Tab) map[string]any {
	b, _ := json.Marshal(t)
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	if c, ok := m["conn"].(map[string]any); ok {
		delete(c, "pass")
		c["hasPass"] = t.Conn.pass != "" || t.Conn.PassStored != ""
	}
	return m
}

func (a *API) hState(w http.ResponseWriter, r *http.Request) {
	if !a.runner.AnyRunning() {
		a.store.reloadIfChanged()
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	st := map[string]any{
		"version": appVersion,
		"locked":  a.store.locked,
		"dataDir": a.store.dir,
		"ui":      a.store.loadUI(), // window state of this PC (uistate.go)
		"pc":      pcName(),
		"settings": map[string]any{
			"theme":          a.store.cfg.Settings.Theme,
			"workers":        a.store.cfg.Settings.Workers,
			"master":         a.store.cfg.Settings.MasterSalt != "",
			"groups":         a.store.cfg.Settings.Groups,
			"builds":         a.store.cfg.Settings.Builds,
			"compiler":       a.store.cfg.Settings.Compiler,
			"compilerFound":  resolveCompiler(a.store.cfg.Settings.Compiler),
			"stopDropbox":    a.store.cfg.Settings.StopDropbox,
			"ignoreOutput":   a.store.cfg.Settings.IgnoreOutput == nil || *a.store.cfg.Settings.IgnoreOutput,
			"versionInfoVar": a.store.cfg.Settings.VersionInfoVar,
			"langSetting":    a.store.cfg.Settings.Lang,
		},
		"lang":                  lang(),
		"defaultExcludes":       defaultExcludes,
		"defaultBackupExcludes": defaultBackupExcludes,
		"gitFound":              gitPath() != "",
		"autostart":             autostartOn(),
		"idfProfile":            findIdfProfile(),
		"fwRunning":             a.fw.Running(),
		"autostartSupported":    autostartKnown,
		"colors":                tabColors,
		"canPickDir":            canPickDir,
	}
	if a.store.locked {
		st["tabs"] = []any{}
		writeJSON(w, st)
		return
	}
	tabs := []any{}
	jobs := map[string]string{}
	for _, t := range a.store.cfg.Tabs {
		tabs = append(tabs, viewTab(t))
		if j := a.runner.Get(t.ID); j != nil {
			j.mu.Lock()
			jobs[t.ID] = j.Phase
			j.mu.Unlock()
		}
	}
	st["tabs"] = tabs
	st["jobs"] = jobs
	writeJSON(w, st)
}

func (a *API) hUnlock(w http.ResponseWriter, r *http.Request) {
	var req struct{ Password string }
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := a.store.Unlock(req.Password); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *API) hCreateTab(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name, Group string }
	_ = readJSON(r, &req)
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	group := ""
	for _, g := range a.store.cfg.Settings.Groups {
		if g.ID == req.Group {
			group = g.ID
		}
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = fmt.Sprintf(T("Projekt %d"), len(a.store.cfg.Tabs)+1)
	}
	t := &Tab{ID: newID(), Name: name, Group: group, Color: tabColors[len(a.store.cfg.Tabs)%len(tabColors)],
		Conn: Conn{Protocol: "ftp"}, SafeUpload: true,
		Pairs:    []*Pair{},
		Excludes: append([]string{}, defaultExcludes...), Checklist: []string{}}
	a.store.normalizeTab(t)
	a.store.cfg.Tabs = append(a.store.cfg.Tabs, t)
	if err := a.store.saveLocked(); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, viewTab(t))
}

type tabUpdate struct {
	Name           string            `json:"name"`
	Group          string            `json:"group"`
	Color          string            `json:"color"`
	SafeUpload     bool              `json:"safeUpload"`
	Excludes       []string          `json:"excludes"`
	Checklist      []string          `json:"checklist"`
	Builds         []string          `json:"builds"`
	Pairs          []*Pair           `json:"pairs"`
	Backups        []*BackupPair     `json:"backups"`
	BackupExcludes []string          `json:"backupExcludes"`
	BackupNoNote   *bool             `json:"backupNoNote"`
	BuildInfo      map[string]string `json:"buildInfo"`
	Fw             *FwConfig         `json:"fw"`
	BuildLight     *BuildLight       `json:"buildLight"`
	Git            []*GitRepo        `json:"git"`
	GitPull        *bool             `json:"gitPull"`
	Dropbox        *string           `json:"dropbox"`
	Conn           struct {
		Protocol    string  `json:"protocol"`
		Host        string  `json:"host"`
		Port        int     `json:"port"`
		User        string  `json:"user"`
		PassNew     *string `json:"passNew"`
		DisableEPSV bool    `json:"disableEPSV"`
		InsecureTLS bool    `json:"insecureTLS"`
		KeyFile     string  `json:"keyFile"`
	} `json:"conn"`
}

func (a *API) hUpdateTab(w http.ResponseWriter, r *http.Request) {
	var u tabUpdate
	if err := readJSON(r, &u); err != nil {
		writeErr(w, 400, err)
		return
	}
	id := r.PathValue("id")
	if j := a.runner.Get(id); j != nil && j.Phase != "done" {
		writeErr(w, 409, errors.New(T("Během synchronizace nejde kartu měnit")))
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	t := a.store.tab(id)
	if t == nil {
		writeErr(w, 404, errors.New(T("karta neexistuje")))
		return
	}
	if strings.TrimSpace(u.Name) != "" {
		t.Name = strings.TrimSpace(u.Name)
	}
	if u.Color != "" {
		t.Color = u.Color
	}
	t.SafeUpload = u.SafeUpload
	if u.Dropbox != nil {
		t.Dropbox = normDropboxMode(*u.Dropbox)
	}
	t.Group = ""
	for _, g := range a.store.cfg.Settings.Groups {
		if g.ID == u.Group {
			t.Group = u.Group
		}
	}
	if u.Excludes != nil {
		t.Excludes = u.Excludes
	}
	if u.Backups != nil {
		for _, b := range u.Backups {
			if b.ID == "" {
				b.ID = newID()
			}
			b.Source = strings.TrimSpace(b.Source)
			b.Target = strings.TrimSpace(b.Target)
			b.Prefix = strings.TrimSpace(b.Prefix)
			if b.Excludes == nil {
				b.Excludes = []string{}
			}
		}
		t.Backups = u.Backups
	}
	if u.BackupExcludes != nil {
		t.BackupExcludes = u.BackupExcludes
	}
	if u.BackupNoNote != nil {
		t.BackupNoNote = *u.BackupNoNote
	}
	if u.Fw != nil {
		u.Fw.normalize()
		if u.Fw.Project == "" && len(u.Fw.Variants) == 0 && !u.Fw.Monitor {
			t.Fw = nil // section emptied
		} else {
			t.Fw = u.Fw
		}
	}
	if u.BuildLight != nil {
		u.BuildLight.normalize()
		if u.BuildLight.Script == "" && u.BuildLight.Args == "" && u.BuildLight.Label == "" {
			t.BuildLight = nil // section emptied
		} else {
			t.BuildLight = u.BuildLight
		}
	}
	if u.BuildInfo != nil {
		t.BuildInfo = map[string]string{}
		for k, v := range u.BuildInfo {
			t.BuildInfo[k] = cleanVersionInfo(v)
		}
	}
	if u.Git != nil {
		for _, g := range u.Git {
			if g.ID == "" {
				g.ID = newID()
			}
			g.Path = strings.TrimSpace(g.Path)
		}
		t.Git = u.Git
	}
	if u.GitPull != nil {
		t.GitPull = *u.GitPull
	}
	if u.Builds != nil {
		valid := map[string]bool{}
		for _, b := range a.store.cfg.Settings.Builds {
			valid[b.ID] = true
		}
		t.Builds = []string{}
		for _, id := range u.Builds {
			if valid[id] {
				t.Builds = append(t.Builds, id)
			}
		}
	}
	if u.Checklist != nil {
		t.Checklist = []string{}
		for _, c := range u.Checklist {
			if c = strings.TrimSpace(c); c != "" {
				t.Checklist = append(t.Checklist, c)
			}
		}
	}
	if u.Pairs != nil {
		for _, p := range u.Pairs {
			if p.ID == "" {
				p.ID = newID()
			}
			p.Local = strings.TrimSpace(p.Local)
			if p.Excludes == nil {
				p.Excludes = []string{}
			}
			p.Remote = strings.TrimSpace(p.Remote)
		}
		t.Pairs = u.Pairs
	}
	c := &t.Conn
	if c.Protocol != u.Conn.Protocol || !strings.EqualFold(c.Host, strings.TrimSpace(u.Conn.Host)) || c.Port != u.Conn.Port {
		c.HostKey = "" // different server – forget trusted SFTP key
	}
	c.Protocol = u.Conn.Protocol
	c.Host = strings.TrimSpace(u.Conn.Host)
	c.Port = u.Conn.Port
	c.User = strings.TrimSpace(u.Conn.User)
	c.DisableEPSV = u.Conn.DisableEPSV
	c.InsecureTLS = u.Conn.InsecureTLS
	c.KeyFile = strings.TrimSpace(u.Conn.KeyFile)
	if u.Conn.PassNew != nil {
		c.pass = *u.Conn.PassNew
	}
	if err := a.store.saveLocked(); err != nil {
		writeErr(w, 500, err)
		return
	}
	a.dropBrowse(id)
	writeJSON(w, viewTab(t))
}

func (a *API) hDeleteTab(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if j := a.runner.Get(id); j != nil && j.Phase != "done" {
		writeErr(w, 409, errors.New(T("Nejdřív zastav synchronizaci")))
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	out := a.store.cfg.Tabs[:0]
	for _, t := range a.store.cfg.Tabs {
		if t.ID != id {
			out = append(out, t)
		}
	}
	a.store.cfg.Tabs = out
	_ = os.Remove(filepath.Join(a.store.dir, "state", id+".json"))
	if err := a.store.saveLocked(); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *API) hDuplicate(w http.ResponseWriter, r *http.Request) {
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	src := a.store.tab(r.PathValue("id"))
	if src == nil {
		writeErr(w, 404, errors.New(T("karta neexistuje")))
		return
	}
	b, _ := json.Marshal(src)
	t := &Tab{}
	_ = json.Unmarshal(b, t)
	t.ID = newID()
	t.Name = src.Name + T(" (kopie)")
	t.LastSync, t.LastBuild, t.LastBackup, t.LastGit = nil, nil, nil, nil
	t.Conn.pass = src.Conn.pass
	for _, p := range t.Pairs {
		p.ID = newID()
	}
	for _, b := range t.Backups {
		b.ID = newID()
	}
	for _, g := range t.Git {
		g.ID = newID()
	}
	// the copy goes right after the original (same group)
	at := len(a.store.cfg.Tabs)
	for i, x := range a.store.cfg.Tabs {
		if x == src {
			at = i + 1
		}
	}
	a.store.cfg.Tabs = append(a.store.cfg.Tabs[:at], append([]*Tab{t}, a.store.cfg.Tabs[at:]...)...)
	if err := a.store.saveLocked(); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, viewTab(t))
}

func (a *API) hOrder(w http.ResponseWriter, r *http.Request) {
	var req struct{ IDs []string }
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	pos := map[string]int{}
	for i, id := range req.IDs {
		pos[id] = i
	}
	sort.SliceStable(a.store.cfg.Tabs, func(i, j int) bool {
		pi, ok1 := pos[a.store.cfg.Tabs[i].ID]
		pj, ok2 := pos[a.store.cfg.Tabs[j].ID]
		if !ok1 {
			pi = 1 << 30
		}
		if !ok2 {
			pj = 1 << 30
		}
		return pi < pj
	})
	if err := a.store.saveLocked(); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *API) hTest(w http.ResponseWriter, r *http.Request) {
	tab, err := a.store.TabCopy(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	t0 := time.Now()
	rem, err := a.runner.connect(ctx, tab)
	if err != nil {
		var hk *ErrHostKeyChanged
		writeJSON(w, map[string]any{"ok": false, "message": err.Error(), "hostKeyChanged": errors.As(err, &hk)})
		return
	}
	defer rem.Close()
	msg := fmt.Sprintf(T("Připojeno za %.1f s · %s · domovská složka %s"), time.Since(t0).Seconds(), rem.Info(), rem.Home())
	if tab.Conn.Protocol == "sftp" && tab.Conn.HostKey != "" {
		msg += T(" · klíč serveru ") + tab.Conn.HostKey
	}
	// check remote folders of pairs
	missing := []string{}
	for _, p := range tab.Pairs {
		if !p.Enabled || p.Remote == "" {
			continue
		}
		rp := resolveRemote(rem.Home(), p.Remote)
		if !rem.IsDir(rp) {
			missing = append(missing, rp)
		}
	}
	res := map[string]any{"ok": true, "message": msg}
	if len(missing) > 0 {
		res["warning"] = T("Na serveru zatím neexistuje: ") + strings.Join(missing, ", ") + T(" (při synchronizaci se vytvoří)")
	}
	writeJSON(w, res)
}

func (a *API) dropBrowse(tabID string) {
	a.brMu.Lock()
	b := a.browse[tabID]
	delete(a.browse, tabID)
	a.brMu.Unlock()
	if b != nil {
		b.mu.Lock()
		if b.rem != nil {
			b.rem.Close()
		}
		b.mu.Unlock()
	}
}

// withBrowse runs fn with a cached connection for the remote folder browser.
func (a *API) withBrowse(ctx context.Context, tabID string, fn func(Remote) error) error {
	tab, err := a.store.TabCopy(tabID)
	if err != nil {
		return err
	}
	sig := fmt.Sprintf("%s|%s|%d|%s|%s|%v", tab.Conn.Protocol, tab.Conn.Host, tab.Conn.Port, tab.Conn.User, tab.Conn.pass, tab.Conn.InsecureTLS)
	a.brMu.Lock()
	b := a.browse[tabID]
	if b == nil {
		b = &browseSess{}
		a.browse[tabID] = b
	}
	a.brMu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.used = time.Now()
	if b.rem != nil && b.sig != sig {
		b.rem.Close()
		b.rem = nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		if b.rem == nil {
			rem, err := a.runner.connect(ctx, tab)
			if err != nil {
				return err
			}
			b.rem, b.sig = rem, sig
		}
		err = fn(b.rem)
		if err == nil {
			return nil
		}
		// stale connection? reconnect once
		b.rem.Close()
		b.rem = nil
	}
	return err
}

func (a *API) hBrowse(w http.ResponseWriter, r *http.Request) {
	var req struct{ Path string }
	_ = readJSON(r, &req)
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	var out map[string]any
	err := a.withBrowse(ctx, r.PathValue("id"), func(rem Remote) error {
		p := resolveRemote(rem.Home(), req.Path)
		if !rem.IsDir(p) {
			p = rem.Home()
		}
		es, err := rem.List(p)
		if err != nil {
			return err
		}
		dirs := []string{}
		files := 0
		for _, e := range es {
			if e.IsDir || e.Size == -1 {
				dirs = append(dirs, e.Name)
			} else {
				files++
			}
		}
		sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i]) < strings.ToLower(dirs[j]) })
		out = map[string]any{"path": p, "home": rem.Home(), "dirs": dirs, "files": files, "parent": path.Dir(p)}
		return nil
	})
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, out)
}

func (a *API) hMkdir(w http.ResponseWriter, r *http.Request) {
	var req struct{ Path, Name string }
	_ = readJSON(r, &req)
	name := strings.TrimSpace(req.Name)
	if name == "" || strings.ContainsAny(name, "/\\") {
		writeErr(w, 400, errors.New(T("Neplatný název složky")))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	p := path.Join(req.Path, name)
	err := a.withBrowse(ctx, r.PathValue("id"), func(rem Remote) error { return rem.Mkdir(p) })
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, map[string]string{"path": p})
}

func (a *API) hTrustKey(w http.ResponseWriter, r *http.Request) {
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	if t := a.store.tab(r.PathValue("id")); t != nil {
		t.Conn.HostKey = ""
		_ = a.store.saveLocked()
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *API) hSync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Preview, Force, Build, Backup bool
		Sync                          *bool
		StopDropbox                   bool // the user chose "stop Dropbox for this build" in the Dropbox check
		Notes                         []string
		Only                          []int
		Names                         []string
		BuildInfo                     map[string]string
	}
	_ = readJSON(r, &req)
	id := r.PathValue("id")
	a.dropBrowse(id)
	o := StartOpts{Preview: req.Preview, Force: req.Force, Build: req.Build, Backup: req.Backup, Notes: req.Notes, Only: req.Only, Names: req.Names, BuildInfo: req.BuildInfo, Sync: req.Sync == nil || *req.Sync, StopDropbox: req.StopDropbox}
	if _, err := a.runner.Start(id, o); err != nil {
		writeErr(w, 409, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *API) hCancel(w http.ResponseWriter, r *http.Request) {
	if j := a.runner.Get(r.PathValue("id")); j != nil {
		j.Cancel()
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *API) hJob(w http.ResponseWriter, r *http.Request) {
	j := a.runner.Get(r.PathValue("id"))
	if j == nil {
		writeJSON(w, map[string]any{"none": true})
		return
	}
	since, _ := strconv.Atoi(r.URL.Query().Get("log"))
	writeJSON(w, j.Snapshot(since))
}

func (a *API) hOpenLog(w http.ResponseWriter, r *http.Request) {
	p := filepath.Join(a.store.dir, "logs", r.PathValue("id")+".log")
	if _, err := os.Stat(p); err != nil {
		writeErr(w, 404, errors.New(T("Log zatím neexistuje – vznikne při první synchronizaci")))
		return
	}
	if err := openWithShell(p); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

var reEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (a *API) hSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Theme          string
		Workers        int
		Compiler       *string
		StopDropbox    *bool
		IgnoreOutput   *bool
		VersionInfoVar *string
		Lang           *string // "cs" | "en" | "" (by Windows)
		Autostart      *bool   // per PC (registry), not in config.json
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.Autostart != nil {
		if err := setAutostart(*req.Autostart); err != nil {
			writeErr(w, 500, err)
			return
		}
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	if req.Theme == "auto" || req.Theme == "light" || req.Theme == "dark" {
		a.store.cfg.Settings.Theme = req.Theme
	}
	if req.Workers >= 1 && req.Workers <= 8 {
		a.store.cfg.Settings.Workers = req.Workers
	}
	if req.StopDropbox != nil {
		a.store.cfg.Settings.StopDropbox = *req.StopDropbox
	}
	if req.IgnoreOutput != nil {
		v := *req.IgnoreOutput
		a.store.cfg.Settings.IgnoreOutput = &v
	}
	if req.Compiler != nil {
		a.store.cfg.Settings.Compiler = strings.TrimSpace(*req.Compiler)
	}
	if req.VersionInfoVar != nil {
		v := strings.TrimSpace(*req.VersionInfoVar)
		if v != "" && !reEnvName.MatchString(v) {
			writeErr(w, 400, errors.New(T("Název proměnné smí obsahovat jen písmena A–Z, číslice a _")))
			return
		}
		a.store.cfg.Settings.VersionInfoVar = v
	}
	if req.Lang != nil && (*req.Lang == "" || *req.Lang == "cs" || *req.Lang == "en") {
		a.store.cfg.Settings.Lang = *req.Lang
		setLang(*req.Lang)
	}
	if err := a.store.saveLocked(); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *API) hMaster(w http.ResponseWriter, r *http.Request) {
	var req struct{ Old, New string }
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := a.store.SetMaster(req.Old, req.New); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *API) hPickDir(w http.ResponseWriter, r *http.Request) {
	var req struct{ Start, Title string }
	_ = readJSON(r, &req)
	p, err := pickFolder(req.Title, req.Start)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, map[string]any{"path": p, "cancelled": p == ""})
}

func (a *API) hOpenFolder(w http.ResponseWriter, r *http.Request) {
	var req struct{ Path string }
	_ = readJSON(r, &req)
	if st, err := os.Stat(req.Path); err != nil || !st.IsDir() {
		writeErr(w, 404, errors.New(T("Složka neexistuje")))
		return
	}
	if err := openWithShell(req.Path); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// hLocalCheck reports which local folders exist (shown in the UI).
func (a *API) hLocalCheck(w http.ResponseWriter, r *http.Request) {
	var req struct{ Paths []string }
	_ = readJSON(r, &req)
	out := map[string]bool{}
	for _, p := range req.Paths {
		st, err := os.Stat(strings.TrimSpace(p))
		out[p] = err == nil && st.IsDir()
	}
	writeJSON(w, out)
}

// hPickExclude opens a folder/file dialog inside a pair's local folder and returns
// the chosen item as a path relative to that folder ("img/originals/" for a folder).
func (a *API) hPickExclude(w http.ResponseWriter, r *http.Request) {
	var req struct{ Local, Kind string }
	_ = readJSON(r, &req)
	root := filepath.Clean(strings.TrimSpace(req.Local))
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		writeErr(w, 400, errors.New(T("Nejdřív vyber existující složku v PC")))
		return
	}
	var p string
	var err error
	if req.Kind == "file" {
		p, err = pickFile(T("Vyber soubor, který se nemá nahrávat"), root, "", "")
	} else {
		p, err = pickFolder(T("Vyber složku, která se nemá nahrávat"), root)
	}
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if p == "" {
		writeJSON(w, map[string]any{"cancelled": true})
		return
	}
	rel, err := filepath.Rel(root, filepath.Clean(p))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		writeErr(w, 400, fmt.Errorf(T("Vybraná položka není uvnitř %s"), root))
		return
	}
	rel = filepath.ToSlash(rel)
	if req.Kind != "file" {
		rel += "/"
	}
	writeJSON(w, map[string]any{"rel": rel})
}

func (a *API) hPickFile(w http.ResponseWriter, r *http.Request) {
	var req struct{ Start, Title, FilterName, FilterSpec string }
	_ = readJSON(r, &req)
	p, err := pickFile(req.Title, req.Start, req.FilterName, req.FilterSpec)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, map[string]any{"path": p, "cancelled": p == ""})
}

// hLayout sets the order of all cards and the group of each card at once
// (used when tiles are dragged on the overview).
func (a *API) hLayout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []struct {
			ID    string `json:"id"`
			Group string `json:"group"`
		} `json:"items"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	valid := map[string]bool{"": true}
	for _, g := range a.store.cfg.Settings.Groups {
		valid[g.ID] = true
	}
	pos := map[string]int{}
	for i, it := range req.Items {
		pos[it.ID] = i
		if t := a.store.tab(it.ID); t != nil && valid[it.Group] {
			t.Group = it.Group
		}
	}
	sort.SliceStable(a.store.cfg.Tabs, func(i, j int) bool {
		pi, ok1 := pos[a.store.cfg.Tabs[i].ID]
		pj, ok2 := pos[a.store.cfg.Tabs[j].ID]
		if !ok1 {
			pi = 1 << 30
		}
		if !ok2 {
			pj = 1 << 30
		}
		return pi < pj
	})
	if err := a.store.saveLocked(); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// hGroups replaces the list of groups (add / rename / delete / reorder).
// Cards in a deleted group move to "no group".
func (a *API) hGroups(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Groups []*Group `json:"groups"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	out := []*Group{}
	seen := map[string]bool{}
	for _, g := range req.Groups {
		if g == nil {
			continue
		}
		g.Name = strings.TrimSpace(g.Name)
		if g.Name == "" {
			continue
		}
		if g.ID == "" || seen[g.ID] {
			g.ID = newID()
		}
		seen[g.ID] = true
		out = append(out, g)
	}
	a.store.cfg.Settings.Groups = out
	for _, t := range a.store.cfg.Tabs {
		if t.Group != "" && !seen[t.Group] {
			t.Group = ""
		}
	}
	if err := a.store.saveLocked(); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]any{"groups": out})
}

// hBuilds replaces the list of build definitions; cards lose builds that were deleted.
func (a *API) hBuilds(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Builds []*Build `json:"builds"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	out := []*Build{}
	ids := map[string]bool{}
	for _, b := range req.Builds {
		if b == nil {
			continue
		}
		b.normalize()
		if b.Name == "" {
			b.Name = "Build"
		}
		if ids[b.ID] {
			b.ID = newID()
		}
		ids[b.ID] = true
		out = append(out, b)
	}
	a.store.cfg.Settings.Builds = out
	for _, t := range a.store.cfg.Tabs {
		keep := []string{}
		for _, id := range t.Builds {
			if ids[id] {
				keep = append(keep, id)
			}
		}
		t.Builds = keep
	}
	if err := a.store.saveLocked(); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]any{"builds": out})
}

// hBuildInspect reads a .dproj (configurations, defines, post-build) and its Predefines.inc (switches).
func (a *API) hBuildInspect(w http.ResponseWriter, r *http.Request) {
	var req struct{ Project, IncFile, Config, Platform string }
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	b := &Build{Project: strings.TrimSpace(req.Project), IncFile: req.IncFile, Config: req.Config, Platform: req.Platform}
	b.normalize()
	if b.Project == "" {
		writeErr(w, 400, errors.New(T("Vyber projekt (.dproj)")))
		return
	}
	out := map[string]any{}
	info, err := parseDproj(b.Project, "", b.Platform)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	out["configs"] = info.Configs
	if b.Config != "" {
		if ci, err := parseDproj(b.Project, b.Config, b.Platform); err == nil {
			out["configDefines"] = ci.Defines
			out["postBuild"] = expandMacros(ci.PostBuild, b)
		} else {
			out["configError"] = err.Error()
		}
	}
	out["inc"] = parseInc(b.incPath())
	writeJSON(w, out)
}

// autostartOn: Leonzilla starts with Windows on this PC and the entry points to this exe.
func autostartOn() bool {
	on, current := autostartState()
	return on && current
}
