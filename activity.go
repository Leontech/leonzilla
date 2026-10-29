package main

// activity.json – what Leonzilla is doing, for other programs on the same PC (e.g. LeoLink, which puts the PC
// to sleep after a remote session): %LOCALAPPDATA%\Leonzilla\activity.json
//
//	{"busy": true|false, "lastRemote": "<time of the last request from the phone>", "updated": "<time>"}
//
// busy = a job (upload, build, backup …) is running. Written when something changes, otherwise once a minute.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var activity struct {
	mu         sync.Mutex
	lastRemote time.Time
}

// noteRemote: a logged-in request came from the phone (remote access).
func noteRemote() {
	activity.mu.Lock()
	activity.lastRemote = time.Now()
	activity.mu.Unlock()
}

func watchActivity(r *Runner) {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return
	}
	p := filepath.Join(local, "Leonzilla", "activity.json")
	var lastBusy bool
	var lastRemote, lastWrite time.Time
	for {
		busy := r.AnyRunning()
		activity.mu.Lock()
		remote := activity.lastRemote
		activity.mu.Unlock()
		if busy != lastBusy || !remote.Equal(lastRemote) && time.Since(lastWrite) > 30*time.Second || time.Since(lastWrite) > time.Minute {
			b, _ := json.Marshal(map[string]any{"busy": busy, "lastRemote": remote, "updated": time.Now()})
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			if writeFileAtomic(p, b) == nil {
				lastBusy, lastRemote, lastWrite = busy, remote, time.Now()
			}
		}
		time.Sleep(5 * time.Second)
	}
}
