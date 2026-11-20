package main

// GitHub: a card lists local git repositories; the "GitHub" button walks them one by one,
// shows changed files + diff and commits / pushes the selected files with git.exe
// (Git for Windows, credentials from Git Credential Manager – Leonzilla stores nothing).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

type GitRepo struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
}

type GitFile struct {
	Path string `json:"path"`
	Orig string `json:"orig,omitempty"` // renamed from
	XY   string `json:"xy"`
	Kind string `json:"kind"` // new | modified | deleted | renamed | conflict
}

type GitStatus struct {
	OK        bool      `json:"ok"`
	Error     string    `json:"error,omitempty"`
	Root      string    `json:"root"`
	Name      string    `json:"name"`
	Branch    string    `json:"branch"`
	Detached  bool      `json:"detached"`
	Upstream  string    `json:"upstream"`
	Remote    string    `json:"remote"` // origin URL without credentials
	Ahead     int       `json:"ahead"`
	Behind    int       `json:"behind"`
	NoCommits bool      `json:"noCommits"`
	Busy      string    `json:"busy,omitempty"` // merge / rebase in progress
	Files     []GitFile `json:"files"`
	Truncated bool      `json:"truncated"`
	FetchErr  string    `json:"fetchError,omitempty"`
}

type GitStep struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	Out  string `json:"out"`
}

var (
	gitOnce sync.Once
	gitExe  string
)

// gitPath finds git.exe: PATH, Git for Windows, GitHub Desktop's bundled git.
func gitPath() string {
	gitOnce.Do(func() {
		if p, err := exec.LookPath("git"); err == nil {
			gitExe = p
			return
		}
		if runtime.GOOS != "windows" {
			return
		}
		var c []string
		for _, env := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
			if d := os.Getenv(env); d != "" {
				c = append(c, filepath.Join(d, "Git", "cmd", "git.exe"))
			}
		}
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			c = append(c, filepath.Join(d, "Programs", "Git", "cmd", "git.exe"))
			m, _ := filepath.Glob(filepath.Join(d, "GitHubDesktop", "app-*", "resources", "app", "git", "cmd", "git.exe"))
			sort.Sort(sort.Reverse(sort.StringSlice(m)))
			c = append(c, m...)
		}
		for _, p := range c {
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				gitExe = p
				return
			}
		}
	})
	return gitExe
}

// runGit runs git in dir. stdin may be nil.
func runGit(ctx context.Context, dir string, stdin []byte, args ...string) (string, error) {
	return runGitEnv(ctx, dir, stdin, nil, args...)
}

// runGitEnv = runGit with extra environment variables (e.g. GIT_INDEX_FILE).
func runGitEnv(ctx context.Context, dir string, stdin []byte, env []string, args ...string) (string, error) {
	exe := gitPath()
	if exe == "" {
		return "", errors.New(T("Git není nainstalovaný (Git for Windows: https://git-scm.com)"))
	}
	full := append([]string{"--literal-pathspecs", "-c", "core.quotepath=off", "-c", "color.ui=never", "-c", "core.pager=", "-C", dir}, args...)
	cmd := exec.CommandContext(ctx, exe, full...)
	prepareCmd(cmd)
	cmd.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=", "GCM_INTERACTIVE=auto"), env...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	text := out.String()
	if err != nil {
		msg := strings.TrimSpace(decodeLines(errb.Bytes()))
		if msg == "" {
			msg = strings.TrimSpace(decodeLines(out.Bytes()))
		}
		if ctx.Err() == context.DeadlineExceeded {
			msg = T("vypršel čas (") + msg + ")"
		}
		if msg == "" {
			msg = err.Error()
		}
		return text, errors.New(msg)
	}
	if s := strings.TrimSpace(errb.String()); s != "" && len(text) == 0 {
		// push / pull report progress on stderr
		return decodeLines(errb.Bytes()), nil
	}
	return text, nil
}

// decodeLines decodes line by line: UTF-8 lines stay, other lines are Windows-1250 (Delphi sources).
func decodeLines(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	dec := charmap.Windows1250.NewDecoder()
	var sb strings.Builder
	for _, ln := range bytes.SplitAfter(b, []byte("\n")) {
		if utf8.Valid(ln) {
			sb.Write(ln)
		} else if s, err := dec.Bytes(ln); err == nil {
			sb.Write(s)
		} else {
			sb.Write(ln)
		}
	}
	return sb.String()
}

