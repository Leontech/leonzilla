package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Job = one synchronization (or preview) of one tab
// ---------------------------------------------------------------------------

type UploadItem struct {
	PairID string `json:"-"`
	Local  string `json:"-"`
	Remote string `json:"remote"`
	Rel    string `json:"rel"`
	Size   int64  `json:"size"`
	Reason string `json:"reason"` // new | changed | forced
	mtime  int64
	ps     *PairState
}

type PairPlan struct {
	PairID   string        `json:"pairId"`
	Local    string        `json:"local"`
	Remote   string        `json:"remote"`
	Dirs     []string      `json:"dirs"` // remote dirs to create
	Files    []*UploadItem `json:"files"`
	Same     int           `json:"same"`
	Excluded int           `json:"excluded"`
	Error    string        `json:"error,omitempty"`
}

type LogLine struct {
	T    time.Time `json:"t"`
	Kind string    `json:"kind"` // info | up | err | warn
	Text string    `json:"text"`
}

type Job struct {
	TabID   string    `json:"tabId"`
	Preview bool      `json:"preview"`
	Force   bool      `json:"force"`
	Build   bool      `json:"build"`     // run the card's builds first
	Backup  bool      `json:"backup"`    // zip backup (no synchronization)
	Sync    bool      `json:"syncAfter"` // synchronize (after the builds)
	stopDB  bool      // stop Dropbox during the builds (chosen in the Dropbox check)
	Steps   []*Step   `json:"steps"`
	Phase   string    `json:"phase"` // building | connecting | scanning | uploading | done
	Result  string    `json:"result,omitempty"`
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended,omitempty"`
	Server  string    `json:"server"`

	ScanFiles  int64   `json:"scanFiles"`
	FilesTotal int     `json:"filesTotal"`
	FilesDone  int64   `json:"filesDone"`
	FilesFail  int64   `json:"filesFail"`
	BytesTotal int64   `json:"bytesTotal"`
	BytesDone  int64   `json:"bytesDone"`
	DirsMade   int64   `json:"dirsMade"`
	Speed      float64 `json:"speed"` // B/s

	Plans  []*PairPlan `json:"plans,omitempty"` // only for preview
	Active []string    `json:"active"`
	Log    []LogLine   `json:"log"`

	mu        sync.Mutex
	cancel    context.CancelFunc
	active    map[int]string
	lastB     int64
	lastT     time.Time
	logFile   *os.File
	allLines  int
	plans     []*buildPlan
	backups   []*BackupPair
	synced    bool
	notes     map[*BackupPair]string // backup: note per zip
	names     map[*BackupPair]string // backup: own zip name (without .zip)
	buildInfo map[string]string      // build ID → "Info k verzi" changed in the confirmation (this run only)
}

func (j *Job) logf(kind, format string, a ...any) {
	line := LogLine{T: time.Now(), Kind: kind, Text: fmt.Sprintf(format, a...)}
	j.mu.Lock()
	j.Log = append(j.Log, line)
	if len(j.Log) > 4000 {
		j.Log = j.Log[len(j.Log)-3000:]
	}
	j.allLines++
	if j.logFile != nil {
		fmt.Fprintf(j.logFile, "%s  %-4s %s\n", line.T.Format("2006-01-02 15:04:05"), kind, line.Text)
	}
	j.mu.Unlock()
}

func (j *Job) setPhase(p string) {
	j.mu.Lock()
	j.Phase = p
	j.mu.Unlock()
}

func (j *Job) setActive(worker int, name string) {
	j.mu.Lock()
	if name == "" {
		delete(j.active, worker)
	} else {
		j.active[worker] = name
	}
	j.mu.Unlock()
}

