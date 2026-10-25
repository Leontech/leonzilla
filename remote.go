package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// RemoteEntry is one item in a remote directory listing.
type RemoteEntry struct {
	Name  string
	IsDir bool
	Size  int64
	Mtime time.Time
}

// Remote is a connection to the server (FTP/FTPS/SFTP).
// It is not safe for concurrent use – each worker opens its own.
type Remote interface {
	Home() string                           // initial directory after login
	List(dir string) ([]RemoteEntry, error) // direct children, without . and ..
	Mkdir(dir string) error                 // single level
	Upload(ctx context.Context, local, remote string, progress func(int64)) error
	Rename(from, to string) error // overwrite target if possible
	Delete(file string) error     // used only for our own temp files / replacing a file
	PreciseTime() bool            // listing returns exact modification times
	IsDir(dir string) bool
	Size(file string) int64 // -1 = unknown
	Info() string
	Close()
}

// ErrHostKeyChanged signals that the SFTP host key differs from the stored one.
type ErrHostKeyChanged struct{ Old, New string }

func (e *ErrHostKeyChanged) Error() string {
	return fmt.Sprintf(T("Klíč serveru se změnil! Uložený %s, server teď posílá %s. Pokud jde o změnu na straně providera, klikni na „Důvěřovat novému klíči“."), e.Old, e.New)
}

// dial opens a connection. newHostKey receives the SFTP fingerprint when none was stored yet.
func dial(ctx context.Context, c Conn, newHostKey *string) (Remote, error) {
	if strings.TrimSpace(c.Host) == "" {
		return nil, errors.New(T("Není vyplněný server"))
	}
	switch c.Protocol {
	case "sftp":
		return dialSFTP(ctx, c, newHostKey)
	case "ftp", "ftps", "ftps-implicit":
		return dialFTP(ctx, c)
	}
	return nil, fmt.Errorf(T("neznámý protokol %q"), c.Protocol)
}

func hostPort(c Conn, def int) string {
	p := c.Port
	if p == 0 {
		p = def
	}
	h := strings.TrimSpace(c.Host)
	h = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(h, "ftp://"), "sftp://"), "ftps://")
	h = strings.TrimSuffix(h, "/")
	if hh, pp, err := net.SplitHostPort(h); err == nil { // "host:port" typed into host field
		if n, err := strconv.Atoi(pp); err == nil && c.Port == 0 {
			return net.JoinHostPort(hh, strconv.Itoa(n))
		}
		h = hh
	}
	return net.JoinHostPort(h, strconv.Itoa(p))
}

// ---------------------------------------------------------------------------
// FTP / FTPS
// ---------------------------------------------------------------------------

type ftpRemote struct {
	c    *ftp.ServerConn
	home string
	info string
}