func cleanRemote(u string) string {
	u = strings.TrimSpace(u)
	if p, err := url.Parse(u); err == nil && p.User != nil {
		p.User = nil
		return p.String()
	}
	return u
}

// gitStatus reads branch info and changed files. fetch = ask the server first (for "behind").
func gitStatus(path string, fetch bool) *GitStatus {
	s := &GitStatus{Files: []GitFile{}}
	path = strings.TrimSpace(path)
	if path == "" {
		s.Error = T("Složka není vybraná")
		return s
	}
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		s.Error = T("Složka neexistuje")
		return s
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root, err := runGit(ctx, path, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		if strings.Contains(err.Error(), "not a git repository") || strings.Contains(err.Error(), "není") {
			s.Error = T("Složka není git repozitář")
		} else {
			s.Error = err.Error()
		}
		return s
	}
	s.Root = filepath.FromSlash(strings.TrimSpace(root))
	s.Name = filepath.Base(s.Root)
	if r, err := runGit(ctx, path, nil, "remote", "get-url", "origin"); err == nil {
		s.Remote = cleanRemote(r)
	} else if r, err := runGit(ctx, path, nil, "remote"); err == nil && strings.TrimSpace(r) != "" {
		first := strings.Fields(r)[0]
		if u, err := runGit(ctx, path, nil, "remote", "get-url", first); err == nil {
			s.Remote = cleanRemote(u)
		}
	}
	if fetch && s.Remote != "" {
		if _, err := runGit(ctx, path, nil, "fetch", "--quiet"); err != nil {
			s.FetchErr = err.Error()
		}
	}
	out, err := runGit(ctx, path, nil, "status", "--porcelain=v2", "--branch", "-z", "-uall")
	if err != nil {
		s.Error = err.Error()
		return s
	}
	parseStatusV2(out, s)
	gd := ""
	if g, err := runGit(ctx, path, nil, "rev-parse", "--absolute-git-dir"); err == nil {
		gd = filepath.FromSlash(strings.TrimSpace(g))
	}
	for f, what := range map[string]string{"MERGE_HEAD": "merge", "rebase-merge": "rebase", "rebase-apply": "rebase", "CHERRY_PICK_HEAD": "cherry-pick"} {
		if gd != "" {
			if _, err := os.Stat(filepath.Join(gd, f)); err == nil {
				s.Busy = what
			}
		}
	}
	s.OK = true
	return s
}

const gitMaxFiles = 3000

func parseStatusV2(out string, s *GitStatus) {
	toks := strings.Split(out, "\x00")
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t == "" {
			continue
		}
		switch {
		case strings.HasPrefix(t, "# branch.oid "):
			s.NoCommits = strings.TrimPrefix(t, "# branch.oid ") == "(initial)"
		case strings.HasPrefix(t, "# branch.head "):
			s.Branch = strings.TrimPrefix(t, "# branch.head ")
			s.Detached = s.Branch == "(detached)"
		case strings.HasPrefix(t, "# branch.upstream "):
			s.Upstream = strings.TrimPrefix(t, "# branch.upstream ")
		case strings.HasPrefix(t, "# branch.ab "):
			f := strings.Fields(strings.TrimPrefix(t, "# branch.ab "))
			if len(f) == 2 {
				s.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f[0], "+"))
				s.Behind, _ = strconv.Atoi(strings.TrimPrefix(f[1], "-"))
			}
		case strings.HasPrefix(t, "1 "):
			p := strings.SplitN(t, " ", 9)
			if len(p) == 9 {
				s.add(GitFile{Path: p[8], XY: p[1]})
			}
		case strings.HasPrefix(t, "2 "):
			p := strings.SplitN(t, " ", 10)
			orig := ""
			if i+1 < len(toks) {
				orig = toks[i+1]
				i++
			}
			if len(p) == 10 {
				s.add(GitFile{Path: p[9], Orig: orig, XY: p[1]})
			}
		case strings.HasPrefix(t, "u "):
			p := strings.SplitN(t, " ", 11)
			if len(p) == 11 {
				s.add(GitFile{Path: p[10], XY: p[1]})
			}
		case strings.HasPrefix(t, "? "):
			s.add(GitFile{Path: t[2:], XY: "??"})
		}
	}
}