// Snapshot returns a copy safe to marshal.
func (j *Job) Snapshot(withLogSince int) map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now()
	bd := atomic.LoadInt64(&j.BytesDone)
	if dt := now.Sub(j.lastT).Seconds(); dt >= 0.7 {
		inst := float64(bd-j.lastB) / dt
		if j.Speed == 0 {
			j.Speed = inst
		} else {
			j.Speed = j.Speed*0.6 + inst*0.4
		}
		j.lastB, j.lastT = bd, now
	}
	act := make([]string, 0, len(j.active))
	for _, v := range j.active {
		act = append(act, v)
	}
	sort.Strings(act)
	logFrom := 0
	// client tells how many lines it has seen in total; send only new ones
	base := j.allLines - len(j.Log)
	if withLogSince > base {
		logFrom = withLogSince - base
	}
	if logFrom > len(j.Log) {
		logFrom = len(j.Log)
	}
	return map[string]any{
		"tabId": j.TabID, "preview": j.Preview, "force": j.Force, "phase": j.Phase, "result": j.Result,
		"started": j.Started, "ended": j.Ended, "server": j.Server,
		"scanFiles": atomic.LoadInt64(&j.ScanFiles), "filesTotal": j.FilesTotal,
		"filesDone": atomic.LoadInt64(&j.FilesDone), "filesFail": atomic.LoadInt64(&j.FilesFail),
		"bytesTotal": j.BytesTotal, "bytesDone": bd, "dirsMade": atomic.LoadInt64(&j.DirsMade),
		"speed": j.Speed, "active": act, "plans": j.Plans,
		"build": j.Build, "syncAfter": j.Sync, "backup": j.Backup, "steps": stepsCopy(j.Steps),
		"log": append([]LogLine{}, j.Log[logFrom:]...), "logTotal": j.allLines,
	}
}

func (j *Job) Cancel() {
	if j.cancel != nil {
		j.cancel()
	}
}

// ---------------------------------------------------------------------------
// Exclude patterns
// ---------------------------------------------------------------------------

type matcher struct {
	pats []pat
}
type pat struct {
	p       string
	dirOnly bool
	full    bool // match against relative path, not just name
}

func newMatcher(list []string) *matcher {
	m := &matcher{}
	for _, raw := range list {
		p := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		pt := pat{}
		if strings.HasSuffix(p, "/") {
			pt.dirOnly = true
			p = strings.TrimSuffix(p, "/")
		}
		p = strings.TrimPrefix(p, "/")
		pt.full = strings.Contains(p, "/")
		pt.p = strings.ToLower(p)
		m.pats = append(m.pats, pt)
	}
	return m
}

