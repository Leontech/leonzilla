package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

const appVersion = "1.11.17"
const appTitle = "Leonzilla"

// dataDir = folder "Leonzilla_data" next to the exe (portable – lives in Dropbox together with the exe).
func dataDir() string {
	if d := os.Getenv("LEONZILLA_DATA"); d != "" {
		return d
	}
	exe, err := os.Executable()
	if err != nil {
		return "Leonzilla_data"
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Join(filepath.Dir(exe), "Leonzilla_data")
}

func main() {
	setLang("") // by the OS until the settings are loaded
	if !acquireSingleInstance() {
		return
	}
	restoreDropboxAfterCrash() // a previous run stopped Dropbox for a build and did not get to start it again
	store, err := newStore(dataDir())
	if err != nil {
		fatalBox(T("Nelze načíst nastavení: ") + err.Error())
		return
	}
	// a hard crash (also in native Windows code) is written here instead of disappearing silently
	if f, err := os.OpenFile(filepath.Join(store.dir, "logs", "crash.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		_ = debug.SetCrashOutput(f, debug.CrashOptions{})
		f.Close()
	}
	runner := newRunner(store)
	tokB := make([]byte, 16)
	_, _ = rand.Read(tokB)
	token := hex.EncodeToString(tokB)
	api := newAPI(store, runner, token)

	addr := "127.0.0.1:0"
	if p := os.Getenv("LEONZILLA_PORT"); p != "" {
		addr = "127.0.0.1:" + p
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fatalBox(T("Nelze otevřít lokální port: ") + err.Error())
		return
	}
	api.host = ln.Addr().String()
	srv := &http.Server{Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()

	url := fmt.Sprintf("http://%s/#t=%s", ln.Addr().String(), token)
	if os.Getenv("LEONZILLA_HEADLESS") != "" {
		log.Printf("Leonzilla %s: %s  (data %s)", appVersion, url, store.dir)
		select {}
	}
	if !runWindow(url) {
		// no WebView2 – fall back to the default browser and quit when the page is closed
		_ = openWithShell(url)
		time.Sleep(20 * time.Second)
		for api.sinceLastPing() < 30*time.Second || runner.AnyRunning() {
			time.Sleep(5 * time.Second)
		}
	}
}
