package inspect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/crypto"
	"portcloak/internal/engine/inspect/index"
	"portcloak/internal/engine/manifest"
	"portcloak/internal/engine/obs"
	"portcloak/internal/engine/realm"
	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/snapshot"
	"portcloak/internal/engine/store"
)

// OpenRequest is what the operator supplied to open a snapshot.
type OpenRequest struct {
	Storage    string
	BundleKey  string
	SnapshotID string
	// Passphrase or Identities, depending on how the bundle was encrypted.
	Passphrase string
	Identities []string
}

// Session is one open snapshot.
//
// It owns a working directory holding the decrypted realm material and, once
// the Users tab is opened, an index. Both are destroyed on Close.
type Session struct {
	ID        string
	Realm     string
	Storage   string
	BundleKey string
	OpenedAt  time.Time

	Envelope   snapshot.Envelope
	Manifest   manifest.Manifest
	Provenance snapshot.Provenance
	Verify     snapshot.VerifyResult

	home   config.Home
	opened *snapshot.Opened
	rep    *realm.Representation

	mu       sync.Mutex
	idx      *index.Index
	indexing bool
	closed   bool
}

// Degraded reports whether the snapshot failed verification.
//
// A snapshot that cannot be proven intact opens in a clearly flagged read-only
// state so it can be diagnosed — and restore is blocked, which is enforced
// where a restore starts rather than here.
func (s *Session) Degraded() bool { return !s.Verify.OK }

// Open fetches, decrypts, verifies and reads a snapshot.
//
// Verification happens before anything is handed back. A bundle that fails its
// checksum is never presented as readable content.
func Open(ctx context.Context, home config.Home, blobs store.BlobStore, req OpenRequest, rep *obs.Reporter) (*Session, error) {
	if rep == nil {
		rep = obs.NewReporter(req.SnapshotID, obs.NopSink{})
	}

	workDir := home.WorkPath("open-"+req.SnapshotID, "")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return nil, err
	}
	// Anything created below is cleaned up unless the open succeeds, so a
	// failed open never leaves decrypted realm material behind.
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(workDir)
		}
	}()

	rep.StartPhase(obs.PhaseDownload)
	bundlePath := filepath.Join(workDir, "bundle"+store.BundleExt)
	f, err := os.OpenFile(bundlePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := blobs.Stat(ctx, req.BundleKey)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if _, err := blobs.Get(ctx, req.BundleKey, f, store.GetOptions{
		Progress: func(read int64) { rep.Progress(read, info.Size, "bytes", req.BundleKey) },
	}); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	rep.CompletePhase(obs.PhaseDownload, req.BundleKey)

	// The envelope says what kind of artifact this is, which is how the right
	// opener is chosen before any key is asked for.
	envelope, err := readEnvelope(ctx, bundlePath, req)
	if err != nil {
		return nil, err
	}
	opener, err := crypto.OpenerFor(envelope.Encryption, req.Passphrase, req.Identities)
	if err != nil {
		return nil, err
	}

	rep.StartPhase(obs.PhaseIntegrity)
	bundle, err := os.Open(bundlePath)
	if err != nil {
		return nil, err
	}
	extractDir := filepath.Join(workDir, "contents")
	if err := os.MkdirAll(extractDir, 0o700); err != nil {
		_ = bundle.Close()
		return nil, err
	}
	var opts snapshot.OpenOptions
	opts.Dir = extractDir
	if opener != nil {
		opts.Opener = opener
	}
	opened, err := snapshot.Open(ctx, bundle, opts)
	_ = bundle.Close()
	if err != nil {
		return nil, err
	}
	// The sealed bundle has been extracted; keeping the encrypted copy around
	// serves no purpose and doubles the footprint.
	_ = os.Remove(bundlePath)

	if opened.Verify.OK {
		rep.CompletePhase(obs.PhaseIntegrity, opened.Verify.Message)
	} else {
		rep.FailPhase(obs.PhaseIntegrity, opened.Verify.Message)
	}

	s := &Session{
		ID:        opened.Envelope.SnapshotID,
		Realm:     opened.Envelope.Realm,
		Storage:   req.Storage,
		BundleKey: req.BundleKey,
		OpenedAt:  time.Now(),
		Envelope:  opened.Envelope,
		Verify:    opened.Verify,
		home:      home,
		opened:    opened,
	}
	if err := opened.Document(snapshot.ManifestPath, &s.Manifest); err != nil {
		_ = opened.Close()
		return nil, resil.Fatal("open the snapshot",
			"This snapshot's manifest could not be read.", err)
	}
	if _, ok := opened.Documents[snapshot.ProvenancePath]; ok {
		_ = opened.Document(snapshot.ProvenancePath, &s.Provenance)
	}

	success = true
	return s, nil
}

