package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// GitHub backup (gitCommit) on throw-away repositories with a bare "server": every case has to commit and push,
// and nothing may stay behind uncommitted or unpushed.

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeT(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRepoT: a repository with a few committed files, pushed to a bare remote.
func newRepoT(t *testing.T, crlf bool) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	remote, repo := filepath.Join(root, "remote.git"), filepath.Join(root, "repo")
	gitT(t, root, "init", "-q", "--bare", "-b", "main", remote)
	gitT(t, root, "init", "-q", "-b", "main", repo)
	gitT(t, repo, "config", "user.name", "Test")
	gitT(t, repo, "config", "user.email", "test@example.com")
	if crlf {
		gitT(t, repo, "config", "core.autocrlf", "true")
	}
	for _, f := range []string{"a.txt", "c.txt", "d.bin", "e.txt", "f.txt", "m.txt", "sub/s.txt"} {
		writeT(t, filepath.Join(repo, f), "first "+f+"\n")
	}
	writeT(t, filepath.Join(repo, ".gitignore"), "*.tmp\n")
	gitT(t, repo, "add", "-A")
	gitT(t, repo, "commit", "-q", "-m", "init")
	gitT(t, repo, "remote", "add", "origin", remote)
	gitT(t, repo, "push", "-q", "-u", "origin", "main")
	return repo
}

// backupT: what the GitHub button does – all changed files selected, commit + push; then nothing may be left.
func backupT(t *testing.T, repo string) *gitCommitRes {
	t.Helper()
	st := gitStatus(repo, false)
	if !st.OK || len(st.Files) == 0 {
		t.Fatalf("status before: ok=%v err=%q files=%d", st.OK, st.Error, len(st.Files))
	}
	res := gitCommit(gitCommitReq{Path: repo, Files: st.Files, Message: "test backup", Commit: true, Push: true}, func(string, string) {})
	if !res.OK {
		var b strings.Builder
		for _, s := range res.Steps {
			b.WriteString(s.Name + ": " + s.Out + "\n")
		}
		t.Fatalf("backup failed: %s\nfiles: %+v\n%s", res.Error, st.Files, b.String())
	}
	if !res.Pushed {
		t.Fatal("not pushed")
	}
	if left := gitT(t, repo, "status", "--porcelain"); strings.TrimSpace(left) != "" {
		t.Fatalf("left uncommitted:\n%s", left)
	}
	if ahead := strings.TrimSpace(gitT(t, repo, "rev-list", "--count", "origin/main..HEAD")); ahead != "0" {
		t.Fatalf("%s commit(s) not pushed", ahead)
	}
	return res
}

func TestGitBackupModifiedNewDeleted(t *testing.T) {
	repo := newRepoT(t, false)
	writeT(t, filepath.Join(repo, "a.txt"), "changed\n")           // " M"
	writeT(t, filepath.Join(repo, "new.txt"), "new\n")             // "??"
	writeT(t, filepath.Join(repo, "sub/new2.txt"), "new in sub\n") // "??" in a folder
	os.Remove(filepath.Join(repo, "c.txt"))                        // " D"
	backupT(t, repo)
	if strings.Contains(gitT(t, repo, "ls-files"), "c.txt") {
		t.Fatal("c.txt still tracked")
	}
}

// git rm --cached + the file in .gitignore: "D." (index only), the file stays on the disk and is ignored.
func TestGitBackupRmCachedIgnored(t *testing.T) {
	repo := newRepoT(t, false)
	gitT(t, repo, "rm", "-q", "--cached", "d.bin")
	writeT(t, filepath.Join(repo, ".gitignore"), "*.tmp\nd.bin\n")
	backupT(t, repo)
	if strings.Contains(gitT(t, repo, "ls-files"), "d.bin") {
		t.Fatal("d.bin still tracked")
	}
	if _, err := os.Stat(filepath.Join(repo, "d.bin")); err != nil {
		t.Fatal("d.bin must stay on the disk")
	}
}

// the deletion is already in the index after a manual "git add -A": "D.", the file is neither on the disk nor in the index.
func TestGitBackupDeletionAlreadyStaged(t *testing.T) {
	repo := newRepoT(t, false)
	os.Remove(filepath.Join(repo, "e.txt"))
	writeT(t, filepath.Join(repo, "a.txt"), "changed too\n")
	gitT(t, repo, "add", "-A")
	backupT(t, repo)
	if strings.Contains(gitT(t, repo, "ls-files"), "e.txt") {
		t.Fatal("e.txt still tracked")
	}
}