func dialFTP(ctx context.Context, c Conn) (Remote, error) {
	def := 21
	if c.Protocol == "ftps-implicit" {
		def = 990
	}
	addr := hostPort(c, def)
	host, _, _ := net.SplitHostPort(addr)
	tlsCfg := &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: c.InsecureTLS,
		ClientSessionCache: tls.NewLRUClientSessionCache(16), // many servers require TLS session reuse on data connections
		MinVersion:         tls.VersionTLS12,
	}
	dialer := &net.Dialer{Timeout: 20 * time.Second}
	useTLS := c.Protocol != "ftp"

	// control connection – dialed by us so that data connections can use our own dial function
	var ctrl net.Conn
	var err error
	ctrl, err = dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, explainNetErr(T("Připojení k ")+addr, err)
	}
	ctrl = &idleConn{Conn: ctrl}
	if c.Protocol == "ftps-implicit" {
		tc := tls.Client(ctrl, tlsCfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			ctrl.Close()
			return nil, explainNetErr("TLS s "+addr, err)
		}
		ctrl = tc
	}
	first := true
	opts := []ftp.DialOption{
		ftp.DialWithTimeout(30 * time.Second),
		ftp.DialWithShutTimeout(60 * time.Second),
		ftp.DialWithDisabledEPSV(c.DisableEPSV),
		// data connections: TLS with a proper shutdown (close_notify + wait for the server),
		// otherwise some servers drop the end of an uploaded file
		// (the first call of the dial function is the control connection, dialed above)
		ftp.DialWithDialFunc(func(network, a string) (net.Conn, error) {
			if first {
				first = false
				return ctrl, nil
			}
			nc, err := dialer.DialContext(ctx, network, a)
			if err != nil {
				return nil, err
			}
			nc = &idleConn{Conn: nc}
			if !useTLS {
				return nc, nil
			}
			return &gracefulTLS{Conn: tls.Client(nc, tlsCfg)}, nil
		}),
	}
	info := "FTP"
	switch c.Protocol {
	case "ftps":
		opts = append(opts, ftp.DialWithExplicitTLS(tlsCfg))
		info = T("FTPS (explicitní TLS)")
	case "ftps-implicit":
		info = T("FTPS (implicitní TLS)")
	}
	conn, err := ftp.Dial(addr, opts...)
	if err != nil {
		ctrl.Close()
		return nil, explainNetErr(T("Připojení k ")+addr, err)
	}
	user := c.User
	if user == "" {
		user = "anonymous"
	}
	if err := conn.Login(user, c.pass); err != nil {
		conn.Quit()
		return nil, fmt.Errorf(T("Přihlášení odmítnuto: %v"), err)
	}
	_ = conn.Type(ftp.TransferTypeBinary)
	home, err := conn.CurrentDir()
	if err != nil || home == "" {
		home = "/"
	}
	r := &ftpRemote{c: conn, home: home, info: info}
	if conn.IsTimePreciseInList() {
		r.info += ", MLSD"
	} else {
		r.info += T(", LIST (bez přesných časů)")
	}
	return r, nil
}

func (r *ftpRemote) Home() string      { return r.home }
func (r *ftpRemote) Info() string      { return r.info }
func (r *ftpRemote) PreciseTime() bool { return r.c.IsTimePreciseInList() }
func (r *ftpRemote) Close()            { _ = r.c.Quit() }

func (r *ftpRemote) List(dir string) ([]RemoteEntry, error) {
	es, err := r.c.List(dir)
	if err != nil {
		return nil, err
	}
	out := make([]RemoteEntry, 0, len(es))
	for _, e := range es {
		name := path.Base(e.Name) // some servers return full paths
		if name == "." || name == ".." || name == "" {
			continue
		}
		switch e.Type {
		case ftp.EntryTypeFolder:
			out = append(out, RemoteEntry{Name: name, IsDir: true, Mtime: e.Time})
		case ftp.EntryTypeFile:
			out = append(out, RemoteEntry{Name: name, Size: int64(e.Size), Mtime: e.Time})
		case ftp.EntryTypeLink:
			// treat links as directories only if we can enter them – keep simple: as file entry with unknown size
			out = append(out, RemoteEntry{Name: name, Size: -1, Mtime: e.Time})
		}
	}
	return out, nil
}

func (r *ftpRemote) Mkdir(dir string) error { return r.c.MakeDir(dir) }

// IsDir uses CWD – listing a missing directory returns an empty list on some servers.
func (r *ftpRemote) IsDir(dir string) bool {
	if r.c.ChangeDir(dir) != nil {
		return false
	}
	_ = r.c.ChangeDir(r.home)
	return true
}

func (r *ftpRemote) Upload(ctx context.Context, local, remote string, progress func(int64)) error {
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer f.Close()
	return r.c.Stor(remote, &progressReader{ctx: ctx, r: f, fn: progress})
}

func (r *ftpRemote) Rename(from, to string) error {
	if err := r.c.Rename(from, to); err != nil {
		// some servers refuse to rename onto an existing file
		if derr := r.c.Delete(to); derr == nil {
			return r.c.Rename(from, to)
		}
		return err
	}
	return nil
}

