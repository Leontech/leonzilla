package main

// Export / import of the whole configuration (App settings → General): cards, groups, builds and general settings in
// one JSON file – for backups or moving to another installation. Passwords of the servers are included only when the
// user asks for it, and then encrypted with an export password (scrypt + AES-GCM). Import only ADDS what the user picks:
// cards always get a new ID, groups and builds that already exist (same ID or name) are reused, nothing is overwritten,
// and config.json is copied to Leonzilla_data\backup first.

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/scrypt"
)

const exportKind = "leonzilla-export"

type exportSettings struct {
	Theme          string `json:"theme,omitempty"`
	Workers        int    `json:"workers,omitempty"`
	Lang           string `json:"lang,omitempty"`
	Compiler       string `json:"compiler,omitempty"`
	VersionInfoVar string `json:"versionInfoVar,omitempty"`
	IgnoreOutput   *bool  `json:"ignoreOutput,omitempty"`
}

type exportFile struct {
	Kind      string         `json:"kind"` // always "leonzilla-export"
	Format    int            `json:"format"`
	App       string         `json:"app"` // Leonzilla version that wrote it
	Created   time.Time      `json:"created"`
	PC        string         `json:"pc"`
	PassSalt  string         `json:"passSalt,omitempty"`  // set = passwords included (Conn.pass = "exp:…")
	PassCheck string         `json:"passCheck,omitempty"` // known text encrypted with the export key
	Settings  exportSettings `json:"settings"`
	Groups    []*Group       `json:"groups"`
	Builds    []*Build       `json:"builds"`
	Tabs      []*Tab         `json:"tabs"`
}

const exportCheckPlain = "leonzilla-export-ok"

func exportKey(password, saltB64 string) ([]byte, error) {
	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		return nil, err
	}
	return scrypt.Key([]byte(password), salt, 1<<15, 8, 1, 32)
}

func exportDir(dataDir string) string { return filepath.Join(dataDir, "export") }

// hExport writes the export file into the chosen folder (default Leonzilla_data\export).
func (a *API) hExport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Folder    string `json:"folder"`
		Passwords bool   `json:"passwords"`
		Password  string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.Passwords && len(req.Password) < 4 {
		writeErr(w, 400, errors.New(T("Heslo k exportu musí mít aspoň 4 znaky")))
		return
	}
	a.store.mu.Lock()
	cfg := a.store.cfg
	f := exportFile{Kind: exportKind, Format: 1, App: appVersion, Created: time.Now(), PC: pcName(),
		Settings: exportSettings{Theme: cfg.Settings.Theme, Workers: cfg.Settings.Workers, Lang: cfg.Settings.Lang,
			Compiler: cfg.Settings.Compiler, VersionInfoVar: cfg.Settings.VersionInfoVar, IgnoreOutput: cfg.Settings.IgnoreOutput}}
	var key []byte
	if req.Passwords {
		salt := make([]byte, 16)
		_, _ = rand.Read(salt)
		f.PassSalt = base64.StdEncoding.EncodeToString(salt)
		k, err := exportKey(req.Password, f.PassSalt)
		if err != nil {
			a.store.mu.Unlock()
			writeErr(w, 500, err)
			return
		}
		key = k
		f.PassCheck = gcmSeal(key, exportCheckPlain)
	}
	// deep copies through JSON (the store's objects stay untouched)
	b, _ := json.Marshal(cfg.Settings.Groups)
	_ = json.Unmarshal(b, &f.Groups)
	b, _ = json.Marshal(cfg.Settings.Builds)
	_ = json.Unmarshal(b, &f.Builds)
	for _, t := range cfg.Tabs {
		var c *Tab // deep copy through JSON; the card in the store is not touched
		b, _ := json.Marshal(t)
		if json.Unmarshal(b, &c) != nil || c == nil {
			continue
		}
		c.Conn.pass = t.Conn.pass
		c.Conn.PassStored = ""
		if key != nil && c.Conn.pass != "" {
			c.Conn.PassStored = "exp:" + gcmSeal(key, c.Conn.pass)
		}
		c.LastSync, c.LastBuild, c.LastBackup, c.LastGit = nil, nil, nil, nil
		f.Tabs = append(f.Tabs, c)
	}
	a.store.mu.Unlock()

	dir := strings.TrimSpace(req.Folder)
	if dir == "" {
		dir = exportDir(a.store.dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeErr(w, 400, err)
		return
	}
	out, _ := json.MarshalIndent(f, "", "  ")
	base := fmt.Sprintf("Leonzilla_export_%s_%s", pcName(), time.Now().Format("2006-01-02_15-04-05"))
	if key != nil {
		base += "_s-hesly"
	}
	path := filepath.Join(dir, base+".json")
	for i := 2; ; i++ { // never overwrite an earlier export
		if _, err := os.Stat(path); os.IsNotExist(err) {
			break
		}
		path = filepath.Join(dir, fmt.Sprintf("%s_%d.json", base, i))
	}
	if err := writeFileAtomic(path, out); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]any{"path": path, "folder": dir, "tabs": len(f.Tabs), "groups": len(f.Groups), "builds": len(f.Builds), "passwords": key != nil})
}

