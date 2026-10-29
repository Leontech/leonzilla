package main

// Remote access from a phone (App settings → Remote access).
//
// A second HTTP server on a fixed port, off by default and switched on per PC (Leonzilla_data\mobile\<PC>.json – the
// config.json is shared by all PCs through Dropbox, and only the chosen PC should listen). It is meant for the local
// network or a VPN (WireGuard, Tailscale …), never for the open internet:
//   - only private / loopback / VPN addresses may connect (everything else gets 403, even with a forwarded port),
//   - one password (stored only as a scrypt hash), a session cookie per device (stored only as a SHA-256 hash),
//     failed logins slow down and lock the address for 10 minutes after 5 attempts,
//   - API requests need the X-LZ-Mobile header (a page of another site cannot send it) and the session cookie,
//   - only RUNNING what the cards already have is allowed (synchronize, builds, own scripts, backups, GitHub,
//     FW build / flash / monitor, progress, log) – no card or app settings, no dialogs or programs on the PC.
// The page itself is the same UI as the window, in "remote" mode (window.LZ_REMOTE), laid out for a phone.

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
	"golang.org/x/crypto/scrypt"
)

const mobileDefaultPort = 8765
const mobileCookie = "lz_mobile"

type mobileSession struct {
	ID       string    `json:"id"`   // SHA-256 of the cookie value
	Name     string    `json:"name"` // device, from the login (e.g. "Android · Chrome")
	IP       string    `json:"ip"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"lastSeen"`
}

type mobileConf struct {
	Enabled  bool             `json:"enabled"`
	Port     int              `json:"port"`
	PassSalt string           `json:"passSalt,omitempty"`
	PassHash string           `json:"passHash,omitempty"`
	Sessions []*mobileSession `json:"sessions"`
}

type mobileFail struct {
	n     int
	until time.Time
}

type Mobile struct {
	a     *API
	mu    sync.Mutex
	path  string
	conf  mobileConf
	srv   *http.Server
	addr  string // listening address ("" = not running)
	err   string // last start error
	fails map[string]*mobileFail
	saved time.Time // last save of LastSeen
}

func newMobile(a *API) *Mobile {
	m := &Mobile{a: a, path: filepath.Join(a.store.dir, "mobile", pcName()+".json"), fails: map[string]*mobileFail{}}
	if b, err := os.ReadFile(m.path); err == nil {
		_ = json.Unmarshal(b, &m.conf)
	}
	if m.conf.Port <= 0 || m.conf.Port > 65535 {
		m.conf.Port = mobileDefaultPort
	}
	if m.conf.Sessions == nil {
		m.conf.Sessions = []*mobileSession{}
	}
	return m
}

// saveLocked writes the per-PC file. Caller holds m.mu.
func (m *Mobile) saveLocked() error {
	_ = os.MkdirAll(filepath.Dir(m.path), 0o755)
	b, _ := json.MarshalIndent(m.conf, "", "  ")
	return writeFileAtomic(m.path, b)
}

// apply starts or stops the server according to the settings.
func (m *Mobile) apply() {
	m.mu.Lock()
	defer m.mu.Unlock()
	want := m.conf.Enabled && m.conf.PassHash != ""
	if m.srv != nil && (!want || m.addr != fmt.Sprintf(":%d", m.conf.Port)) {
		_ = m.srv.Close()
		m.srv, m.addr = nil, ""
	}
	if !want || m.srv != nil {
		if !want {
			m.err = ""
		}
		return
	}
	addr := fmt.Sprintf(":%d", m.conf.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		m.err = err.Error()
		return
	}
	m.err = ""
	m.addr = addr
	m.srv = &http.Server{Handler: m.handler(), ReadHeaderTimeout: 10 * time.Second}
	go func(s *http.Server) { _ = s.Serve(ln) }(m.srv)
}

// ---------------------------------------------------------------------------
// who may connect

var cgnat = mustCIDR("100.64.0.0/10") // Tailscale and other VPNs

func mustCIDR(s string) *net.IPNet { _, n, _ := net.ParseCIDR(s); return n }

func mobileAllowedIP(remoteAddr string) (string, bool) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host, false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return host, ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || cgnat.Contains(ip)
}