func (r *ftpRemote) Delete(file string) error { return r.c.Delete(file) }

// ---------------------------------------------------------------------------
// SFTP
// ---------------------------------------------------------------------------

type sftpRemote struct {
	ssh  *ssh.Client
	c    *sftp.Client
	home string
	info string
}

func fingerprint(k ssh.PublicKey) string {
	h := sha256.Sum256(k.Marshal())
	return "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(h[:]), "=")
}

func dialSFTP(ctx context.Context, c Conn, newHostKey *string) (Remote, error) {
	addr := hostPort(c, 22)
	auths := []ssh.AuthMethod{}
	if c.KeyFile != "" {
		kb, err := os.ReadFile(c.KeyFile)
		if err != nil {
			return nil, fmt.Errorf(T("Soubor s klíčem nejde přečíst: %v"), err)
		}
		signer, err := ssh.ParsePrivateKey(kb)
		var ppe *ssh.PassphraseMissingError
		if errors.As(err, &ppe) {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(kb, []byte(c.pass))
		}
		if err != nil {
			return nil, fmt.Errorf(T("Klíč nejde načíst: %v"), err)
		}
		auths = append(auths, ssh.PublicKeys(signer))
	}
	if c.pass != "" {
		pw := c.pass
		auths = append(auths, ssh.Password(pw),
			ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
				a := make([]string, len(qs))
				for i := range a {
					a[i] = pw
				}
				return a, nil
			}))
	}
	cfg := &ssh.ClientConfig{
		User:    c.User,
		Auth:    auths,
		Timeout: 20 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			fp := fingerprint(key)
			if c.HostKey == "" {
				if newHostKey != nil {
					*newHostKey = fp
				}
				return nil
			}
			if c.HostKey != fp {
				return &ErrHostKeyChanged{Old: c.HostKey, New: fp}
			}
			return nil
		},
	}
	d := net.Dialer{Timeout: 20 * time.Second}
	nc, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, explainNetErr(T("Připojení k ")+addr, err)
	}
	sc, chans, reqs, err := ssh.NewClientConn(nc, addr, cfg)
	if err != nil {
		nc.Close()
		var hk *ErrHostKeyChanged
		if errors.As(err, &hk) {
			return nil, hk
		}
		if strings.Contains(err.Error(), "unable to authenticate") {
			return nil, errors.New(T("Přihlášení odmítnuto (špatné jméno, heslo nebo klíč)"))
		}
		return nil, fmt.Errorf("SSH: %v", err)
	}
	client := ssh.NewClient(sc, chans, reqs)
	sf, err := sftp.NewClient(client, sftp.UseConcurrentWrites(true))
	if err != nil {
		client.Close()
		return nil, fmt.Errorf(T("Server nepodporuje SFTP: %v"), err)
	}
	home, err := sf.Getwd()
	if err != nil || home == "" {
		home = "/"
	}
	return &sftpRemote{ssh: client, c: sf, home: home, info: "SFTP"}, nil
}

func (r *sftpRemote) Home() string      { return r.home }
func (r *sftpRemote) Info() string      { return r.info }
func (r *sftpRemote) PreciseTime() bool { return true }
func (r *sftpRemote) Close()            { r.c.Close(); r.ssh.Close() }

func (r *sftpRemote) List(dir string) ([]RemoteEntry, error) {
	fis, err := r.c.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]RemoteEntry, 0, len(fis))
	for _, fi := range fis {
		if fi.Name() == "." || fi.Name() == ".." {
			continue
		}
		isDir := fi.IsDir()
		if fi.Mode()&os.ModeSymlink != 0 {
			if st, err := r.c.Stat(path.Join(dir, fi.Name())); err == nil {
				isDir = st.IsDir()
			}
		}
		out = append(out, RemoteEntry{Name: fi.Name(), IsDir: isDir, Size: fi.Size(), Mtime: fi.ModTime()})
	}
	return out, nil
}

