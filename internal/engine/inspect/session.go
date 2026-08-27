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
	// Either is an explicit override and is tried before anything stored.
	Passphrase string
	Identities []string
	// Candidates are the named keys already held in this machine's keychain.
	//
	// They are tried without being asked for, which is the whole point: a key
	// PortCloak generated, stored and can read is a key the operator has
	// already decided to trust it with, and asking for it again at every
	// restore is a prompt that teaches people to turn encryption off.
	Candidates []KeyCandidate
}

// KeyCandidate is one named key PortCloak may try on its own. Exactly one of
// Passphrase or Identity is set.
type KeyCandidate struct {
	Name       string
	Passphrase string
	Identity   string
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

	// UnlockedWith names the stored key that opened this snapshot without being
	// asked for, and is empty when the operator supplied the key themselves or
	// the bundle was never encrypted. A key used silently is still a key an
	// operator gets to see the name of.
	UnlockedWith string

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

	// The envelope says what kind of artifact this is, and reading it is also
	// what proves a key opens the bundle — so the opener that succeeded here is
	// the one used below rather than a second one built from the same material.
	_, opener, unlockedWith, err := unseal(ctx, bundlePath, req)
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
		ID:           opened.Envelope.SnapshotID,
		Realm:        opened.Envelope.Realm,
		Storage:      req.Storage,
		BundleKey:    req.BundleKey,
		OpenedAt:     time.Now(),
		Envelope:     opened.Envelope,
		Verify:       opened.Verify,
		UnlockedWith: unlockedWith,
		home:         home,
		opened:       opened,
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

// unseal reads the envelope, and in doing so establishes which key opens the
// bundle.
//
// Reading the envelope is the cheapest possible proof that a key works: it is
// the first document in the archive, so a wrong key fails here rather than
// after a multi-gigabyte extraction. That makes this the natural place to try
// more than one key, and the order is the one an operator would expect —
// nothing, then whatever they typed, then the keys they have already asked
// PortCloak to hold.
//
// The name of the stored key that worked is returned so the screen can say
// which one it was. A key used silently is still a key an operator gets to see
// the name of.
func unseal(ctx context.Context, path string, req OpenRequest) (snapshot.Envelope, *crypto.Opener, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return snapshot.Envelope{}, nil, "", err
	}
	defer f.Close() //nolint:errcheck

	try := func(opener *crypto.Opener) (snapshot.Envelope, bool) {
		if _, serr := f.Seek(0, 0); serr != nil {
			return snapshot.Envelope{}, false
		}
		// A nil *Opener has to reach the reader as a nil interface, not as a
		// non-nil interface holding a nil pointer, or "read it in the clear"
		// becomes a call through a nil receiver.
		var as snapshot.Opener
		if opener != nil {
			as = opener
		}
		e, rerr := snapshot.ReadEnvelopeOnly(ctx, f, as)
		return e, rerr == nil
	}

	// An unencrypted bundle yields its envelope directly.
	if e, ok := try(nil); ok {
		return e, nil, "", nil
	}

	// What the operator supplied, which is always an override: a key typed on
	// the screen beats a key PortCloak found for itself.
	if req.Passphrase != "" {
		if opener, oerr := crypto.NewPassphraseOpener(req.Passphrase); oerr == nil {
			if e, ok := try(opener); ok {
				return e, opener, "", nil
			}
		}
	}
	if len(req.Identities) > 0 {
		if opener, oerr := crypto.NewIdentityOpener(req.Identities); oerr == nil {
			if e, ok := try(opener); ok {
				return e, opener, "", nil
			}
		}
	}

	// Then the stored keys, one at a time rather than all at once, so the one
	// that worked can be named. Identities first: matching an age recipient
	// costs nothing, while every passphrase attempt pays scrypt's deliberate
	// second of work.
	for _, c := range candidatesInTryOrder(req.Candidates) {
		var opener *crypto.Opener
		var oerr error
		switch {
		case c.Identity != "":
			opener, oerr = crypto.NewIdentityOpener([]string{c.Identity})
		case c.Passphrase != "":
			opener, oerr = crypto.NewPassphraseOpener(c.Passphrase)
		default:
			continue
		}
		if oerr != nil {
			continue
		}
		if e, ok := try(opener); ok {
			return e, opener, c.Name, nil
		}
	}

	// The envelope could not be read at all, which for a well-formed bundle
	// means it is encrypted and nothing available opens it.
	return snapshot.Envelope{}, nil, "", resil.Fatal("open the snapshot",
		unopenableMessage(req), snapshot.ErrEncrypted).
		WithAdvice("Check the passphrase, or that you hold a private key matching one of the snapshot's recipients.")
}

// candidatesInTryOrder puts identities before passphrases, because an identity
// attempt is free and a passphrase attempt is not.
func candidatesInTryOrder(in []KeyCandidate) []KeyCandidate {
	out := make([]KeyCandidate, 0, len(in))
	for _, c := range in {
		if c.Identity != "" {
			out = append(out, c)
		}
	}
	for _, c := range in {
		if c.Identity == "" && c.Passphrase != "" {
			out = append(out, c)
		}
	}
	return out
}

// unopenableMessage says what was actually tried, because "wrong key" and
// "no key" are different problems with different fixes.
func unopenableMessage(req OpenRequest) string {
	supplied := req.Passphrase != "" || len(req.Identities) > 0
	switch {
	case supplied && len(req.Candidates) > 0:
		return fmt.Sprintf("This snapshot is encrypted. Neither the key supplied nor any of the %d key(s) stored on this machine opened it.",
			len(req.Candidates))
	case supplied:
		return "This snapshot is encrypted and could not be opened with the key supplied."
	case len(req.Candidates) > 0:
		return fmt.Sprintf("This snapshot is encrypted, and none of the %d key(s) stored on this machine opened it. Supply the key it was sealed with.",
			len(req.Candidates))
	default:
		return "This snapshot is encrypted and no key was supplied."
	}
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
	opts := index.Options{Name: s.ID}
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

// RealmFiles are the export artifacts this snapshot carries, in bundle-relative
// form.
func (s *Session) RealmFiles() []string { return s.opened.RealmFiles }

// PathOf resolves a bundle-relative artifact name to where it was extracted.
func (s *Session) PathOf(name string) string { return s.opened.Path(name) }

// RealmFileBytes returns the realm document verbatim.
//
// Verbatim matters: the realm JSON is what the destination consumes, and
// re-serialising it would put PortCloak in the path of the very data it
// promises to carry faithfully.
func (s *Session) RealmFileBytes() ([]byte, error) {
	for _, name := range s.opened.RealmFiles {
		base := strings.TrimPrefix(name, snapshot.RealmDir)
		if base == s.Realm+"-realm.json" || base == s.Realm+".json" {
			return os.ReadFile(s.opened.Path(name))
		}
	}
	for _, name := range s.opened.RealmFiles {
		if strings.HasSuffix(name, "-realm.json") {
			return os.ReadFile(s.opened.Path(name))
		}
	}
	return nil, fmt.Errorf("this snapshot does not contain a realm file")
}
