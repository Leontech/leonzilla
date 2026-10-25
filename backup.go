package main

// Backups: each pair zips a local folder into <target>\<prefix>_YYYY-MM-DD_HH-MM-SS.zip.
// Excludes work like for synchronization (pair excludes + "exclude in all backups" of the card).

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

type BackupPair struct {
	ID       string   `json:"id"`
	Source   string   `json:"source"`   // folder to back up
	Target   string   `json:"target"`   // folder where the zip is written
	Prefix   string   `json:"prefix"`   // zip name prefix ("" = source folder name)
	Enabled  bool     `json:"enabled"`  //
	Excludes []string `json:"excludes"` // relative to Source, like sync pair excludes
}

var defaultBackupExcludes = []string{"__history/", "__recovery/", "Thumbs.db", "desktop.ini", ".DS_Store", "*.tmp", "~$*", "*.zip.part"}

var reBadName = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)

func (p *BackupPair) zipPrefix() string {
	pre := strings.TrimSpace(p.Prefix)
	if pre == "" {
		pre = filepath.Base(filepath.Clean(strings.TrimSpace(p.Source)))
	}
	pre = strings.Trim(reBadName.ReplaceAllString(pre, "_"), " .")
	if pre == "" {
		pre = "zaloha"
	}
	return pre
}

// backupName: prefix_RRRR-MM-DD_hh-mm-ss.zip, with a note prefix_note_RRRR-MM-DD_hh-mm-ss.zip
func backupName(p *BackupPair, t time.Time, note string) string {
	if note != "" {
		return p.zipPrefix() + "_" + note + "_" + t.Format("2006-01-02_15-04-05") + ".zip"
	}
	return p.zipPrefix() + "_" + t.Format("2006-01-02_15-04-05") + ".zip"
}

// cleanNote makes the backup note safe for a file name (forbidden characters → _, max 60 characters).
func cleanNote(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(reBadName.ReplaceAllString(s, "_"), " ._")
	if r := []rune(s); len(r) > 60 {
		s = strings.TrimRight(string(r[:60]), " ._")
	}
	return s
}

type backupFile struct {
	path, rel string
	size      int64
	mod       time.Time
}

// prepareBackups creates the step list for the enabled pairs.
func prepareBackups(j *Job, tab *Tab, notes, names []string, only []int) ([]*BackupPair, error) {
	var pairs []*BackupPair
	j.notes = map[*BackupPair]string{}
	j.names = map[*BackupPair]string{}
	pick := map[int]bool{}
	for _, i := range only {
		pick[i] = true
	}
	for i, p := range tab.Backups {
		if only != nil && !pick[i] {
			continue // unticked in the confirmation – skipped this time
		}
		if p.Enabled && strings.TrimSpace(p.Source) != "" && strings.TrimSpace(p.Target) != "" {
			pairs = append(pairs, p)
			if i < len(notes) && !tab.BackupNoNote {
				j.notes[p] = cleanNote(notes[i])
			}
			if i < len(names) && !tab.BackupNoNote {
				n := strings.TrimSpace(names[i])
				if strings.HasSuffix(strings.ToLower(n), ".zip") {
					n = n[:len(n)-4]
				}
				j.names[p] = cleanNote(n)
			}
			j.Steps = append(j.Steps, &Step{Name: T("Záloha ") + p.zipPrefix(), State: "pending"})
		}
	}
	if len(pairs) == 0 {
		if only != nil {
			return nil, errors.New(T("Není vybraná žádná záloha"))
		}
		return nil, errors.New(T("Karta nemá žádnou zapnutou zálohu (složka a cíl)"))
	}
	return pairs, nil
}

func (r *Runner) runBackups(ctx context.Context, j *Job, tab *Tab) (string, string) {
	stamp := j.Started
	okCount, failed := 0, 0
	var totalZip int64
	for i, p := range j.backups {
		if ctx.Err() != nil {
			j.setStep(i, "skipped", "")
			continue
		}
		j.setStep(i, "running", "")
		size, files, err := r.backupPair(ctx, j, tab, p, stamp)
		if ctx.Err() != nil {
			j.setStep(i, "failed", T("Přerušeno"))
			continue
		}
		if err != nil {
			failed++
			j.logf("err", "%s: %v", p.zipPrefix(), err)
			j.setStep(i, "failed", err.Error())
			continue
		}
		okCount++
		totalZip += size
		j.setStep(i, "ok", fmt.Sprintf(T("%d souborů → %s"), files, humanBytes(size)))
	}
	switch {
	case ctx.Err() != nil:
		return "cancelled", T("Záloha přerušena")
	case failed > 0 && okCount == 0:
		return "failed", T("Záloha se nepovedla")
	case failed > 0 || atomic.LoadInt64(&j.FilesFail) > 0:
		return "errors", fmt.Sprintf(T("Záloha hotová s chybami (%d ZIP, %s)"), okCount, humanBytes(totalZip))
	}
	return "ok", fmt.Sprintf(T("Záloha hotová: %d ZIP, %s"), okCount, humanBytes(totalZip))
}

