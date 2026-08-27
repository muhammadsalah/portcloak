// Package sftpstore pushes snapshots onto a folder on a remote host.
//
// It rides the same SSH transport as the SSH target, so an operator who has
// already described a host does not describe it twice, and the same keychain
// entry can back both.
package sftpstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/store"
	"portcloak/internal/engine/target/sshx"
)

// Store is the SFTP BlobStore.
type Store struct {
	name string
	root string
	cfg  sshx.Config

	mu     sync.Mutex
	conn   *sshx.Conn
	client *sftp.Client
	// remoteHashing records whether the host can compute a digest for us, which
	// the probe reports rather than hides.
	remoteHashing *bool
}

// New builds an SFTP store from a storage definition.
func New(st config.Storage, creds config.CredentialStore) (*Store, error) {
	cfg, err := sshx.FromStorage(st, creds)
	if err != nil {
		return nil, err
	}
	return &Store{
		name: st.Name,
		root: strings.TrimRight(strings.TrimSpace(st.Folder), "/"),
		cfg:  cfg,
	}, nil
}

// AcceptHostKey records the operator's decision to trust a first connection.
func (s *Store) AcceptHostKey() { s.cfg.AcceptNewHostKey = true }

// Endpoint identifies this store for the circuit breaker and error messages.
func (s *Store) Endpoint() string {
	return fmt.Sprintf("sftp://%s@%s%s", s.cfg.Target.User, s.cfg.Target.Host, s.root)
}

func (s *Store) connect(ctx context.Context) (*sftp.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		return s.client, nil
	}
	conn, err := sshx.Dial(ctx, s.cfg)
	if err != nil {
		return nil, err
	}
	client, err := sftp.NewClient(conn.Client())
	if err != nil {
		_ = conn.Close()
		return nil, resil.Retry("open an SFTP channel",
			fmt.Sprintf("PortCloak connected to %s but could not start SFTP.", s.cfg.Target.Host), err).
			WithAdvice("The host may not have an SFTP subsystem enabled.")
	}
	s.conn, s.client = conn, client
	return client, nil
}

// Close shuts the channel and connection down.
func (s *Store) Close() error {
	s.mu.Lock()
	client, conn := s.client, s.conn
	s.client, s.conn = nil, nil
	s.mu.Unlock()

	var errs []error
	if client != nil {
		errs = append(errs, client.Close())
	}
	if conn != nil {
		errs = append(errs, conn.Close())
	}
	return errors.Join(errs...)
}

func (s *Store) path(key string) (string, error) {
	for _, seg := range strings.Split(key, "/") {
		if seg == ".." {
			return "", resil.Fatal("resolve a key",
				fmt.Sprintf("%q points outside the storage folder.", key), nil)
		}
	}
	return path.Join(s.root, key), nil
}

// Probe performs the round trip UC-S5 describes: list, write, verify, delete —
// and cleans up after itself even when a step fails.
func (s *Store) Probe(ctx context.Context) (store.Reach, error) {
	start := time.Now()
	r := store.Reach{Root: s.Endpoint(), Resumable: true, ProbedAt: start}

	client, err := s.connect(ctx)
	if err != nil {
		r.Access = store.AccessNone
		r.FailedStep = "connecting"
		r.Detail = err.Error()
		return r, nil
	}

	if _, err := client.Stat(s.root); err != nil {
		r.Access = store.AccessNone
		r.FailedStep = "finding the folder"
		r.Detail = fmt.Sprintf("%s does not exist on %s.", s.root, s.cfg.Target.Host)
		return r, nil
	}
	if _, err := client.ReadDir(s.root); err != nil {
		r.Access = store.AccessNone
		r.FailedStep = "listing the folder"
		r.Detail = err.Error()
		return r, nil
	}
	r.Access = store.AccessReadOnly

	probeKey := path.Join(s.root, ".portcloak-probe")
	payload := []byte("portcloak write probe")
	// The cleanup is deferred before the write, so probe artifacts are removed
	// even when the verification below fails.
	defer func() { _ = client.Remove(probeKey) }()

	f, err := client.Create(probeKey)
	if err != nil {
		r.Detail = fmt.Sprintf("The folder can be read but not written to: %v", err)
		r.Latency = time.Since(start)
		r.Integrity = store.IntegrityReadBack
		return r, nil
	}
	if _, err := f.Write(payload); err != nil {
		_ = f.Close()
		r.FailedStep = "writing the probe file"
		r.Detail = err.Error()
		r.Latency = time.Since(start)
		return r, nil
	}
	if err := f.Close(); err != nil {
		r.FailedStep = "closing the probe file"
		r.Detail = err.Error()
		return r, nil
	}

	got, err := readAll(client, probeKey)
	if err != nil || string(got) != string(payload) {
		r.FailedStep = "reading the probe file back"
		r.Detail = "The host accepted a write but did not return the same bytes."
		r.Latency = time.Since(start)
		return r, nil
	}

	r.Access = store.AccessWritable
	r.Integrity = s.integrityMethod(ctx)
	r.Latency = time.Since(start)
	return r, nil
}

// integrityMethod establishes how a digest will be obtained, and reports it
// rather than hiding which method applies.
func (s *Store) integrityMethod(ctx context.Context) store.Integrity {
	s.mu.Lock()
	cached := s.remoteHashing
	conn := s.conn
	s.mu.Unlock()

	if cached != nil {
		if *cached {
			return store.IntegrityRemoteCommand
		}
		return store.IntegrityReadBack
	}
	if conn == nil {
		return store.IntegrityReadBack
	}

	available := false
	if session, err := conn.Client().NewSession(); err == nil {
		defer session.Close() //nolint:errcheck
		var out strings.Builder
		session.Stdout = &out
		if err := session.Run("command -v sha256sum >/dev/null && echo yes"); err == nil {
			available = strings.Contains(out.String(), "yes")
		}
	}

	s.mu.Lock()
	s.remoteHashing = &available
	s.mu.Unlock()

	if available {
		return store.IntegrityRemoteCommand
	}
	return store.IntegrityReadBack
}

// Stat reports on one object.
func (s *Store) Stat(ctx context.Context, key string) (store.ObjectInfo, error) {
	client, err := s.connect(ctx)
	if err != nil {
		return store.ObjectInfo{}, err
	}
	p, err := s.path(key)
	if err != nil {
		return store.ObjectInfo{}, err
	}
	st, err := client.Stat(p)
	if err != nil {
		if isNotExist(err) {
			return store.ObjectInfo{}, fmt.Errorf("%w: %s", store.ErrNotFound, key)
		}
		return store.ObjectInfo{}, err
	}
	return store.ObjectInfo{Key: key, Size: st.Size(), ModTime: st.ModTime()}, nil
}

// Put writes an object, resuming at an offset when one is given.
//
// SFTP can write at an offset, which is what makes an interrupted upload cost
// the remainder rather than the whole file. The write goes to a temp name and
// is renamed, so a dropped connection never leaves something that looks
// complete.
func (s *Store) Put(ctx context.Context, key string, r io.Reader, opts store.PutOptions) (store.PutResult, error) {
	client, err := s.connect(ctx)
	if err != nil {
		return store.PutResult{}, err
	}
	p, err := s.path(key)
	if err != nil {
		return store.PutResult{}, err
	}
	if err := client.MkdirAll(path.Dir(p)); err != nil {
		return store.PutResult{}, resil.Retry("upload the snapshot",
			fmt.Sprintf("PortCloak could not create %s on %s.", path.Dir(p), s.cfg.Target.Host), err)
	}

	tmp := p + ".part"
	var written int64
	var f *sftp.File

	if opts.Offset > 0 {
		if st, statErr := client.Stat(tmp); statErr == nil && st.Size() >= opts.Offset {
			f, err = client.OpenFile(tmp, os.O_WRONLY)
			if err == nil {
				if _, err = f.Seek(opts.Offset, io.SeekStart); err == nil {
					written = opts.Offset
				}
			}
		} else {
			// The checkpoint describes progress the remote file cannot
			// corroborate, so starting again is the honest move rather than
			// resuming into a gap.
			opts.Offset = 0
		}
	}
	if f == nil {
		f, err = client.Create(tmp)
		if err != nil {
			return store.PutResult{}, resil.Retry("upload the snapshot",
				fmt.Sprintf("PortCloak could not create %s.", tmp), err)
		}
	}

	h := sha256.New()
	pw := &progressWriter{w: io.MultiWriter(f, h), ctx: ctx, onWrite: opts.Progress, written: written}
	n, copyErr := io.Copy(pw, r)
	if copyErr != nil {
		_ = f.Close()
		return store.PutResult{}, resil.Retry("upload the snapshot",
			fmt.Sprintf("The connection to %s dropped partway through the upload.", s.cfg.Target.Host), copyErr).
			WithAdvice("The job kept its checkpoint, so resuming continues from where it stopped.")
	}
	if err := f.Close(); err != nil {
		return store.PutResult{}, resil.Retry("upload the snapshot",
			"The connection dropped while finishing the upload.", err)
	}

	digest := hex.EncodeToString(h.Sum(nil))
	if opts.Offset > 0 {
		digest = opts.Digest
	} else if opts.Digest != "" && opts.Digest != digest {
		_ = client.Remove(tmp)
		return store.PutResult{}, resil.Fatal("verify the upload",
			fmt.Sprintf("%s did not arrive intact — the bytes written do not match the digest computed before the transfer.", key), nil)
	}

	_ = client.Remove(p)
	if err := client.Rename(tmp, p); err != nil {
		return store.PutResult{}, resil.Retry("finish the upload",
			fmt.Sprintf("PortCloak could not put %s in place.", p), err)
	}
	if err := client.Chmod(p, 0o600); err != nil {
		// An unencrypted bundle in a world-readable file is exactly the
		// exposure the tool exists to manage, so a failure here is worth
		// reporting rather than ignoring.
		return store.PutResult{}, resil.Fatal("restrict the uploaded file",
			fmt.Sprintf("%s was uploaded but its permissions could not be restricted.", p), err)
	}

	return store.PutResult{Key: key, Size: written + n, Digest: digest, Resumed: opts.Offset > 0}, nil
}