func (s *GitStatus) add(f GitFile) {
	if len(s.Files) >= gitMaxFiles {
		s.Truncated = true
		return
	}
	x, y := f.XY[0], f.XY[1]
	switch {
	case f.XY == "??" || x == 'A' || y == 'A':
		f.Kind = "new"
	case f.XY == "UU" || f.XY == "AA" || f.XY == "DD" || x == 'U' || y == 'U':
		f.Kind = "conflict"
	case x == 'R' || x == 'C':
		f.Kind = "renamed"
	case x == 'D' || y == 'D':
		f.Kind = "deleted"
	default:
		f.Kind = "modified"
	}
	s.Files = append(s.Files, f)
}

// gitDiff returns the change of one file against HEAD (untracked = whole content).
func gitDiff(repo string, f GitFile) (text string, binary, truncated bool, err error) {
	const max = 300 << 10
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if f.Kind == "new" && f.XY == "??" {
		b, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(f.Path)))
		if err != nil {
			return "", false, false, err
		}
		if bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 {
			return "", true, false, nil
		}
		if len(b) > max {
			b, truncated = b[:max], true
		}
		s := decodeLines(bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF}))
		s = strings.ReplaceAll(s, "\r\n", "\n")
		lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
		var sb strings.Builder
		fmt.Fprintf(&sb, T("@@ nový soubor, %d řádků @@\n"), len(lines))
		for _, l := range lines {
			sb.WriteString("+" + l + "\n")
		}
		return sb.String(), false, truncated, nil
	}
	args := []string{"diff", "--no-color", "--no-ext-diff", "-M"}
	if _, herr := runGit(ctx, repo, nil, "rev-parse", "--verify", "-q", "HEAD"); herr != nil {
		args = append(args, "--cached")
	} else {
		args = append(args, "HEAD")
	}
	args = append(args, "--")
	if f.Orig != "" {
		args = append(args, f.Orig)
	}
	args = append(args, f.Path)
	out, err := runGit(ctx, repo, nil, args...)
	if err != nil {
		return "", false, false, err
	}
	if strings.Contains(out, "\nBinary files ") || strings.HasPrefix(out, "Binary files ") {
		return "", true, false, nil
	}
	if len(out) > max {
		out, truncated = out[:max], true
	}
	return strings.ReplaceAll(decodeLines([]byte(out)), "\r\n", "\n"), false, truncated, nil
}

type gitCommitReq struct {
	Tab     string    `json:"tab"`
	Path    string    `json:"path"`
	Files   []GitFile `json:"files"`
	Message string    `json:"message"`
	Details string    `json:"details"`
	Commit  bool      `json:"commit"` // false = only push existing commits
	Push    bool      `json:"push"`
	Pull    bool      `json:"pull"`
}

type gitCommitRes struct {
	OK     bool       `json:"ok"`
	Error  string     `json:"error,omitempty"`
	Hash   string     `json:"hash,omitempty"`
	Pushed bool       `json:"pushed"`
	Steps  []*GitStep `json:"steps"`
	Status *GitStatus `json:"status"`
}