// backupPair zips one folder. Returns the zip size and the number of files.
func (r *Runner) backupPair(ctx context.Context, j *Job, tab *Tab, p *BackupPair, stamp time.Time) (int64, int, error) {
	src := filepath.Clean(strings.TrimSpace(p.Source))
	dst := filepath.Clean(strings.TrimSpace(p.Target))
	if st, err := os.Stat(src); err != nil || !st.IsDir() {
		return 0, 0, fmt.Errorf(T("složka %s neexistuje"), src)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, 0, fmt.Errorf(T("cílovou složku %s nejde vytvořit: %v"), dst, err)
	}
	m := newMatcher(append(append([]string{}, tab.BackupExcludes...), p.Excludes...))
	dstAbs, _ := filepath.Abs(dst)

	// 1) list files
	j.setPhase("scanning")
	final := filepath.Join(dst, backupName(p, stamp, j.notes[p]))
	if own := j.names[p]; own != "" {
		// own name (fw-160.zip) never overwrites an older backup: fw-160 (2).zip, fw-160 (3).zip …
		final = filepath.Join(dst, own+".zip")
		for n := 2; fileExists(final) || fileExists(final+".part"); n++ {
			final = filepath.Join(dst, fmt.Sprintf("%s (%d).zip", own, n))
		}
	}
	j.logf("info", T("Záloha %s → %s"), src, final)
	var files []backupFile
	var total int64
	excluded := 0
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			j.logf("warn", T("Nelze číst %s: %v"), path, err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == src {
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if abs, _ := filepath.Abs(path); strings.EqualFold(abs, dstAbs) {
				return fs.SkipDir // target folder inside the backed up folder
			}
			if m.excluded(rel, true) {
				excluded++
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || m.excluded(rel, false) {
			if m.excluded(rel, false) {
				excluded++
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, backupFile{path: path, rel: rel, size: fi.Size(), mod: fi.ModTime()})
		total += fi.Size()
		atomic.AddInt64(&j.ScanFiles, 1)
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	j.logf("info", T("  %d souborů (%s), vyloučeno %d"), len(files), humanBytes(total), excluded)
	j.mu.Lock()
	j.FilesTotal += len(files)
	j.BytesTotal += total
	j.mu.Unlock()

	// 2) zip into a temporary file, then rename
	j.setPhase("zipping")
	tmp := final + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, 0, fmt.Errorf(T("ZIP nejde vytvořit: %v"), err)
	}
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(tmp)
		}
	}()
	zw := zip.NewWriter(f)
	root := filepath.Base(src)
	done := 0
	for _, bf := range files {
		if ctx.Err() != nil {
			return 0, 0, ctx.Err()
		}
		j.setActive(0, bf.rel)
		if err := addToZip(ctx, zw, root+"/"+bf.rel, bf, func(n int64) { atomic.AddInt64(&j.BytesDone, n) }); err != nil {
			if ctx.Err() != nil {
				return 0, 0, ctx.Err()
			}
			atomic.AddInt64(&j.FilesFail, 1)
			j.logf("warn", T("Vynechán %s: %v"), bf.rel, err)
			continue
		}
		done++
		atomic.AddInt64(&j.FilesDone, 1)
	}
	j.setActive(0, "")
	if err := zw.Close(); err != nil {
		return 0, 0, fmt.Errorf(T("ZIP nejde dokončit: %v"), err)
	}
	if err := f.Close(); err != nil {
		return 0, 0, fmt.Errorf(T("ZIP nejde uložit: %v"), err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return 0, 0, fmt.Errorf(T("ZIP nejde přejmenovat: %v"), err)
	}
	ok = true
	st, _ := os.Stat(final)
	var size int64
	if st != nil {
		size = st.Size()
	}
	j.logf("up", T("%s (%s, %d souborů)"), final, humanBytes(size), done)
	return size, done, nil
}

func addToZip(ctx context.Context, zw *zip.Writer, name string, bf backupFile, prog func(int64)) error {
	in, err := os.Open(bf.path)
	if err != nil {
		return err
	}
	defer in.Close()
	h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: bf.mod}
	h.SetMode(0o644)
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, &progressReader{ctx: ctx, r: in, fn: prog})
	return err
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