func readExport(path string) (*exportFile, error) {
	b, err := os.ReadFile(strings.Trim(strings.TrimSpace(path), `"`))
	if err != nil {
		return nil, err
	}
	var f exportFile
	if err := json.Unmarshal(b, &f); err != nil || f.Kind != exportKind {
		return nil, errors.New(T("Soubor není export Leonzilly"))
	}
	return &f, nil
}

// hImportPreview shows what the file contains and what already exists here.
func (a *API) hImportPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	_ = readJSON(r, &req)
	f, err := readExport(req.Path)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	type item struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Exists bool   `json:"exists"`
		Note   string `json:"note,omitempty"`
	}
	var groups, builds, tabs []item
	for _, g := range f.Groups {
		groups = append(groups, item{ID: g.ID, Name: g.Name, Exists: a.store.findGroup(g) != ""})
	}
	for _, b := range f.Builds {
		builds = append(builds, item{ID: b.ID, Name: b.Name, Exists: a.store.findBuild(b) != ""})
	}
	gname := map[string]string{}
	for _, g := range f.Groups {
		gname[g.ID] = g.Name
	}
	for _, t := range f.Tabs {
		exists := false
		for _, x := range a.store.cfg.Tabs {
			exists = exists || strings.EqualFold(x.Name, t.Name)
		}
		var parts []string
		if g := gname[t.Group]; g != "" {
			parts = append(parts, g)
		}
		if t.Conn.Host != "" {
			parts = append(parts, t.Conn.Host)
		}
		tabs = append(tabs, item{ID: t.ID, Name: t.Name, Exists: exists, Note: strings.Join(parts, " · ")})
	}
	writeJSON(w, map[string]any{"app": f.App, "created": f.Created, "pc": f.PC, "passwords": f.PassSalt != "",
		"groups": groups, "builds": builds, "tabs": tabs, "settings": f.Settings})
}

// findGroup returns the ID of an existing group that is the same (same ID or name). Caller holds s.mu.
func (s *Store) findGroup(g *Group) string {
	for _, x := range s.cfg.Settings.Groups {
		if x.ID == g.ID || strings.EqualFold(strings.TrimSpace(x.Name), strings.TrimSpace(g.Name)) {
			return x.ID
		}
	}
	return ""
}

// findBuild returns the ID of an existing build that is the same (same ID or name). Caller holds s.mu.
func (s *Store) findBuild(b *Build) string {
	for _, x := range s.cfg.Settings.Builds {
		if x.ID == b.ID || strings.EqualFold(strings.TrimSpace(x.Name), strings.TrimSpace(b.Name)) {
			return x.ID
		}
	}
	return ""
}

// backupConfig copies config.json to Leonzilla_data\backup\config_<what>_<time>.json. Caller holds s.mu.
func (s *Store) backupConfig(what string) (string, error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	dir := filepath.Join(s.dir, "backup")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, fmt.Sprintf("config_%s_%s.json", what, time.Now().Format("2006-01-02_15-04-05")))
	return p, writeFileAtomic(p, b)
}