// gitCommit stages + commits exactly the chosen files, optionally pulls (rebase) and pushes.
func gitCommit(req gitCommitReq, logf func(kind, text string)) *gitCommitRes {
	res := &gitCommitRes{Steps: []*GitStep{}}
	repo := strings.TrimSpace(req.Path)
	var stepEnv []string // extra environment of the next steps (commit from a temporary index)
	step := func(name string, timeout time.Duration, stdin []byte, args ...string) (string, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		out, err := runGitEnv(ctx, repo, stdin, stepEnv, args...)
		st := &GitStep{Name: name, OK: err == nil, Out: tidyGitOut(strings.TrimSpace(out))}
		if err != nil {
			st.Out = tidyGitOut(strings.TrimSpace(strings.TrimSpace(out) + "\n" + err.Error()))
		}
		res.Steps = append(res.Steps, st)
		k := "ok"
		if err != nil {
			k = "err"
		}
		logf(k, name+": "+firstLine(st.Out))
		return out, err == nil
	}
	fail := func(msg string) *gitCommitRes {
		res.Error = msg
		res.Status = gitStatus(repo, false)
		return res
	}
	cur := gitStatus(repo, false)
	if !cur.OK {
		return fail(cur.Error)
	}
	if req.Commit {
		if strings.TrimSpace(req.Message) == "" {
			return fail(T("Chybí popis commitu"))
		}
		if len(req.Files) == 0 {
			return fail(T("Není vybraný žádný soubor"))
		}
		if cur.Busy != "" {
			return fail(T("V repozitáři právě probíhá ") + cur.Busy + T(" – dokonči ho ručně"))
		}
		// git add gets only files with a change in the working copy (Y != '.', new "??"). A change that is only in the
		// index (e.g. "D." after git rm --cached of an ignored file, or a deletion already staged by git add -A) goes
		// to the commit as it is – git add would fail on it ("paths are ignored", "pathspec did not match any files").
		// The state comes from git now, not from the window (the file list may be minutes old).
		now := map[string]string{}
		for _, f := range cur.Files {
			now[f.Path] = f.XY
		}
		var spec, addSpec bytes.Buffer
		nAdd := 0
		for _, f := range req.Files {
			spec.WriteString(f.Path + "\x00")
			if f.Orig != "" {
				spec.WriteString(f.Orig + "\x00") // commit the removal of the old name too
			}
			xy, known := now[f.Path]
			if !known {
				// not in the list (more than gitMaxFiles changes) – an existing file can always be added
				if _, err := os.Stat(filepath.Join(cur.Root, filepath.FromSlash(f.Path))); err != nil || !cur.Truncated {
					continue
				}
				xy = " M"
			}
			if xy == "??" || (len(xy) == 2 && xy[1] != '.' && xy[1] != ' ') {
				addSpec.WriteString(f.Path + "\x00")
				nAdd++
			}
		}
		if nAdd > 0 {
			if _, ok := step(T("Přidání souborů"), 2*time.Minute, addSpec.Bytes(), "add", "-A", "--pathspec-from-file=-", "--pathspec-file-nul"); !ok {
				return fail(T("git add se nepovedl"))
			}
		}
		msg := strings.TrimSpace(req.Message)
		if d := strings.TrimSpace(req.Details); d != "" {
			msg += "\n\n" + d
		}
		mf, err := os.CreateTemp("", "leonzilla-commit-*.txt")
		if err != nil {
			return fail(err.Error())
		}
		mf.WriteString(msg + "\n")
		mf.Close()
		defer os.Remove(mf.Name())
		// What goes into the commit = the index for the chosen files. "git commit -- <paths>" can't be used: it takes the
		// files from the working copy, so a file removed with git rm --cached (still on the disk) would be committed
		// again. When the index holds no other staged change, a plain commit is exact; otherwise the commit is made
		// from a temporary index = HEAD + the chosen files from the index, and other staged changes stay staged.
		chosen := map[string]bool{}
		for _, p := range strings.Split(strings.TrimRight(spec.String(), "\x00"), "\x00") {
			chosen[p] = true
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		staged, err := runGit(ctx, repo, nil, "diff", "--cached", "--name-only", "-z", "--no-renames")
		cancel()
		if err != nil && !cur.NoCommits {
			return fail(err.Error())
		}
		others := false
		for _, p := range strings.Split(staged, "\x00") {
			if p != "" && !chosen[p] {
				others = true
				break
			}
		}
		if cur.NoCommits {
			others = false // the first commit: everything that is staged
		}
		if others {
			tmp, err := gitTempIndex(repo, chosen)
			if err != nil {
				return fail(T("Commit se nepovedl") + ": " + err.Error())
			}
			defer os.Remove(tmp)
			stepEnv = []string{"GIT_INDEX_FILE=" + tmp}
		}
		_, ok := step("Commit", 2*time.Minute, nil, "commit", "-F", mf.Name())
		stepEnv = nil
		if !ok {
			return fail(T("Commit se nepovedl"))
		}
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		h, _ := runGit(ctx, repo, nil, "rev-parse", "--short", "HEAD")
		cancel()
		res.Hash = strings.TrimSpace(h)
	}
	if req.Push || req.Pull {
		st := gitStatus(repo, false)
		if st.Remote == "" {
			if req.Push {
				return fail(T("Repozitář nemá nastavený vzdálený server (origin) – commit zůstal jen v PC"))
			}
		} else {
			if req.Pull && st.Upstream != "" {
				if _, ok := step(T("Stažení změn (pull --rebase)"), 5*time.Minute, nil, "pull", "--rebase", "--autostash"); !ok {
					after := gitStatus(repo, false)
					if after.Busy == "rebase" {
						step(T("Vrácení pullu (rebase --abort)"), time.Minute, nil, "rebase", "--abort")
					}
					return fail(T("Stažení změn ze serveru se nepovedlo (konflikt?) – commit zůstal v PC, vyřeš ručně"))
				}
			}
			if req.Push {
				args := []string{"push"}
				if st.Upstream == "" {
					args = []string{"push", "-u", "origin", "HEAD"}
				}
				if _, ok := step("Push", 5*time.Minute, nil, args...); !ok {
					m := T("Push se nepovedl – commit zůstal v PC")
					last := res.Steps[len(res.Steps)-1].Out
					if strings.Contains(last, "rejected") || strings.Contains(last, "fetch first") || strings.Contains(last, "non-fast-forward") {
						m += T(". Na serveru jsou novější změny – zapni v nastavení karty „Před pushem stáhnout změny (pull)“, nebo je stáhni ručně.")
					}
					return fail(m)
				}
				res.Pushed = true
			}
		}
	}
	res.OK = true
	res.Status = gitStatus(repo, false)
	return res
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i] + " …"
	}
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// appendTabLog writes git actions into the card's log file (same file as sync).
func appendTabLog(dataDir, tabID, kind, text string) {
	f, err := openLog(dataDir, tabID)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s  %-4s %s\n", time.Now().Format("2006-01-02 15:04:05"), kind, text)
}

