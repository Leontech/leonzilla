package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (a *API) hFwDetect(w http.ResponseWriter, r *http.Request) {
	var req struct{ Project string }
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	vs, err := detectFwVariants(req.Project)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, map[string]any{"variants": vs, "idfProfile": findIdfProfile()})
}

func (a *API) hFwRun(w http.ResponseWriter, r *http.Request) {
	var req fwRunReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	tab, err := a.store.TabCopy(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	req.StopDB = req.StopDB || tab.Dropbox == "stop" // card setting "Dropbox při buildu: vždy vypnout"
	if err := a.fw.Start(tab, req); err != nil {
		writeErr(w, 409, err)
		return
	}
	// remember the choice on the card
	a.store.mu.Lock()
	if t := a.store.tab(tab.ID); t != nil && t.Fw != nil {
		t.Fw.LastVariant, t.Fw.LastAction = req.Variant, req.Action
		if strings.TrimSpace(req.Port) != "" {
			t.Fw.LastPort = strings.TrimSpace(req.Port)
		}
		if req.Action != "monitor" { // checkboxes are hidden for "monitor only"
			rc := req.Reconfig
			t.Fw.LastClean, t.Fw.LastReconf = req.CleanCfg, &rc
		}
		_ = a.store.saveLocked()
	}
	a.store.mu.Unlock()
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *API) hFwState(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.Atoi(r.URL.Query().Get("log"))
	writeJSON(w, a.fw.Snapshot(r.PathValue("id"), since))
}

func (a *API) hFwInput(w http.ResponseWriter, r *http.Request) {
	var req struct{ Text string }
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := a.fw.Input(r.PathValue("id"), strings.TrimRight(req.Text, "\r\n")); err != nil {
		writeErr(w, 409, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *API) hFwReset(w http.ResponseWriter, r *http.Request) {
	if err := a.fw.Reset(r.PathValue("id")); err != nil {
		writeErr(w, 409, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *API) hFwStop(w http.ResponseWriter, r *http.Request) {
	a.fw.Stop(r.PathValue("id"))
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *API) hFwTerminal(w http.ResponseWriter, r *http.Request) {
	tab, err := a.store.TabCopy(r.PathValue("id"))
	if err != nil || tab.Fw == nil || tab.Fw.Project == "" {
		writeErr(w, 400, errors.New(T("Karta nemá nastavenou složku FW projektu")))
		return
	}
	if st, err := os.Stat(tab.Fw.Project); err != nil || !st.IsDir() {
		writeErr(w, 400, errors.New(T("Složka projektu neexistuje")))
		return
	}
	if err := openIdfTerminal(idfProfile(tab.Fw), filepath.Clean(tab.Fw.Project)); err != nil {
		writeErr(w, 500, errors.New(T("Terminál jde otevřít jen ve Windows")))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