func readEnvelope(ctx context.Context, path string, req OpenRequest) (snapshot.Envelope, error) {
	f, err := os.Open(path)
	if err != nil {
		return snapshot.Envelope{}, err
	}
	defer f.Close() //nolint:errcheck

	// An unencrypted bundle yields its envelope directly. An encrypted one does
	// not, and the sidecar is what says so — but a caller may have supplied a
	// key already, so both are tried before giving up.
	if e, err := snapshot.ReadEnvelopeOnly(ctx, f, nil); err == nil {
		return e, nil
	}

	if req.Passphrase != "" {
		if _, err := f.Seek(0, 0); err != nil {
			return snapshot.Envelope{}, err
		}
		opener, oerr := crypto.NewPassphraseOpener(req.Passphrase)
		if oerr == nil {
			if e, err := snapshot.ReadEnvelopeOnly(ctx, f, opener); err == nil {
				return e, nil
			}
		}
	}
	if len(req.Identities) > 0 {
		if _, err := f.Seek(0, 0); err != nil {
			return snapshot.Envelope{}, err
		}
		opener, oerr := crypto.NewIdentityOpener(req.Identities)
		if oerr == nil {
			if e, err := snapshot.ReadEnvelopeOnly(ctx, f, opener); err == nil {
				return e, nil
			}
		}
	}

	// The envelope could not be read at all, which for a well-formed bundle
	// means it is encrypted and the key supplied does not open it.
	return snapshot.Envelope{}, resil.Fatal("open the snapshot",
		"This snapshot is encrypted and could not be opened with the key supplied.", snapshot.ErrEncrypted).
		WithAdvice("Check the passphrase, or that you hold a private key matching one of the snapshot's recipients.")
}

// Representation lazily parses the realm file, which every entity view reads.
func (s *Session) Representation() (*realm.Representation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rep != nil {
		return s.rep, nil
	}
	for _, name := range s.opened.RealmFiles {
		base := strings.TrimPrefix(name, snapshot.RealmDir)
		if !strings.HasSuffix(base, "-realm.json") && base != s.Realm+".json" {
			continue
		}
		rep, err := realm.Load(s.opened.Path(name))
		if err != nil {
			return nil, err
		}
		s.rep = rep
		return rep, nil
	}
	return nil, fmt.Errorf("this snapshot does not contain a realm file")
}

// UserFiles are the export's user files, in order.
func (s *Session) UserFiles() []index.BuildInput {
	var out []index.BuildInput
	for _, name := range s.opened.RealmFiles {
		if !strings.Contains(name, "-users-") {
			continue
		}
		out = append(out, index.BuildInput{Name: name, Path: s.opened.Path(name)})
	}
	return out
}

// Index returns the user index, building it on first use.
//
// The build is where Tier 2 begins, and it is deliberately not done at open: an
// operator who only wanted to check the completeness report should not pay for
// a 120,000-user index.
func (s *Session) Index(ctx context.Context, rep *obs.Reporter) (*index.Index, error) {
	s.mu.Lock()
	if s.idx != nil {
		defer s.mu.Unlock()
		return s.idx, nil
	}
	if s.indexing {
		s.mu.Unlock()
		return nil, resil.Retry("build the inspection index",
			"The index for this snapshot is still being built.", nil)
	}
	s.indexing = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.indexing = false
		s.mu.Unlock()
	}()

	if rep == nil {
		rep = obs.NewReporter(s.ID, obs.NopSink{})
	}
	rep.StartPhase(obs.PhaseIndex)

	// A small realm is indexed entirely in memory and never touches disk, which
	// removes the residue question for the common case.
	opts := index.Options{}
	if s.Manifest.Counts.Users > index.InMemoryThreshold {
		opts.Path = s.home.IndexFile(s.ID)
	}
	total := s.Manifest.Counts.Users
	opts.Progress = func(indexed int) {
		rep.Progress(int64(indexed), int64(total), "users", "")
	}

	idx, err := index.Open(opts)
	if err != nil {
		return nil, err
	}

	providers := map[string]string{}
	if rep2, err := s.Representation(); err == nil {
		for _, c := range rep2.Components[realm.ComponentUserStorageProvider] {
			providers[c.ID] = "LDAP · " + c.Name
			providers[c.Name] = "LDAP · " + c.Name
		}
	}

	inputs := s.UserFiles()
	if len(inputs) == 0 {
		// realm_file mode keeps users inline, so the realm file is the input.
		for _, name := range s.opened.RealmFiles {
			if strings.HasSuffix(name, "-realm.json") {
				inputs = append(inputs, index.BuildInput{Name: name, Path: s.opened.Path(name)})
			}
		}
	}

	if err := idx.Build(ctx, inputs, providers, opts); err != nil {
		// A cancelled or failed build deletes its partial file rather than
		// leaving results that look complete.
		_ = idx.Close()
		return nil, err
	}

	s.mu.Lock()
	s.idx = idx
	s.mu.Unlock()

	rep.CompletePhase(obs.PhaseIndex, fmt.Sprintf("%d users indexed.", idx.Counts().Users))
	return idx, nil
}