// tidyGitOut folds git's line-ending warnings ("warning: in the working copy of '…', LF will be replaced by CRLF …" –
// one per file, hundreds in a big commit) into one line, so the real error message at the end stays visible.
func tidyGitOut(s string) string {
	var keep []string
	n := 0
	for _, l := range strings.Split(s, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "warning: in the working copy of ") && (strings.Contains(t, "will be replaced by") || strings.Contains(t, "nahrazen")) {
			n++
			continue
		}
		keep = append(keep, l)
	}
	if n == 0 {
		return s
	}
	note := fmt.Sprintf(T("(%d× upozornění na konce řádků LF/CRLF – nejde o chybu, vynecháno)"), n)
	return strings.TrimSpace(strings.TrimSpace(strings.Join(keep, "\n")) + "\n" + note) // the real message first
}

// gitTempIndex makes a temporary index = HEAD + the entries of the chosen paths from the real index (a chosen path
// missing in the index = removed). Committing from it records exactly the chosen files as they are staged.
func gitTempIndex(repo string, chosen map[string]bool) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ls, err := runGit(ctx, repo, nil, "ls-files", "-s", "-z")
	if err != nil {
		return "", err
	}
	var info bytes.Buffer
	seen := map[string]bool{}
	for _, e := range strings.Split(ls, "\x00") {
		tab := strings.IndexByte(e, '\t')
		if tab < 0 {
			continue
		}
		if p := e[tab+1:]; chosen[p] && !seen[p] {
			seen[p] = true
			info.WriteString(e + "\x00") // "mode sha stage<TAB>path"
		}
	}
	for p := range chosen {
		if !seen[p] {
			info.WriteString("0 0000000000000000000000000000000000000000\t" + p + "\x00") // removed
		}
	}
	f, err := os.CreateTemp("", "leonzilla-index-*")
	if err != nil {
		return "", err
	}
	f.Close()
	os.Remove(f.Name()) // git creates it
	env := []string{"GIT_INDEX_FILE=" + f.Name()}
	if _, err := runGitEnv(ctx, repo, nil, env, "read-tree", "HEAD"); err != nil {
		return "", err
	}
	if _, err := runGitEnv(ctx, repo, info.Bytes(), env, "update-index", "-z", "--index-info"); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