// ---------------------------------------------------------------------------
// passwords and sessions

func mobileHash(password, saltB64 string) string {
	salt, _ := base64.StdEncoding.DecodeString(saltB64)
	k, _ := scrypt.Key([]byte(password), salt, 1<<15, 8, 1, 32)
	return base64.StdEncoding.EncodeToString(k)
}

func sha(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

// session returns the session of the request (nil = not logged in). Updates LastSeen.
func (m *Mobile) session(r *http.Request) *mobileSession {
	c, err := r.Cookie(mobileCookie)
	if err != nil || len(c.Value) < 32 {
		return nil
	}
	id := sha(c.Value)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.conf.Sessions {
		if subtle.ConstantTimeCompare([]byte(s.ID), []byte(id)) == 1 {
			s.LastSeen = time.Now()
			noteRemote()
			if ip, _ := mobileAllowedIP(r.RemoteAddr); ip != "" {
				s.IP = ip
			}
			if time.Since(m.saved) > 10*time.Minute {
				m.saved = time.Now()
				_ = m.saveLocked()
			}
			c := *s
			return &c
		}
	}
	return nil
}

func (m *Mobile) hLogin(w http.ResponseWriter, r *http.Request) {
	ip, _ := mobileAllowedIP(r.RemoteAddr)
	var req struct {
		Password string `json:"password"`
		Device   string `json:"device"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	m.mu.Lock()
	f := m.fails[ip]
	if f != nil && time.Now().Before(f.until) {
		m.mu.Unlock()
		writeErr(w, 429, fmt.Errorf(T("Příliš mnoho pokusů – zkus to za %d min"), int(time.Until(f.until).Minutes())+1))
		return
	}
	salt, hash := m.conf.PassSalt, m.conf.PassHash
	m.mu.Unlock()
	if hash == "" || subtle.ConstantTimeCompare([]byte(mobileHash(req.Password, salt)), []byte(hash)) != 1 {
		time.Sleep(700 * time.Millisecond)
		m.mu.Lock()
		if f == nil || time.Now().After(f.until) && f.n >= 5 {
			f = &mobileFail{}
			m.fails[ip] = f
		}
		f.n++
		if f.n >= 5 {
			f.until = time.Now().Add(10 * time.Minute)
		}
		m.mu.Unlock()
		writeErr(w, 401, errors.New(T("Špatné heslo")))
		return
	}
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	name := strings.TrimSpace(req.Device)
	if r := []rune(name); len(r) > 60 {
		name = string(r[:60])
	}
	if name == "" {
		name = T("Zařízení")
	}
	oldID := ""
	if c, err := r.Cookie(mobileCookie); err == nil && c.Value != "" {
		oldID = sha(c.Value)
	}
	m.mu.Lock()
	delete(m.fails, ip)
	// the same phone / browser signing in again replaces its old session (one line per device in the list)
	keep := m.conf.Sessions[:0]
	for _, s := range m.conf.Sessions {
		if s.ID == oldID || (s.Name == name && s.IP == ip) {
			continue
		}
		keep = append(keep, s)
	}
	m.conf.Sessions = append(keep, &mobileSession{ID: sha(tok), Name: name, IP: ip, Created: time.Now(), LastSeen: time.Now()})
	if len(m.conf.Sessions) > 20 { // the oldest devices fall out
		sort.Slice(m.conf.Sessions, func(i, j int) bool { return m.conf.Sessions[i].LastSeen.After(m.conf.Sessions[j].LastSeen) })
		m.conf.Sessions = m.conf.Sessions[:20]
	}
	err := m.saveLocked()
	m.mu.Unlock()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: mobileCookie, Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 400 * 24 * 3600})
	writeJSON(w, map[string]bool{"ok": true})
}

func (m *Mobile) hLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(mobileCookie); err == nil {
		id := sha(c.Value)
		m.mu.Lock()
		out := m.conf.Sessions[:0]
		for _, s := range m.conf.Sessions {
			if s.ID != id {
				out = append(out, s)
			}
		}
		m.conf.Sessions = out
		_ = m.saveLocked()
		m.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: mobileCookie, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------------------
// what the phone may do: only running what the cards already have

var mobileRoutes = func() []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, s := range []string{
		`GET /api/state`, `POST /api/unlock`, `POST /api/ping`, `GET /api/ui`,
		`POST /api/tabs/[^/]+/sync`, `POST /api/tabs/[^/]+/cancel`, `GET /api/tabs/[^/]+/job`,
		`GET /api/radstudio`, `GET /api/dropbox`, `GET /api/serialports`, `GET /api/git/info`,
		`POST /api/builds/inspect`, `POST /api/localcheck`,
		`POST /api/tabs/[^/]+/fw/run`, `GET /api/tabs/[^/]+/fw`, `POST /api/tabs/[^/]+/fw/(input|reset|stop)`,
		`POST /api/git/(status|diff|commit)`, `POST /api/tabs/[^/]+/gitdone`,
	} {
		out = append(out, regexp.MustCompile(`^`+s+`$`))
	}
	return out
}()

func mobileAllowedRoute(r *http.Request) bool {
	k := r.Method + " " + r.URL.Path
	for _, re := range mobileRoutes {
		if re.MatchString(k) {
			return true
		}
	}
	return false
}

// gitPathAllowed: from the phone, git works only in repositories set on some card.
func (m *Mobile) gitPathAllowed(p string) bool {
	p = filepath.Clean(strings.TrimSpace(p))
	m.a.store.mu.Lock()
	defer m.a.store.mu.Unlock()
	for _, t := range m.a.store.cfg.Tabs {
		for _, g := range t.Git {
			if g.Path != "" && strings.EqualFold(filepath.Clean(g.Path), p) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------

func (m *Mobile) handler() http.Handler {
	sub, _ := fs.Sub(uiFS, "ui")
	static := http.FileServer(http.FS(sub))
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "DENY")
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			b, err := fs.ReadFile(sub, "index.html")
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			// remote mode + phone bits (installable as an app: "Add to home screen")
			inj := `<script>window.LZ_REMOTE=1</script><link rel="manifest" href="/manifest.webmanifest"><meta name="theme-color" content="#121816"><meta name="mobile-web-app-capable" content="yes"><meta name="apple-mobile-web-app-capable" content="yes">`
			b = bytes.Replace(b, []byte("<head>"), []byte("<head>"+inj), 1)
			b = bytes.Replace(b, []byte(`name="viewport" content="`), []byte(`name="viewport" content="viewport-fit=cover, `), 1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(b)
			return
		}
		if r.URL.Path == "/manifest.webmanifest" {
			w.Header().Set("Content-Type", "application/manifest+json")
			_, _ = io.WriteString(w, `{"name":"Leonzilla","short_name":"Leonzilla","start_url":"/","display":"standalone","background_color":"#121816","theme_color":"#121816","icons":[{"src":"/icon192.png","sizes":"192x192","type":"image/png"},{"src":"/icon512.png","sizes":"512x512","type":"image/png"},{"src":"/icon.svg","sizes":"any","type":"image/svg+xml"}]}`)
			return
		}
		static.ServeHTTP(w, r)
	})
	mux.HandleFunc("POST /api/mobile/login", m.hLogin)
	mux.HandleFunc("POST /api/mobile/logout", m.hLogout)
	mux.HandleFunc("GET /api/mobile/me", func(w http.ResponseWriter, r *http.Request) {
		s := m.session(r)
		writeJSON(w, map[string]any{"loggedIn": s != nil, "pc": pcName(), "version": appVersion, "master": m.a.store.hasMaster(), "locked": m.a.store.isLocked()})
	})
	api := m.a.apiMux()
	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-LZ-Mobile") != "1" {
			http.Error(w, "missing header", http.StatusForbidden)
			return
		}
		if m.session(r) == nil {
			writeErr(w, 401, errors.New(T("Nejsi přihlášený")))
			return
		}
		if !mobileAllowedRoute(r) {
			writeErr(w, 403, errors.New(T("Z mobilu jde jen spouštět – nastavení je jen na PC")))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/git/") && r.Method == "POST" {
			body, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
			var req struct {
				Path string `json:"path"`
			}
			_ = json.Unmarshal(body, &req)
			if !m.gitPathAllowed(req.Path) {
				writeErr(w, 403, errors.New(T("Repozitář není na žádné kartě")))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		if r.URL.Path != "/api/state" && r.URL.Path != "/api/unlock" && r.URL.Path != "/api/ping" && m.a.store.isLocked() {
			writeErr(w, http.StatusLocked, errors.New(T("Aplikace je zamčená hlavním heslem")))
			return
		}
		api.ServeHTTP(w, r)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := mobileAllowedIP(r.RemoteAddr); !ok {
			http.Error(w, "Leonzilla: remote access only from the local network or a VPN", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// settings (only from the window on the PC, through the normal token API)

// mobileURLs lists the addresses the phone can use (IPv4 of the network adapters that are up).
func mobileURLs(port int) []string {
	var out []string
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				if v4 := n.IP.To4(); v4 != nil && (v4.IsPrivate() || cgnat.Contains(v4)) {
					out = append(out, fmt.Sprintf("http://%s:%d/", v4, port))
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func (m *Mobile) view() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	ss := append([]*mobileSession{}, m.conf.Sessions...)
	sort.Slice(ss, func(i, j int) bool { return ss[i].LastSeen.After(ss[j].LastSeen) })
	type sv struct {
		Name     string    `json:"name"`
		IP       string    `json:"ip"`
		Created  time.Time `json:"created"`
		LastSeen time.Time `json:"lastSeen"`
	}
	var list []sv
	for _, s := range ss {
		list = append(list, sv{s.Name, s.IP, s.Created, s.LastSeen})
	}
	return map[string]any{"enabled": m.conf.Enabled, "port": m.conf.Port, "hasPass": m.conf.PassHash != "", "running": m.srv != nil,
		"error": m.err, "urls": mobileURLs(m.conf.Port), "sessions": list, "pc": pcName()}
}

func (a *API) hMobileGet(w http.ResponseWriter, r *http.Request) { writeJSON(w, a.mobile.view()) }

func (a *API) hMobileSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled  *bool   `json:"enabled"`
		Port     *int    `json:"port"`
		Password *string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	m := a.mobile
	m.mu.Lock()
	if req.Password != nil {
		if len([]rune(*req.Password)) < 6 {
			m.mu.Unlock()
			writeErr(w, 400, errors.New(T("Heslo pro vzdálený přístup musí mít aspoň 6 znaků")))
			return
		}
		salt := make([]byte, 16)
		_, _ = rand.Read(salt)
		m.conf.PassSalt = base64.StdEncoding.EncodeToString(salt)
		m.conf.PassHash = mobileHash(*req.Password, m.conf.PassSalt)
		m.conf.Sessions = []*mobileSession{} // a new password logs every device out
	}
	if req.Port != nil {
		if *req.Port < 1024 || *req.Port > 65535 {
			m.mu.Unlock()
			writeErr(w, 400, errors.New(T("Port musí být 1024–65535")))
			return
		}
		m.conf.Port = *req.Port
	}
	if req.Enabled != nil {
		if *req.Enabled && m.conf.PassHash == "" {
			m.mu.Unlock()
			writeErr(w, 400, errors.New(T("Nejdřív nastav heslo pro vzdálený přístup")))
			return
		}
		m.conf.Enabled = *req.Enabled
	}
	err := m.saveLocked()
	m.mu.Unlock()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	m.apply()
	writeJSON(w, m.view())
}

func (a *API) hMobileLogoutAll(w http.ResponseWriter, r *http.Request) {
	m := a.mobile
	m.mu.Lock()
	m.conf.Sessions = []*mobileSession{}
	err := m.saveLocked()
	m.mu.Unlock()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, m.view())
}

// hMobileQR draws a QR code of one of the addresses (so the phone does not have to type it).
func (a *API) hMobileQR(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("url")
	ok := false
	for _, x := range mobileURLs(a.mobile.conf.Port) {
		ok = ok || x == u
	}
	if !ok {
		http.Error(w, "unknown address", 400)
		return
	}
	png, err := qrcode.Encode(u, qrcode.Medium, 320)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}
