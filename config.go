package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/scrypt"
)

// ---------------------------------------------------------------------------
// Data model (stored in Leonzilla_data/config.json next to the exe)
// ---------------------------------------------------------------------------

type Config struct {
	Version  int      `json:"version"`
	Settings Settings `json:"settings"`
	Tabs     []*Tab   `json:"tabs"`
}

type Settings struct {
	Theme    string   `json:"theme"`    // auto | light | dark
	Workers  int      `json:"workers"`  // parallel uploads 1..8
	Groups   []*Group `json:"groups"`   // groups of cards on the overview, in display order
	Builds   []*Build `json:"builds"`   // build definitions (RAD Studio / TMS WEB Core), picked by cards
	Compiler string   `json:"compiler"` // TMSWebCompiler.exe ("" = find automatically)
	// VersionInfoVar: extra environment variable name for the build "version info"
	// (LEONZILLA_VERSION_INFO is always set), e.g. the name a project's post-build script reads.
	VersionInfoVar string `json:"versionInfoVar,omitempty"`
	// Lang: UI language "cs" | "en"; "" = by the Windows display language
	Lang string `json:"lang,omitempty"`
	// IgnoreOutput: exclude the build output folder (TMSWeb) from Dropbox sync. nil = default (on).
	IgnoreOutput *bool `json:"ignoreOutput,omitempty"`
	// StopDropbox: also stop Dropbox during builds (only if it runs) and start it again afterwards. Default off.
	StopDropbox bool `json:"stopDropbox,omitempty"`
	// Master password (optional). When set, server passwords are encrypted with it.
	MasterSalt  string `json:"masterSalt,omitempty"`
	MasterCheck string `json:"masterCheck,omitempty"`
}

type Group struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Tab struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Group     string   `json:"group,omitempty"` // Group.ID, "" = no group
	Color     string   `json:"color"`
	Conn      Conn     `json:"conn"`
	Pairs     []*Pair  `json:"pairs"`
	Excludes  []string `json:"excludes"`
	Checklist []string `json:"checklist"` // items to confirm before every sync (empty = plain confirm)
	Builds    []string `json:"builds"`    // Build.IDs run (in this order) by "Build + Synchronizovat"
	// "Info k verzi" last used on this card per build ID (overrides the build setting; missing = build setting)
	BuildInfo map[string]string `json:"buildInfo,omitempty"`
	// "Build light": own Python script run like a build before the upload – nil = not used
	BuildLight *BuildLight `json:"buildLight,omitempty"`
	// FW build (ESP-IDF) – nil = not used
	Fw        *FwConfig    `json:"fw,omitempty"`
	LastBuild *SyncSummary `json:"lastBuild,omitempty"`
	// backups: zip folders into target folders (button "Zálohovat")
	Backups        []*BackupPair `json:"backups"`
	BackupExcludes []string      `json:"backupExcludes"`
	LastBackup     *SyncSummary  `json:"lastBackup,omitempty"`
	BackupNoNote   bool          `json:"backupNoNote"` // don't ask for a note in the zip names
	// git repositories (button "GitHub")
	Git        []*GitRepo   `json:"git"`
	GitPull    bool         `json:"gitPull"` // pull --rebase before push
	LastGit    *SyncSummary `json:"lastGit,omitempty"`
	SafeUpload bool         `json:"safeUpload"` // upload to temp name, then rename
	LastSync   *SyncSummary `json:"lastSync,omitempty"`
}