// Get reads an object.
func (s *Store) Get(ctx context.Context, key string, w io.Writer, opts store.GetOptions) (store.GetResult, error) {
	client, err := s.connect(ctx)
	if err != nil {
		return store.GetResult{}, err
	}
	p, err := s.path(key)
	if err != nil {
		return store.GetResult{}, err
	}
	f, err := client.Open(p)
	if err != nil {
		if isNotExist(err) {
			return store.GetResult{}, fmt.Errorf("%w: %s", store.ErrNotFound, key)
		}
		return store.GetResult{}, resil.Retry("download the snapshot",
			fmt.Sprintf("PortCloak could not open %s.", p), err)
	}
	defer f.Close() //nolint:errcheck

	if opts.Offset > 0 {
		if _, err := f.Seek(opts.Offset, io.SeekStart); err != nil {
			return store.GetResult{}, err
		}
	}
	h := sha256.New()
	pw := &progressWriter{w: io.MultiWriter(w, h), ctx: ctx, onWrite: opts.Progress, written: opts.Offset}
	n, err := io.Copy(pw, f)
	if err != nil {
		return store.GetResult{}, resil.Retry("download the snapshot",
			"The connection dropped partway through the download.", err)
	}
	return store.GetResult{Size: opts.Offset + n, Digest: hex.EncodeToString(h.Sum(nil))}, nil
}

// List returns every object under a prefix.
func (s *Store) List(ctx context.Context, prefix string) ([]store.ObjectInfo, error) {
	client, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	base := s.root
	if prefix != "" {
		p, err := s.path(prefix)
		if err != nil {
			return nil, err
		}
		base = p
	}
	if st, err := client.Stat(base); err != nil || !st.IsDir() {
		base = path.Dir(base)
	}

	var out []store.ObjectInfo
	walker := client.Walk(base)
	for walker.Step() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := walker.Err(); err != nil {
			if isNotExist(err) {
				continue
			}
			return nil, resil.Retry("list the storage",
				fmt.Sprintf("PortCloak could not read %s on %s.", base, s.cfg.Target.Host), err)
		}
		st := walker.Stat()
		if st.IsDir() {
			continue
		}
		name := path.Base(walker.Path())
		if name == ".portcloak-probe" || strings.HasSuffix(name, ".part") {
			continue
		}
		key := strings.TrimPrefix(strings.TrimPrefix(walker.Path(), s.root), "/")
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			continue
		}
		out = append(out, store.ObjectInfo{Key: key, Size: st.Size(), ModTime: st.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// Delete removes an object.
func (s *Store) Delete(ctx context.Context, key string) error {
	client, err := s.connect(ctx)
	if err != nil {
		return err
	}
	p, err := s.path(key)
	if err != nil {
		return err
	}
	if err := client.Remove(p); err != nil && !isNotExist(err) {
		return err
	}
	// Prune the realm directory once it is empty, so a browsable tree does not
	// accumulate the ghosts of deleted realms.
	dir := path.Dir(p)
	for dir != s.root && strings.HasPrefix(dir, s.root) {
		entries, err := client.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			break
		}
		if err := client.RemoveDirectory(dir); err != nil {
			break
		}
		dir = path.Dir(dir)
	}
	return nil
}

// EnsureRoot creates the remote folder.
func (s *Store) EnsureRoot(ctx context.Context) error {
	client, err := s.connect(ctx)
	if err != nil {
		return err
	}
	return client.MkdirAll(s.root)
}

func readAll(client *sftp.Client, p string) ([]byte, error) {
	f, err := client.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	return io.ReadAll(f)
}

func isNotExist(err error) bool {
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "file does not exist") ||
		strings.Contains(strings.ToLower(err.Error()), "no such file")
}

type progressWriter struct {
	w       io.Writer
	ctx     context.Context
	onWrite func(int64)
	written int64
}

func (p *progressWriter) Write(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := p.w.Write(b)
	p.written += int64(n)
	if p.onWrite != nil {
		p.onWrite(p.written)
	}
	return n, err
}