// hImport adds the chosen cards / groups / builds (and optionally the general settings) from an export file.
func (a *API) hImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path     string   `json:"path"`
		Password string   `json:"password"` // export password; "" = import without server passwords
		Tabs     []string `json:"tabs"`
		Groups   []string `json:"groups"`
		Builds   []string `json:"builds"`
		Settings bool     `json:"settings"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if a.runner.AnyRunning() {
		writeErr(w, 409, errors.New(T("Během synchronizace nebo buildu nejde importovat")))
		return
	}
	f, err := readExport(req.Path)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	var key []byte
	if f.PassSalt != "" && req.Password != "" {
		k, err := exportKey(req.Password, f.PassSalt)
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		if p, err := gcmOpen(k, f.PassCheck); err != nil || p != exportCheckPlain {
			writeErr(w, 400, errors.New(T("Špatné heslo k exportu")))
			return
		}
		key = k
	}
	want := func(list []string) map[string]bool {
		m := map[string]bool{}
		for _, x := range list {
			m[x] = true
		}
		return m
	}
	wantT, wantG, wantB := want(req.Tabs), want(req.Groups), want(req.Builds)

	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	if a.store.locked {
		writeErr(w, 409, errors.New(T("aplikace je zamčená")))
		return
	}
	bak, err := a.store.backupConfig("pred-importem")
	if err != nil {
		writeErr(w, 500, fmt.Errorf(T("Záloha config.json se nepovedla: %v"), err))
		return
	}
	st := &a.store.cfg.Settings
	added := map[string]int{}
	// groups: existing (same ID / name) are reused, chosen new ones are added
	gmap := map[string]string{}
	for _, g := range f.Groups {
		if id := a.store.findGroup(g); id != "" {
			gmap[g.ID] = id
			continue
		}
		if wantG[g.ID] {
			ng := &Group{ID: g.ID, Name: strings.TrimSpace(g.Name)}
			if ng.ID == "" {
				ng.ID = newID()
			}
			st.Groups = append(st.Groups, ng)
			gmap[g.ID] = ng.ID
			added["groups"]++
		}
	}
	// builds: the same way
	bmap := map[string]string{}
	for _, b := range f.Builds {
		if id := a.store.findBuild(b); id != "" {
			bmap[b.ID] = id
			continue
		}
		if wantB[b.ID] {
			nb := *b
			if nb.ID == "" {
				nb.ID = newID()
			}
			st.Builds = append(st.Builds, &nb)
			bmap[b.ID] = nb.ID
			added["builds"]++
		}
	}
	// cards: always added as new cards (new ID, own sync state), never overwrite an existing one
	noPass := 0
	for _, t := range f.Tabs {
		if !wantT[t.ID] {
			continue
		}
		c := *t
		c.ID = newID()
		c.LastSync, c.LastBuild, c.LastBackup, c.LastGit = nil, nil, nil, nil
		for _, x := range a.store.cfg.Tabs {
			if strings.EqualFold(x.Name, c.Name) {
				c.Name += T(" (import)")
				break
			}
		}
		c.Group = gmap[t.Group]
		var bl []string
		bi := map[string]string{}
		for _, id := range t.Builds {
			if nid, ok := bmap[id]; ok {
				bl = append(bl, nid)
				if v, ok := t.BuildInfo[id]; ok {
					bi[nid] = v
				}
			}
		}
		c.Builds, c.BuildInfo = bl, nil
		if len(bi) > 0 {
			c.BuildInfo = bi
		}
		c.Conn.pass = ""
		if strings.HasPrefix(t.Conn.PassStored, "exp:") {
			if key != nil {
				if p, err := gcmOpen(key, t.Conn.PassStored[4:]); err == nil {
					c.Conn.pass = p
				}
			} else {
				noPass++
			}
		}
		c.Conn.PassStored = ""
		for _, p := range c.Pairs {
			p.ID = newID()
		}
		a.store.normalizeTab(&c)
		a.store.cfg.Tabs = append(a.store.cfg.Tabs, &c)
		added["tabs"]++
	}
	if req.Settings {
		s := f.Settings
		if s.Theme != "" {
			st.Theme = s.Theme
		}
		if s.Workers > 0 {
			st.Workers = s.Workers
		}
		st.Lang, st.Compiler, st.VersionInfoVar, st.IgnoreOutput = s.Lang, s.Compiler, s.VersionInfoVar, s.IgnoreOutput
	}
	if err := a.store.saveLocked(); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "backup": bak, "tabs": added["tabs"], "groups": added["groups"], "builds": added["builds"], "noPass": noPass})
}

// hExportList lists the export files in Leonzilla_data\export (newest first) – for the import dialog.
func (a *API) hExportList(w http.ResponseWriter, r *http.Request) {
	dir := exportDir(a.store.dir)
	ents, _ := os.ReadDir(dir)
	type fileInfo struct {
		Path string    `json:"path"`
		Name string    `json:"name"`
		At   time.Time `json:"at"`
	}
	var out []fileInfo
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		if fi, err := e.Info(); err == nil {
			out = append(out, fileInfo{Path: filepath.Join(dir, e.Name()), Name: e.Name(), At: fi.ModTime()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	writeJSON(w, map[string]any{"folder": dir, "files": out})
}