type Conn struct {
	Protocol    string `json:"protocol"` // ftp | ftps | ftps-implicit | sftp
	Host        string `json:"host"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	PassStored  string `json:"pass,omitempty"` // "obf:..." or "enc:..."
	DisableEPSV bool   `json:"disableEPSV,omitempty"`
	InsecureTLS bool   `json:"insecureTLS,omitempty"`
	KeyFile     string `json:"keyFile,omitempty"` // SFTP private key (optional)
	HostKey     string `json:"hostKey,omitempty"` // SFTP host key fingerprint (trust on first use)

	pass string // plain text in memory only
}

type Pair struct {
	ID       string   `json:"id"`
	Local    string   `json:"local"`
	Remote   string   `json:"remote"`
	Enabled  bool     `json:"enabled"`
	Excludes []string `json:"excludes"` // relative to Local, e.g. "img/originals/" or "*.psd"
}

type SyncSummary struct {
	At       time.Time `json:"at"`
	Result   string    `json:"result"` // ok | errors | cancelled | failed
	Uploaded int       `json:"uploaded"`
	Bytes    int64     `json:"bytes"`
	Failed   int       `json:"failed"`
	Seconds  float64   `json:"seconds"`
	Message  string    `json:"message,omitempty"`
}

var defaultExcludes = []string{
	".git/", ".svn/", ".hg/", ".vscode/", ".idea/",
	"Thumbs.db", "desktop.ini", ".DS_Store", ".dropbox", ".dropbox.attr",
	"*.tmp", "~$*", "*conflicted copy*", "*konfliktní kopie*",
}

var tabColors = []string{"#1f8a6b", "#d9822b", "#3a6fd8", "#b0417a", "#7a5cc7", "#4a8a2a", "#c2453a", "#2a8fa8"}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

type Store struct {
	mu        sync.Mutex
	dir       string
	path      string
	cfg       *Config
	key       []byte // master key (nil = obfuscation only)
	locked    bool
	lastMtime time.Time
}

func newStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dataDir, "state"), 0o755); err != nil {
		return nil, err
	}
	_ = os.MkdirAll(filepath.Join(dataDir, "logs"), 0o755)
	s := &Store{dir: dataDir, path: filepath.Join(dataDir, "config.json")}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	cfg := &Config{Version: 1, Settings: Settings{Theme: "auto", Workers: 3}}
	if err == nil {
		if err := json.Unmarshal(b, cfg); err != nil {
			return fmt.Errorf(T("config.json je poškozený: %w"), err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if cfg.Settings.Workers < 1 || cfg.Settings.Workers > 8 {
		cfg.Settings.Workers = 3
	}
	if cfg.Settings.Theme == "" {
		cfg.Settings.Theme = "auto"
	}
	if cfg.Settings.Groups == nil {
		cfg.Settings.Groups = []*Group{}
	}
	if cfg.Settings.Builds == nil {
		cfg.Settings.Builds = []*Build{}
	}
	for _, b := range cfg.Settings.Builds {
		b.normalize()
	}
	s.cfg = cfg
	setLang(cfg.Settings.Lang)
	s.locked = cfg.Settings.MasterSalt != ""
	if s.locked && s.key != nil {
		// reload after external change while unlocked: try the key we already have
		if s.checkKey(s.key) {
			s.locked = false
		} else {
			s.key = nil
		}
	}
	for _, t := range cfg.Tabs {
		s.normalizeTab(t)
		if !s.locked {
			t.Conn.pass, _ = s.decodePass(t.Conn.PassStored)
		}
	}
	if fi, err := os.Stat(s.path); err == nil {
		s.lastMtime = fi.ModTime()
	}
	return nil
}

// reloadIfChanged picks up config.json changed by Dropbox from another PC.
func (s *Store) reloadIfChanged() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	fi, err := os.Stat(s.path)
	if err != nil || fi.ModTime().Equal(s.lastMtime) {
		return false
	}
	if err := s.load(); err != nil {
		return false
	}
	return true
}

func (s *Store) normalizeTab(t *Tab) {
	if t.ID == "" {
		t.ID = newID()
	}
	if t.Conn.Protocol == "" {
		t.Conn.Protocol = "ftp"
	}
	if t.Color == "" {
		t.Color = tabColors[0]
	}
	if t.Pairs == nil {
		t.Pairs = []*Pair{}
	}
	if t.Excludes == nil {
		t.Excludes = []string{}
	}
	if t.Checklist == nil {
		t.Checklist = []string{}
	}
	if t.Builds == nil {
		t.Builds = []string{}
	}
	if t.Backups == nil {
		t.Backups = []*BackupPair{}
	}
	if t.BackupExcludes == nil {
		t.BackupExcludes = append([]string{}, defaultBackupExcludes...)
	}
	if t.Git == nil {
		t.Git = []*GitRepo{}
	}
	if t.Fw != nil {
		t.Fw.normalize()
	}
	if t.BuildLight != nil {
		t.BuildLight.normalize()
		if t.BuildLight.Script == "" {
			t.BuildLight = nil
		}
	}
	for _, g := range t.Git {
		if g.ID == "" {
			g.ID = newID()
		}
	}
	for _, b := range t.Backups {
		if b.ID == "" {
			b.ID = newID()
		}
		if b.Excludes == nil {
			b.Excludes = []string{}
		}
	}
	for _, p := range t.Pairs {
		if p.ID == "" {
			p.ID = newID()
		}
		if p.Excludes == nil {
			p.Excludes = []string{}
		}
	}
}

// saveLocked writes config atomically. Caller holds s.mu.
func (s *Store) saveLocked() error {
	if s.locked {
		return errors.New(T("aplikace je zamčená"))
	}
	for _, t := range s.cfg.Tabs {
		t.Conn.PassStored = s.encodePass(t.Conn.pass)
	}
	b, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(s.path, b); err != nil {
		return err
	}
	if fi, err := os.Stat(s.path); err == nil {
		s.lastMtime = fi.ModTime()
	}
	return nil
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *Store) tabName(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.tab(id); t != nil {
		return t.Name
	}
	return ""
}

func (s *Store) tab(id string) *Tab {
	for _, t := range s.cfg.Tabs {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// TabCopy returns a deep copy of a tab (with plain password) for a worker.
func (s *Store) TabCopy(id string) (*Tab, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return nil, errors.New(T("aplikace je zamčená"))
	}
	t := s.tab(id)
	if t == nil {
		return nil, errors.New(T("karta neexistuje"))
	}
	c := *t
	c.Conn.pass = t.Conn.pass
	c.Pairs = nil
	for _, p := range t.Pairs {
		pp := *p
		pp.Excludes = append([]string{}, p.Excludes...)
		c.Pairs = append(c.Pairs, &pp)
	}
	c.Excludes = append([]string{}, t.Excludes...)
	c.Builds = append([]string{}, t.Builds...)
	if t.BuildInfo != nil {
		c.BuildInfo = map[string]string{}
		for k, v := range t.BuildInfo {
			c.BuildInfo[k] = v
		}
	}
	if t.BuildLight != nil {
		l := *t.BuildLight
		c.BuildLight = &l
	}
	c.BackupExcludes = append([]string{}, t.BackupExcludes...)
	c.Backups = nil
	if t.Fw != nil {
		f := *t.Fw
		f.Variants = nil
		for _, v := range t.Fw.Variants {
			vv := *v
			f.Variants = append(f.Variants, &vv)
		}
		c.Fw = &f
	}
	c.Git = nil
	for _, g := range t.Git {
		gg := *g
		c.Git = append(c.Git, &gg)
	}
	for _, b := range t.Backups {
		bb := *b
		bb.Excludes = append([]string{}, b.Excludes...)
		c.Backups = append(c.Backups, &bb)
	}
	return &c, nil
}

// writeFileAtomic writes data to a temp file and renames it over path. The data folder usually lives in Dropbox
// (or OneDrive), whose client briefly opens freshly changed files to upload them – during that moment Windows refuses
// the write or the rename ("Access is denied" / "being used by another process"). So both steps are retried for up
// to ~4 s before giving up, which makes fast autosave (typing a card name) reliable.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	var err error
	for i, wait := 0, 50*time.Millisecond; i < 12; i++ {
		if err = os.WriteFile(tmp, data, 0o600); err == nil {
			if err = os.Rename(tmp, path); err == nil {
				return nil
			}
		}
		time.Sleep(wait)
		if wait < 600*time.Millisecond {
			wait *= 2
		}
	}
	_ = os.Remove(tmp)
	return err
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------------------------------------------------------------------------
// Password encoding
//   obf:  AES-GCM with a built-in key  -> only hides the password from a casual look
//   enc:  AES-GCM with a key derived from the master password (scrypt)
// ---------------------------------------------------------------------------

var obfKey = sha256.Sum256([]byte("Leonzilla/obfuscation/v1 - this is not real protection"))

const masterCheckPlain = "leonzilla-master-ok"

func gcmSeal(key []byte, plain string) string {
	blk, _ := aes.NewCipher(key)
	g, _ := cipher.NewGCM(blk)
	nonce := make([]byte, g.NonceSize())
	_, _ = rand.Read(nonce)
	return base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, []byte(plain), nil))
}

func gcmOpen(key []byte, enc string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	blk, _ := aes.NewCipher(key)
	g, _ := cipher.NewGCM(blk)
	if len(raw) < g.NonceSize() {
		return "", errors.New("short")
	}
	p, err := g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], nil)
	return string(p), err
}

func (s *Store) encodePass(plain string) string {
	if plain == "" {
		return ""
	}
	if s.key != nil {
		return "enc:" + gcmSeal(s.key, plain)
	}
	return "obf:" + gcmSeal(obfKey[:], plain)
}

func (s *Store) decodePass(stored string) (string, error) {
	switch {
	case stored == "":
		return "", nil
	case strings.HasPrefix(stored, "obf:"):
		return gcmOpen(obfKey[:], stored[4:])
	case strings.HasPrefix(stored, "enc:"):
		if s.key == nil {
			return "", errors.New(T("zamčeno"))
		}
		return gcmOpen(s.key, stored[4:])
	}
	return stored, nil // plain text (hand-edited config)
}

func deriveKey(password string, saltB64 string) ([]byte, error) {
	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		return nil, err
	}
	return scrypt.Key([]byte(password), salt, 1<<15, 8, 1, 32)
}

func (s *Store) checkKey(key []byte) bool {
	p, err := gcmOpen(key, s.cfg.Settings.MasterCheck)
	return err == nil && p == masterCheckPlain
}

func (s *Store) Unlock(password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.locked {
		return nil
	}
	key, err := deriveKey(password, s.cfg.Settings.MasterSalt)
	if err != nil || !s.checkKey(key) {
		return errors.New(T("Špatné hlavní heslo"))
	}
	s.key = key
	s.locked = false
	for _, t := range s.cfg.Tabs {
		t.Conn.pass, _ = s.decodePass(t.Conn.PassStored)
	}
	return nil
}

// SetMaster sets, changes or (newPass == "") removes the master password.
func (s *Store) SetMaster(oldPass, newPass string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return errors.New(T("aplikace je zamčená"))
	}
	if s.cfg.Settings.MasterSalt != "" {
		key, err := deriveKey(oldPass, s.cfg.Settings.MasterSalt)
		if err != nil || !s.checkKey(key) {
			return errors.New(T("Současné hlavní heslo nesouhlasí"))
		}
	}
	if newPass == "" {
		s.key = nil
		s.cfg.Settings.MasterSalt = ""
		s.cfg.Settings.MasterCheck = ""
	} else {
		salt := make([]byte, 16)
		_, _ = rand.Read(salt)
		s.cfg.Settings.MasterSalt = base64.StdEncoding.EncodeToString(salt)
		key, err := deriveKey(newPass, s.cfg.Settings.MasterSalt)
		if err != nil {
			return err
		}
		s.key = key
		s.cfg.Settings.MasterCheck = gcmSeal(key, masterCheckPlain)
	}
	return s.saveLocked()
}

// ---------------------------------------------------------------------------
// Per-pair upload state (what was uploaded last time) – Leonzilla_data/state/<tab>.json
// ---------------------------------------------------------------------------

type FileRec struct {
	S int64 `json:"s"` // local size
	M int64 `json:"m"` // local mtime (unix seconds)
}

type PairState struct {
	Sig   string              `json:"sig"` // host|user|remote – reset when it changes
	Files map[string]*FileRec `json:"files"`
}

type TabState struct {
	mu    sync.Mutex
	path  string
	Pairs map[string]*PairState `json:"pairs"`
	dirty bool
}

func loadTabState(dataDir, tabID string) *TabState {
	ts := &TabState{path: filepath.Join(dataDir, "state", tabID+".json"), Pairs: map[string]*PairState{}}
	if b, err := os.ReadFile(ts.path); err == nil {
		_ = json.Unmarshal(b, ts)
		if ts.Pairs == nil {
			ts.Pairs = map[string]*PairState{}
		}
	}
	return ts
}

func (ts *TabState) pair(pairID, sig string) *PairState {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ps := ts.Pairs[pairID]
	if ps == nil || ps.Sig != sig {
		ps = &PairState{Sig: sig, Files: map[string]*FileRec{}}
		ts.Pairs[pairID] = ps
		ts.dirty = true
	}
	return ps
}

func (ts *TabState) get(ps *PairState, rel string) *FileRec {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ps.Files[rel]
}

func (ts *TabState) set(ps *PairState, rel string, r *FileRec) {
	ts.mu.Lock()
	ps.Files[rel] = r
	ts.dirty = true
	ts.mu.Unlock()
}

func (ts *TabState) save() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if !ts.dirty {
		return
	}
	b, err := json.Marshal(ts)
	if err == nil && writeFileAtomic(ts.path, b) == nil {
		ts.dirty = false
	}
}