func (m *matcher) excluded(rel string, isDir bool) bool {
	rel = strings.ToLower(rel)
	name := path.Base(rel)
	for _, p := range m.pats {
		if p.dirOnly && !isDir {
			continue
		}
		target := name
		if p.full {
			target = rel
		}
		if ok, _ := path.Match(p.p, target); ok {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Running a job
// ---------------------------------------------------------------------------

type Runner struct {
	store    *Store
	mu       sync.Mutex
	jobs     map[string]*Job
	building string // card whose builds are running (one build at a time: shared project / output folders)
}

func stepsCopy(s []*Step) []Step {
	out := make([]Step, 0, len(s))
	for _, x := range s {
		out = append(out, *x)
	}
	return out
}

type StartOpts struct {
	Preview, Force bool
	Build          bool              // run the card's builds first
	Backup         bool              // zip backup of the card's backup pairs (nothing else)
	Notes          []string          // backup: note for each backup of the card (same order as Tab.Backups) → prefix_note_date.zip
	Only           []int             // backup: run only these backups (indexes into Tab.Backups); nil = all enabled
	Names          []string          // backup: own zip name per backup (same order as Tab.Backups), e.g. "fw-160" → fw-160.zip
	BuildInfo      map[string]string // build ID → "Info k verzi" for this run (overrides the build setting)
	Sync           bool              // synchronize (always true unless "only build")
	StopDropbox    bool              // stop Dropbox during the builds for this run (chosen in the Dropbox check)
}

func newRunner(s *Store) *Runner { return &Runner{store: s, jobs: map[string]*Job{}} }

func (r *Runner) Get(tabID string) *Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.jobs[tabID]
}

func (r *Runner) AnyRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, j := range r.jobs {
		if j.Phase != "done" {
			return true
		}
	}
	return false
}

func (r *Runner) Start(tabID string, o StartOpts) (*Job, error) {
	tab, err := r.store.TabCopy(tabID)
	if err != nil {
		return nil, err
	}
	if o.Backup {
		o.Preview, o.Build, o.Sync = false, false, false
	} else {
		if o.Preview {
			o.Build, o.Sync = false, true
		}
		if !o.Build {
			o.Sync = true
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &Job{TabID: tabID, Preview: o.Preview, Force: o.Force, Build: o.Build, Sync: o.Sync, Backup: o.Backup, stopDB: o.StopDropbox, Phase: "connecting",
		Started: time.Now(), cancel: cancel, active: map[int]string{}, lastT: time.Now(), Log: []LogLine{}, Steps: []*Step{}}
	if o.Backup {
		bp, err := prepareBackups(j, tab, o.Notes, o.Names, o.Only)
		if err != nil {
			cancel()
			return nil, err
		}
		j.backups = bp
		j.Phase = "scanning"
	}
	if o.Build {
		j.buildInfo = o.BuildInfo
		plans, err := r.prepareBuilds(j, tab)
		if err != nil {
			cancel()
			return nil, err
		}
		j.plans = plans
		j.Phase = "building"
		if o.Sync {
			j.Steps = append(j.Steps, &Step{Name: T("Synchronizace"), State: "pending"})
		}
	}
	r.mu.Lock()
	if old := r.jobs[tabID]; old != nil && old.Phase != "done" {
		r.mu.Unlock()
		cancel()
		return nil, errors.New(T("Synchronizace, build nebo záloha této karty už běží"))
	}
	if o.Build {
		if r.building != "" {
			name := r.building
			if t := r.store.tabName(r.building); t != "" {
				name = t
			}
			r.mu.Unlock()
			cancel()
			return nil, fmt.Errorf(T("Právě běží build karty „%s“ – počkej, až doběhne"), name)
		}
		r.building = tabID
	}
	r.jobs[tabID] = j
	r.mu.Unlock()

	r.store.mu.Lock()
	workers := r.store.cfg.Settings.Workers
	r.store.mu.Unlock()

	go r.run(ctx, j, tab, workers)
	return j, nil
}

func (r *Runner) run(ctx context.Context, j *Job, tab *Tab, workers int) {
	defer func() {
		if p := recover(); p != nil {
			buf := make([]byte, 4096)
			buf = buf[:runtime.Stack(buf, false)]
			j.logf("err", T("Vnitřní chyba: %v\n%s"), p, buf)
			r.mu.Lock()
			if r.building == tab.ID {
				r.building = ""
			}
			r.mu.Unlock()
			j.mu.Lock()
			j.Result = "failed"
			j.Phase = "done"
			j.Ended = time.Now()
			j.mu.Unlock()
		}
	}()
	if !j.Preview {
		lf, err := openLog(r.store.dir, tab.ID)
		if err == nil {
			j.logFile = lf
			defer lf.Close()
			fmt.Fprintf(lf, "\n===== %s  %s =====\n", time.Now().Format("2006-01-02 15:04:05"), tab.Name)
		}
	}
	res, msg := "ok", ""
	if j.Backup {
		res, msg = r.runBackups(ctx, j, tab)
		sum := &SyncSummary{At: j.Started, Result: res, Uploaded: int(atomic.LoadInt64(&j.FilesDone)),
			Bytes: atomic.LoadInt64(&j.BytesDone), Failed: int(atomic.LoadInt64(&j.FilesFail)),
			Seconds: time.Since(j.Started).Seconds(), Message: msg}
		r.store.mu.Lock()
		if t := r.store.tab(tab.ID); t != nil {
			t.LastBackup = sum
			_ = r.store.saveLocked()
		}
		r.store.mu.Unlock()
	}
	if j.Build {
		res, msg = r.runBuilds(ctx, j, j.plans)
		r.mu.Lock()
		if r.building == tab.ID {
			r.building = ""
		}
		r.mu.Unlock()
		bsum := &SyncSummary{At: j.Started, Result: res, Seconds: time.Since(j.Started).Seconds(), Message: msg}
		r.store.mu.Lock()
		if t := r.store.tab(tab.ID); t != nil {
			t.LastBuild = bsum
			_ = r.store.saveLocked()
		}
		r.store.mu.Unlock()
		if res == "ok" && j.Sync {
			j.logf("info", T("%s – pokračuji synchronizací"), msg)
			j.setStep(len(j.Steps)-1, "running", "")
			j.setPhase("connecting")
		}
	}
	if res == "ok" && j.Sync {
		j.synced = true
		res, msg = r.execute(ctx, j, tab, workers)
		if j.Build {
			st := map[string]string{"ok": "ok", "errors": "failed", "cancelled": "skipped", "failed": "failed"}[res]
			j.setStep(len(j.Steps)-1, st, "")
		}
	} else if j.Build && j.Sync {
		j.setStep(len(j.Steps)-1, "skipped", "")
	}
	if res == "ok" && j.Build && !j.Sync && tabCanSync(tab) {
		// only when the card could sync and the user chose "build only"; a card without a server says nothing about
		// uploading – its script may upload by itself
		msg = msg + T(" – nic se nenahrávalo (jen build)")
	}
	j.mu.Lock()
	j.Result = res
	j.Phase = "done"
	j.Ended = time.Now()
	j.mu.Unlock()
	if msg != "" {
		kind := "info"
		if res != "ok" {
			kind = "err"
		}
		j.logf(kind, "%s", msg)
	}
	if !j.Preview && j.synced {
		sum := &SyncSummary{At: j.Started, Result: res, Uploaded: int(atomic.LoadInt64(&j.FilesDone)),
			Bytes: atomic.LoadInt64(&j.BytesDone), Failed: int(atomic.LoadInt64(&j.FilesFail)),
			Seconds: j.Ended.Sub(j.Started).Seconds(), Message: msg}
		r.store.mu.Lock()
		if t := r.store.tab(tab.ID); t != nil {
			t.LastSync = sum
			_ = r.store.saveLocked()
		}
		r.store.mu.Unlock()
	}
}

func openLog(dataDir, tabID string) (*os.File, error) {
	p := filepath.Join(dataDir, "logs", tabID+".log")
	if fi, err := os.Stat(p); err == nil && fi.Size() > 2<<20 {
		_ = os.Rename(p, p+".old")
	}
	return os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

// connect opens a connection and stores a newly seen SFTP host key.
func (r *Runner) connect(ctx context.Context, tab *Tab) (Remote, error) {
	var newKey string
	rem, err := dial(ctx, tab.Conn, &newKey)
	if err != nil {
		return nil, err
	}
	if newKey != "" {
		tab.Conn.HostKey = newKey
		r.store.mu.Lock()
		if t := r.store.tab(tab.ID); t != nil && t.Conn.HostKey == "" {
			t.Conn.HostKey = newKey
			_ = r.store.saveLocked()
		}
		r.store.mu.Unlock()
	}
	return rem, nil
}

func resolveRemote(home, p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" {
		return home
	}
	if !strings.HasPrefix(p, "/") {
		p = path.Join(home, p)
	}
	return path.Clean(p)
}

func (r *Runner) execute(ctx context.Context, j *Job, tab *Tab, workers int) (string, string) {
	pairs := []*Pair{}
	for _, p := range tab.Pairs {
		if p.Enabled && strings.TrimSpace(p.Local) != "" {
			pairs = append(pairs, p)
		}
	}
	if len(pairs) == 0 {
		return "failed", T("Karta nemá žádný zapnutý pár složek")
	}
	j.mu.Lock()
	j.Server = fmt.Sprintf("%s@%s", tab.Conn.User, tab.Conn.Host)
	j.mu.Unlock()

	if j.Preview {
		j.logf("info", "%s", T("Náhled – nic se nenahrává"))
	}
	j.logf("info", T("Připojuji k %s …"), tab.Conn.Host)
	main, err := r.connect(ctx, tab)
	if err != nil {
		return "failed", err.Error()
	}
	defer func() { main.Close() }()
	j.logf("info", T("Připojeno (%s), domovská složka %s"), main.Info(), main.Home())

	state := loadTabState(r.store.dir, tab.ID)
	defer state.save()

	// --- scan -----------------------------------------------------------
	j.setPhase("scanning")
	m := newMatcher(tab.Excludes)
	plans := []*PairPlan{}
	for _, p := range pairs {
		if ctx.Err() != nil {
			return "cancelled", T("Přerušeno")
		}
		rroot := resolveRemote(main.Home(), p.Remote)
		plan := &PairPlan{PairID: p.ID, Local: p.Local, Remote: rroot, Dirs: []string{}, Files: []*UploadItem{}}
		plans = append(plans, plan)
		sig := strings.ToLower(tab.Conn.Host) + "|" + tab.Conn.User + "|" + rroot
		ps := state.pair(p.ID, sig)
		j.logf("info", T("Porovnávám %s → %s"), p.Local, rroot)
		pm := &matcher{pats: append(append([]pat{}, m.pats...), newMatcher(p.Excludes).pats...)}
		if err := r.scanPair(ctx, j, main, pm, p, rroot, ps, state, plan); err != nil {
			if ctx.Err() != nil {
				return "cancelled", T("Přerušeno")
			}
			plan.Error = err.Error()
			j.logf("err", "%s: %v", p.Local, err)
			continue
		}
		j.logf("info", T("  nové/změněné: %d, beze změny: %d, nových složek: %d, vyloučeno: %d"),
			len(plan.Files), plan.Same, len(plan.Dirs), plan.Excluded)
	}
	// drop state of pairs that no longer exist
	state.mu.Lock()
	for id := range state.Pairs {
		found := false
		for _, p := range tab.Pairs {
			if p.ID == id {
				found = true
			}
		}
		if !found {
			delete(state.Pairs, id)
			state.dirty = true
		}
	}
	state.mu.Unlock()

	var items []*UploadItem
	var total int64
	nDirs := 0
	pairErrs := 0
	for _, pl := range plans {
		items = append(items, pl.Files...)
		nDirs += len(pl.Dirs)
		if pl.Error != "" {
			pairErrs++
		}
		for _, f := range pl.Files {
			total += f.Size
		}
	}
	j.mu.Lock()
	j.FilesTotal = len(items)
	j.BytesTotal = total
	j.mu.Unlock()

	if j.Preview {
		j.mu.Lock()
		j.Plans = plans
		j.mu.Unlock()
		if pairErrs > 0 {
			return "errors", fmt.Sprintf(T("Náhled hotový s chybami: %d souborů k nahrání"), len(items))
		}
		return "ok", fmt.Sprintf(T("Náhled hotový: %d souborů k nahrání (%s), %d nových složek"), len(items), humanBytes(total), nDirs)
	}

	if len(items) == 0 && nDirs == 0 {
		if pairErrs > 0 {
			return "errors", T("Hotovo s chybami – viz log")
		}
		return "ok", T("Vše je aktuální, nic nebylo potřeba nahrát")
	}

	// --- create directories (sequential, parents first) -------------------
	j.setPhase("uploading")
	for _, pl := range plans {
		for _, d := range pl.Dirs {
			if ctx.Err() != nil {
				return "cancelled", T("Přerušeno")
			}
			if err := main.Mkdir(d); err != nil {
				// maybe it exists (race / listing quirk) – check by listing the parent
				if !main.IsDir(d) {
					j.logf("err", T("Nelze vytvořit složku %s: %v"), d, err)
					continue
				}
			} else {
				atomic.AddInt64(&j.DirsMade, 1)
				j.logf("up", T("složka %s"), d)
			}
		}
	}

	// --- upload files (parallel) ----------------------------------------
	if workers > len(items) {
		workers = len(items)
	}
	if workers < 1 {
		workers = 1
	}
	queue := make(chan *UploadItem)
	var wg sync.WaitGroup
	saveTick := time.NewTicker(5 * time.Second)
	defer saveTick.Stop()
	stopSaver := make(chan struct{})
	go func() {
		for {
			select {
			case <-saveTick.C:
				state.save()
			case <-stopSaver:
				return
			}
		}
	}()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		var conn Remote
		if w == 0 {
			conn = main
		}
		go func(w int, conn Remote) {
			defer wg.Done()
			r.worker(ctx, j, tab, w, conn, main == conn, queue, state)
		}(w, conn)
	}
feed:
	for _, it := range items {
		select {
		case queue <- it:
		case <-ctx.Done():
			break feed
		}
	}
	close(queue)
	wg.Wait()
	close(stopSaver)

	done := atomic.LoadInt64(&j.FilesDone)
	fail := atomic.LoadInt64(&j.FilesFail)
	el := time.Since(j.Started).Seconds()
	summary := fmt.Sprintf(T("Nahráno %d souborů (%s) za %s"), done, humanBytes(atomic.LoadInt64(&j.BytesDone)), humanDur(el))
	if ctx.Err() != nil {
		return "cancelled", T("Přerušeno. ") + summary
	}
	if fail > 0 || pairErrs > 0 {
		return "errors", fmt.Sprintf("%s, %d chyb", summary, fail+int64(pairErrs))
	}
	return "ok", summary
}

func (r *Runner) worker(ctx context.Context, j *Job, tab *Tab, w int, conn Remote, shared bool, queue <-chan *UploadItem, state *TabState) {
	defer func() {
		if conn != nil && !shared {
			conn.Close()
		}
		j.setActive(w, "")
	}()
	for it := range queue {
		if ctx.Err() != nil {
			return
		}
		var lastErr error
		for attempt := 0; attempt < 3; attempt++ {
			if ctx.Err() != nil {
				return
			}
			if conn == nil {
				c, err := r.connect(ctx, tab)
				if err != nil {
					lastErr = err
					time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
					continue
				}
				conn = c
				shared = false
			}
			j.setActive(w, it.Rel)
			var sent int64
			err := r.uploadOne(ctx, conn, tab, it, func(n int64) {
				sent += n
				atomic.AddInt64(&j.BytesDone, n)
			})
			if err == nil {
				lastErr = nil
				break
			}
			atomic.AddInt64(&j.BytesDone, -sent)
			lastErr = err
			if ctx.Err() != nil {
				return
			}
			if isLocalErr(err) {
				break // no point retrying
			}
			// connection problem? reconnect and retry
			if !shared {
				conn.Close()
			}
			conn = nil
			j.logf("warn", T("%s: %v – zkouším znovu"), it.Rel, err)
		}
		j.setActive(w, "")
		if lastErr != nil {
			atomic.AddInt64(&j.FilesFail, 1)
			j.logf("err", "%s: %v", it.Rel, lastErr)
			continue
		}
		atomic.AddInt64(&j.FilesDone, 1)
		state.set(it.ps, it.Rel, &FileRec{S: it.Size, M: it.mtime})
		j.logf("up", "%s (%s)", it.Rel, humanBytes(it.Size))
	}
}

type localErr struct{ error }

func isLocalErr(err error) bool {
	var le *localErr
	return errors.As(err, &le)
}

func (r *Runner) uploadOne(ctx context.Context, conn Remote, tab *Tab, it *UploadItem, prog func(int64)) error {
	if _, err := os.Stat(it.Local); err != nil {
		return &localErr{fmt.Errorf(T("soubor v PC nejde otevřít: %v"), err)}
	}
	if !tab.SafeUpload {
		if err := conn.Upload(ctx, it.Local, it.Remote, prog); err != nil {
			return err
		}
		return verifySize(conn, it.Remote, it.Size)
	}
	tmp := path.Join(path.Dir(it.Remote), ".~lz_"+path.Base(it.Remote))
	if err := conn.Upload(ctx, it.Local, tmp, prog); err != nil {
		_ = conn.Delete(tmp)
		return err
	}
	if err := verifySize(conn, tmp, it.Size); err != nil {
		_ = conn.Delete(tmp)
		return err
	}
	if err := conn.Rename(tmp, it.Remote); err != nil {
		_ = conn.Delete(tmp)
		return fmt.Errorf(T("přejmenování dočasného souboru selhalo: %v"), err)
	}
	return nil
}

// verifySize checks the uploaded size – a truncated file must never count as uploaded.
func verifySize(conn Remote, remote string, want int64) error {
	got := conn.Size(remote)
	if got >= 0 && got != want {
		return fmt.Errorf(T("na serveru má soubor %d B místo %d B (přenos nedokončen)"), got, want)
	}
	return nil
}

// scanPair walks the local folder and the matching remote folders and fills the plan.
func (r *Runner) scanPair(ctx context.Context, j *Job, rem Remote, m *matcher, p *Pair, rroot string,
	ps *PairState, state *TabState, plan *PairPlan) error {

	lroot := filepath.Clean(strings.TrimSpace(p.Local))
	st, err := os.Stat(lroot)
	if err != nil || !st.IsDir() {
		return errors.New(T("složka v PC neexistuje"))
	}

	// make sure the remote root exists
	rootExists := true
	if !rem.IsDir(rroot) {
		rootExists = false
		// create missing path components
		parts := strings.Split(strings.Trim(rroot, "/"), "/")
		cur := ""
		for _, part := range parts {
			cur += "/" + part
			if rem.IsDir(cur) {
				continue
			}
			plan.Dirs = append(plan.Dirs, cur)
		}
	}
	precise := rem.PreciseTime()

	type dirJob struct {
		rel    string
		exists bool
	}
	stack := []dirJob{{"", rootExists}}
	for len(stack) > 0 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		d := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		ldir := filepath.Join(lroot, filepath.FromSlash(d.rel))
		rdir := rroot
		if d.rel != "" {
			rdir = path.Join(rroot, d.rel)
		}
		entries, err := os.ReadDir(ldir)
		if err != nil {
			j.logf("err", T("Nelze číst %s: %v"), ldir, err)
			continue
		}
		remote := map[string]RemoteEntry{}
		if d.exists {
			list, err := rem.List(rdir)
			if err != nil {
				return fmt.Errorf(T("výpis %s na serveru selhal: %v"), rdir, err)
			}
			for _, e := range list {
				remote[e.Name] = e
			}
		}
		var subdirs []dirJob
		for _, e := range entries {
			name := e.Name()
			rel := name
			if d.rel != "" {
				rel = d.rel + "/" + name
			}
			typ := e.Type()
			if typ&fs.ModeSymlink != 0 || typ&fs.ModeIrregular != 0 {
				// follow symlinks / junctions only if they point to a regular file or dir
				fi, err := os.Stat(filepath.Join(ldir, name))
				if err != nil {
					continue
				}
				typ = fi.Mode().Type()
			}
			isDir := typ.IsDir()
			if !isDir && !typ.IsRegular() {
				continue
			}
			if m.excluded(rel, isDir) {
				plan.Excluded++
				continue
			}
			re, has := remote[name]
			if isDir {
				exists := has && (re.IsDir || re.Size == -1)
				if has && !exists {
					j.logf("warn", T("Na serveru je soubor %s, ale v PC je to složka – přeskakuji"), rel)
					continue
				}
				if !exists {
					plan.Dirs = append(plan.Dirs, path.Join(rroot, rel))
				}
				subdirs = append(subdirs, dirJob{rel, exists})
				continue
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			if fi.Mode()&fs.ModeSymlink != 0 {
				if fi, err = os.Stat(filepath.Join(ldir, name)); err != nil {
					continue
				}
			}
			atomic.AddInt64(&j.ScanFiles, 1)
			size, mt := fi.Size(), fi.ModTime().Unix()
			item := &UploadItem{PairID: p.ID, Local: filepath.Join(ldir, name), Remote: path.Join(rroot, rel),
				Rel: rel, Size: size, mtime: mt, ps: ps}
			if has && re.IsDir {
				j.logf("warn", T("Na serveru je složka %s, ale v PC je to soubor – přeskakuji"), rel)
				continue
			}
			reason := decide(j.Force, has, re, size, mt, precise, state.get(ps, rel))
			if reason == "" {
				plan.Same++
				if state.get(ps, rel) == nil || state.get(ps, rel).M != mt {
					state.set(ps, rel, &FileRec{S: size, M: mt})
				}
				continue
			}
			item.Reason = reason
			plan.Files = append(plan.Files, item)
		}
		// keep a natural order: process subdirectories alphabetically
		sort.Slice(subdirs, func(a, b int) bool { return subdirs[a].rel > subdirs[b].rel })
		stack = append(stack, subdirs...)
	}
	sort.SliceStable(plan.Files, func(a, b int) bool { return plan.Files[a].Rel < plan.Files[b].Rel })
	return nil
}

// decide returns "" when the file is up to date, otherwise the reason to upload it.
func decide(force, has bool, re RemoteEntry, size, mtime int64, precise bool, rec *FileRec) string {
	if force {
		return "forced"
	}
	if !has {
		return "new"
	}
	if re.Size >= 0 && re.Size != size {
		return "changed"
	}
	if rec != nil {
		if rec.S == size && rec.M == mtime {
			return "" // uploaded by us and not touched since
		}
		return "changed" // same size, but modified since our last upload
	}
	// first time we see this file: compare times if the server gives exact ones
	if precise && !re.Mtime.IsZero() && mtime > re.Mtime.Unix()+2 {
		return "changed"
	}
	return ""
}

func humanBytes(b int64) string {
	f := float64(b)
	switch {
	case f >= 1<<30:
		return fmt.Sprintf("%.2f GB", f/(1<<30))
	case f >= 1<<20:
		return fmt.Sprintf("%.1f MB", f/(1<<20))
	case f >= 1<<10:
		return fmt.Sprintf("%.0f kB", f/(1<<10))
	}
	return fmt.Sprintf("%d B", b)
}

func humanDur(s float64) string {
	if s < 60 {
		return fmt.Sprintf("%.1f s", s)
	}
	return fmt.Sprintf("%d min %d s", int(s)/60, int(s)%60)
}

// tabCanSync: the card has a server and at least one enabled folder pair (same as syncReady in the UI)
func tabCanSync(t *Tab) bool {
	if t == nil || strings.TrimSpace(t.Conn.Host) == "" {
		return false
	}
	for _, p := range t.Pairs {
		if p != nil && p.Enabled && strings.TrimSpace(p.Local) != "" {
			return true
		}
	}
	return false
}