// UserDetail is one account, read on demand from the decrypted bundle rather
// than projected into the index.
type UserDetail struct {
	index.UserRow
	Attributes          map[string][]string       `json:"attributes,omitempty"`
	RealmRoles          []string                  `json:"realmRoles,omitempty"`
	ClientRoles         map[string][]string       `json:"clientRoles,omitempty"`
	FederatedIdentities []realm.FederatedIdentity `json:"federatedIdentities,omitempty"`
	// Credentials is presence and metadata. There is no action anywhere that
	// would reveal a credential value for a user.
	Credentials []CredentialPresence `json:"credentials"`
}

// CredentialPresence describes one credential without disclosing it.
type CredentialPresence struct {
	Type       string `json:"type"`
	Label      string `json:"label,omitempty"`
	Algorithm  string `json:"algorithm,omitempty"`
	Iterations int    `json:"iterations,omitempty"`
	Created    string `json:"created,omitempty"`
}

// User returns one account's full detail.
func (s *Session) User(ctx context.Context, id string) (UserDetail, error) {
	idx, err := s.Index(ctx, nil)
	if err != nil {
		return UserDetail{}, err
	}
	row, err := idx.User(ctx, id)
	if err != nil {
		return UserDetail{}, err
	}

	detail := UserDetail{UserRow: row}
	detail.RealmRoles, detail.ClientRoles, err = idx.RolesFor(ctx, id)
	if err != nil {
		return UserDetail{}, err
	}

	// The full record is re-read from the bundle, which is why the index does
	// not carry attributes: they can hold configuration secrets, and a
	// projection of them would be a second copy.
	path := s.opened.Path(row.SourceFile)
	ordinal := 0
	found := false
	_, err = realm.StreamUsersFile(ctx, path, func(u realm.User) error {
		if found {
			return nil
		}
		if ordinal == row.SourceIndex || (u.ID != "" && u.ID == id) {
			detail.Attributes = u.Attributes
			detail.FederatedIdentities = u.FederatedIdentities
			for _, c := range u.Credentials {
				p := CredentialPresence{Type: c.Type, Label: c.UserLabel}
				if strings.EqualFold(c.Type, realm.CredentialPassword) {
					p.Algorithm = row.PasswordAlgorithm
					p.Iterations = row.PasswordIterations
				}
				if c.CreatedDate > 0 {
					p.Created = time.UnixMilli(c.CreatedDate).UTC().Format(time.RFC3339)
				}
				detail.Credentials = append(detail.Credentials, p)
			}
			found = true
		}
		ordinal++
		return nil
	})
	if err != nil {
		return UserDetail{}, err
	}
	return detail, nil
}

// Close destroys the index and shreds the decrypted working files.
//
// Closing is a visible action rather than an implicit consequence of navigating
// away, because "is that copy of my user directory gone?" deserves a definite
// answer.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	idx := s.idx
	s.idx = nil
	s.mu.Unlock()

	var errs []error
	if idx != nil {
		errs = append(errs, idx.Close())
	}
	if s.opened != nil {
		errs = append(errs, s.opened.Close())
	}
	// The whole working directory goes, not just the extracted contents.
	errs = append(errs, os.RemoveAll(s.home.WorkPath("open-"+s.ID, "")))
	return errors.Join(errs...)
}

// SweepIndexes removes index files a crash left behind.
//
// Deleting the whole index directory at any moment is always safe, which is
// what makes this a sweep rather than a reconciliation.
func SweepIndexes(home config.Home, keep map[string]bool) (removed int, bytes int64, err error) {
	entries, err := os.ReadDir(home.IndexDir())
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".sqlite")
		if keep[id] {
			continue
		}
		path := filepath.Join(home.IndexDir(), e.Name())
		if info, statErr := e.Info(); statErr == nil {
			bytes += info.Size()
		}
		if rmErr := os.Remove(path); rmErr != nil {
			err = rmErr
			continue
		}
		removed++
	}
	return removed, bytes, err
}

// SweepWorkDirs removes decrypted working directories a crash left behind.
func SweepWorkDirs(home config.Home, keep map[string]bool) (removed int, err error) {
	entries, err := os.ReadDir(home.WorkDir())
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		id := strings.TrimPrefix(name, "open-")
		if keep[id] || keep[name] {
			continue
		}
		if rmErr := os.RemoveAll(filepath.Join(home.WorkDir(), name)); rmErr != nil {
			err = rmErr
			continue
		}
		removed++
	}
	return removed, err
}

// marshalIndent is the shared encoder for exports, kept here so every export
// path produces the same shape.
func marshalIndent(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }
