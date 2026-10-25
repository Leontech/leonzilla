package main

// Per-PC state of the window: collapsed groups in the overview, open tabs at the top and the last card / view.
// The page runs on a random local port, so the browser storage of the window gets a new origin on every start
// and forgets everything. The state is kept next to the data instead, one small file per computer
// (Leonzilla_data/ui/<computer>.json): each PC (e.g. home and work with the same Dropbox) keeps its own layout
// and the cloud sync never has to merge one shared file.

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var (
	uiMu     sync.Mutex
	reUIName = regexp.MustCompile(`[^a-z0-9._-]+`)
	uiKeys   = map[string]bool{"collapsed": true, "open": true, "current": true, "view": true}
)

// pcName is the computer name used for the per-PC file (lower case, safe for a file name).
func pcName() string {
	n := os.Getenv("COMPUTERNAME")
	if n == "" {
		n, _ = os.Hostname()
	}
	n = strings.Trim(reUIName.ReplaceAllString(strings.ToLower(n), "-"), "-.")
	if n == "" {
		n = "pc"
	}
	return n
}

func (s *Store) uiPath() string { return filepath.Join(s.dir, "ui", pcName()+".json") }

// loadUI returns the saved window state of this PC ({} when there is none).
func (s *Store) loadUI() map[string]json.RawMessage {
	uiMu.Lock()
	defer uiMu.Unlock()
	m := map[string]json.RawMessage{}
	if b, err := os.ReadFile(s.uiPath()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	for k := range m {
		if !uiKeys[k] {
			delete(m, k)
		}
	}
	return m
}

// saveUI merges the given keys into the state of this PC and writes it atomically.
func (s *Store) saveUI(upd map[string]json.RawMessage) error {
	uiMu.Lock()
	defer uiMu.Unlock()
	m := map[string]json.RawMessage{}
	if b, err := os.ReadFile(s.uiPath()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	for k, v := range upd {
		if !uiKeys[k] || len(v) > 16<<10 || !json.Valid(v) {
			return errors.New("ui: " + k)
		}
		m[k] = v
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.uiPath()), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(s.uiPath(), b) // retries while Dropbox holds the file
}

func (a *API) hUIGet(w http.ResponseWriter, r *http.Request) { writeJSON(w, a.store.loadUI()) }

func (a *API) hUISet(w http.ResponseWriter, r *http.Request) {
	upd := map[string]json.RawMessage{}
	if err := readJSON(r, &upd); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := a.store.saveUI(upd); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
