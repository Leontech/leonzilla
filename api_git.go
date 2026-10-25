package main

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

var gitMu sync.Mutex // one commit/push at a time

func (a *API) hGitInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"found": gitPath() != "", "path": gitPath()})
}

func (a *API) hGitStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path  string `json:"path"`
		Fetch bool   `json:"fetch"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, gitStatus(req.Path, req.Fetch))
}

func (a *API) hGitDiff(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string  `json:"path"`
		File GitFile `json:"file"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.File.Path == "" || strings.Contains(req.File.Path, "..") {
		writeErr(w, 400, errors.New(T("neplatný soubor")))
		return
	}
	text, bin, trunc, err := gitDiff(req.Path, req.File)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]any{"text": text, "binary": bin, "truncated": trunc})
}

func (a *API) hGitCommit(w http.ResponseWriter, r *http.Request) {
	var req gitCommitReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	gitMu.Lock()
	defer gitMu.Unlock()
	name := req.Path
	logf := func(kind, text string) {
		if req.Tab != "" {
			appendTabLog(a.store.dir, req.Tab, kind, "git "+name+" · "+text)
		}
	}
	writeJSON(w, gitCommit(req, logf))
}

// hGitDone stores the result of a whole "GitHub" run on the card.
func (a *API) hGitDone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Result  string `json:"result"`
		Message string `json:"message"`
		Commits int    `json:"commits"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	t := a.store.tab(r.PathValue("id"))
	if t == nil {
		writeErr(w, 404, errors.New(T("karta neexistuje")))
		return
	}
	t.LastGit = &SyncSummary{At: time.Now(), Result: req.Result, Uploaded: req.Commits, Message: req.Message}
	_ = a.store.saveLocked()
	appendTabLog(a.store.dir, t.ID, "info", "GitHub: "+req.Message)
	writeJSON(w, map[string]any{"ok": true, "lastGit": t.LastGit})
}