func (r *sftpRemote) Mkdir(dir string) error { return r.c.Mkdir(dir) }

func (r *sftpRemote) IsDir(dir string) bool {
	st, err := r.c.Stat(dir)
	return err == nil && st.IsDir()
}

func (r *sftpRemote) Upload(ctx context.Context, local, remote string, progress func(int64)) error {
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer f.Close()
	st, _ := f.Stat()
	w, err := r.c.OpenFile(remote, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	_, err = w.ReadFrom(&progressReader{ctx: ctx, r: f, fn: progress, size: st.Size()})
	cerr := w.Close()
	if err == nil {
		err = cerr
	}
	return err
}

func (r *sftpRemote) Rename(from, to string) error {
	if err := r.c.PosixRename(from, to); err == nil {
		return nil
	}
	if err := r.c.Rename(from, to); err != nil {
		if derr := r.c.Remove(to); derr == nil {
			return r.c.Rename(from, to)
		}
		return err
	}
	return nil
}

func (r *sftpRemote) Delete(file string) error { return r.c.Remove(file) }

// ---------------------------------------------------------------------------

type progressReader struct {
	ctx  context.Context
	r    io.Reader
	fn   func(int64)
	size int64
}

func (p *progressReader) Read(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := p.r.Read(b)
	if n > 0 && p.fn != nil {
		p.fn(int64(n))
	}
	return n, err
}

// Size lets pkg/sftp use concurrent writes.
func (p *progressReader) Size() int64 { return p.size }

func explainNetErr(what string, err error) error {
	var dnsErr *net.DNSError
	switch {
	case errors.As(err, &dnsErr):
		return fmt.Errorf(T("%s: server %q nebyl nalezen (DNS)"), what, dnsErr.Name)
	case strings.Contains(err.Error(), "refused"):
		return fmt.Errorf(T("%s: spojení odmítnuto – zkontroluj port a protokol"), what)
	case strings.Contains(err.Error(), "timeout"), strings.Contains(err.Error(), "i/o timeout"):
		return fmt.Errorf(T("%s: server neodpovídá (timeout)"), what)
	case strings.Contains(err.Error(), "certificate"):
		return fmt.Errorf(T("%s: certifikát serveru neprošel kontrolou (%v). Pokud provider používá certifikát na jiný název, zapni „Ignorovat chybu certifikátu“."), what, err)
	}
	return fmt.Errorf("%s: %v", what, err)
}

// gracefulTLS closes a TLS data connection the way FTP servers expect:
// send close_notify, then wait until the server closes its side.
type gracefulTLS struct{ *tls.Conn }

func (g *gracefulTLS) Close() error {
	_ = g.Conn.SetDeadline(time.Now().Add(15 * time.Second))
	if err := g.Conn.CloseWrite(); err == nil {
		_, _ = io.Copy(io.Discard, g.Conn)
	}
	return g.Conn.Close()
}

// Size returns the remote file size (for verification after upload); -1 when unknown.
func (r *ftpRemote) Size(file string) int64 {
	if n, err := r.c.FileSize(file); err == nil {
		return n
	}
	if es, err := r.List(path.Dir(file)); err == nil {
		for _, e := range es {
			if e.Name == path.Base(file) {
				return e.Size
			}
		}
	}
	return -1
}

func (r *sftpRemote) Size(file string) int64 {
	if st, err := r.c.Stat(file); err == nil {
		return st.Size()
	}
	return -1
}

// idleConn fails a read/write that makes no progress for 90 s (dead network),
// instead of hanging forever.
type idleConn struct{ net.Conn }

const idleTimeout = 90 * time.Second

func (c *idleConn) Read(b []byte) (int, error) {
	_ = c.Conn.SetReadDeadline(time.Now().Add(idleTimeout))
	return c.Conn.Read(b)
}

func (c *idleConn) Write(b []byte) (int, error) {
	_ = c.Conn.SetWriteDeadline(time.Now().Add(idleTimeout))
	return c.Conn.Write(b)
}