// renamed with git mv ("R."), renamed in Explorer (" D" + "??") and a staged change with another change on top ("MM").
func TestGitBackupRenamesAndMixed(t *testing.T) {
	repo := newRepoT(t, false)
	gitT(t, repo, "mv", "f.txt", "g.txt")
	os.Rename(filepath.Join(repo, "sub/s.txt"), filepath.Join(repo, "sub/s2.txt"))
	writeT(t, filepath.Join(repo, "m.txt"), "staged\n")
	gitT(t, repo, "add", "m.txt")
	writeT(t, filepath.Join(repo, "m.txt"), "staged and changed again\n")
	backupT(t, repo)
	files := gitT(t, repo, "ls-files")
	if strings.Contains(files, "f.txt") || !strings.Contains(files, "g.txt") || !strings.Contains(files, "sub/s2.txt") || strings.Contains(files, "sub/s.txt\n") {
		t.Fatalf("renames not committed:\n%s", files)
	}
	if got := gitT(t, repo, "show", "HEAD:m.txt"); got != "staged and changed again\n" {
		t.Fatalf("m.txt = %q", got)
	}
}

// many files with LF while core.autocrlf=true – git writes a "LF will be replaced by CRLF" warning for each.
func TestGitBackupCRLFWarnings(t *testing.T) {
	repo := newRepoT(t, true)
	for i := 0; i < 60; i++ {
		writeT(t, filepath.Join(repo, "lf", "file"+string(rune('a'+i%26))+strings.Repeat("x", i/26)+".txt"), "line\n")
	}
	writeT(t, filepath.Join(repo, "a.txt"), "changed\n")
	backupT(t, repo)
}

// only staged changes (nothing in the working copy): git add has nothing to do and must not even run.
func TestGitBackupOnlyStaged(t *testing.T) {
	repo := newRepoT(t, false)
	gitT(t, repo, "rm", "-q", "--cached", "d.bin")
	os.Remove(filepath.Join(repo, "e.txt"))
	gitT(t, repo, "add", "-A")
	res := backupT(t, repo)
	for _, s := range res.Steps {
		if s.Name == T("Přidání souborů") {
			t.Fatal("git add ran although only staged changes were selected")
		}
	}
}

func TestGitStepOutLFWarnings(t *testing.T) {
	out := "warning: in the working copy of 'a.txt', LF will be replaced by CRLF the next time Git touches it\n" +
		"warning: in the working copy of 'b.txt', LF will be replaced by CRLF the next time Git touches it\n" +
		"fatal: pathspec 'x.zip' did not match any files"
	got := tidyGitOut(out)
	if strings.Contains(got, "LF will be replaced") || !strings.Contains(got, "fatal: pathspec 'x.zip'") || !strings.Contains(got, "2") {
		t.Fatalf("%q", got)
	}
	if tidyGitOut("all fine") != "all fine" {
		t.Fatal("changed a normal output")
	}
}

// Only some files chosen while another change is staged: the chosen ones are committed and pushed (also an
// rm --cached one that is still on the disk), the other staged change stays staged and out of the commit.
func TestGitBackupPartialKeepsOtherStaged(t *testing.T) {
	repo := newRepoT(t, false)
	writeT(t, filepath.Join(repo, "a.txt"), "staged, not chosen\n")
	gitT(t, repo, "add", "a.txt")
	writeT(t, filepath.Join(repo, "c.txt"), "chosen\n")
	writeT(t, filepath.Join(repo, ".gitignore"), "*.tmp\nd.bin\n")
	gitT(t, repo, "rm", "-q", "--cached", "d.bin")
	st := gitStatus(repo, false)
	var pick []GitFile
	for _, f := range st.Files {
		if f.Path != "a.txt" {
			pick = append(pick, f)
		}
	}
	res := gitCommit(gitCommitReq{Path: repo, Files: pick, Message: "partial", Commit: true, Push: true}, func(string, string) {})
	if !res.OK || !res.Pushed {
		t.Fatalf("partial backup failed: %s %+v", res.Error, res.Steps)
	}
	if left := strings.TrimSpace(gitT(t, repo, "status", "--porcelain")); left != "M  a.txt" {
		t.Fatalf("expected only a.txt staged, got:\n%s", left)
	}
	if got := gitT(t, repo, "show", "--name-status", "--format=", "HEAD"); strings.Contains(got, "a.txt") || !strings.Contains(got, "c.txt") || !strings.Contains(got, "D\td.bin") {
		t.Fatalf("wrong commit content:\n%s", got)
	}
	if n := strings.TrimSpace(gitT(t, repo, "rev-list", "--count", "origin/main..HEAD")); n != "0" {
		t.Fatalf("%s commits not pushed", n)
	}
	if _, err := os.Stat(filepath.Join(repo, "d.bin")); err != nil {
		t.Fatal("d.bin removed from the disk")
	}
}

// The very first commit of a new repository.
func TestGitBackupFirstCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	remote, repo := filepath.Join(root, "remote.git"), filepath.Join(root, "repo")
	gitT(t, root, "init", "-q", "--bare", "-b", "main", remote)
	gitT(t, root, "init", "-q", "-b", "main", repo)
	gitT(t, repo, "config", "user.name", "Test")
	gitT(t, repo, "config", "user.email", "test@example.com")
	gitT(t, repo, "remote", "add", "origin", remote)
	writeT(t, filepath.Join(repo, "a.txt"), "a\n")
	writeT(t, filepath.Join(repo, "sub/b.txt"), "b\n")
	backupT(t, repo)
}
