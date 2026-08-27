// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package inspect_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/crypto"
	"portcloak/internal/engine/inspect"
	"portcloak/internal/engine/inspect/index"
	"portcloak/internal/engine/manifest"
	"portcloak/internal/engine/obs"
	"portcloak/internal/engine/realm"
	"portcloak/internal/engine/snapshot"
	"portcloak/internal/engine/store"
	"portcloak/internal/engine/store/disk"
)

const richFixture = "../../../testdata/exports/rich"

// seed builds a real snapshot into a disk store, so inspection is exercised
// against the same artifact a capture produces rather than a hand-made one.
func seed(t *testing.T, enc crypto.Config) (config.Home, *disk.Store, config.Storage, inspect.OpenRequest) {
	t.Helper()

	home := config.Home{Root: t.TempDir()}
	if err := home.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	blobs, err := disk.New(root)
	if err != nil {
		t.Fatal(err)
	}
	stCfg := config.Storage{Name: "local-disk", Kind: config.StoreDisk, Folder: root}

	builder, err := snapshot.NewBuilder(filepath.Join(t.TempDir(), "stage"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	entries, err := os.ReadDir(richFixture)
	if err != nil {
		t.Fatal(err)
	}
	var userFiles []string
	realmFile := ""
	for _, e := range entries {
		src, err := os.Open(filepath.Join(richFixture, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		name := snapshot.RealmDir + e.Name()
		if _, err := builder.Stage(ctx, name, src); err != nil {
			t.Fatal(err)
		}
		_ = src.Close()
		switch {
		case strings.HasSuffix(e.Name(), "-realm.json"):
			realmFile = filepath.Join(builder.Dir(), filepath.FromSlash(name))
		case strings.Contains(e.Name(), "-users-"):
			userFiles = append(userFiles, filepath.Join(builder.Dir(), filepath.FromSlash(name)))
		}
	}

	rep, err := realm.Load(realmFile)
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Build(ctx, rep, manifest.BuildOptions{
		Source: manifest.Source{
			EnvironmentName: "laptop", Kind: "local", KeycloakVersion: "25.0.2",
			CaptureMode: "offline-export", ExecutionMode: "in-place",
			SecretVerification: "skipped", DependencyScan: "skipped",
		},
		UserFiles:         userFiles,
		DependencyScanRan: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Document(snapshot.ManifestPath, m); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Document(snapshot.ProvenancePath, snapshot.Provenance{
		EnvironmentName: "laptop", EnvironmentKind: "local",
		CaptureMode: "offline-export", ExecutionMode: "in-place",
		SecretVerification: "skipped", DependencyScan: "completed",
	}); err != nil {
		t.Fatal(err)
	}
	tree := builder.Tree()
	if _, err := builder.Document(snapshot.IntegrityPath, tree); err != nil {
		t.Fatal(err)
	}

	sealer, err := crypto.NewSealer(enc)
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 8, 27, 9, 14, 0, 0, time.UTC)
	envelope := snapshot.Envelope{
		SchemaVersion: snapshot.SchemaVersion, SnapshotID: "01HZY3", Realm: "acme",
		CreatedAt: createdAt, PortCloakVersion: "0.0.1-test", KeycloakVersion: "25.0.2",
		IntegrityRoot: tree.Root, ArtifactCount: len(tree.Artifacts),
	}
	if sealer != nil {
		envelope.Encryption = sealer.Describe()
	}
	if _, err := builder.Document(snapshot.EnvelopePath, envelope); err != nil {
		t.Fatal(err)
	}

	layout := store.NewLayout("")
	bundleKey := layout.BundleKey("acme", createdAt, "01HZY3")
	bundlePath := filepath.Join(t.TempDir(), "bundle.pck")
	f, err := os.Create(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	var sealed snapshot.SealResult
	if sealer != nil {
		sealed, err = builder.Seal(ctx, f, sealer)
	} else {
		sealed, err = builder.Seal(ctx, f, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	src, err := os.Open(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := blobs.Put(ctx, bundleKey, src, store.PutOptions{Digest: sealed.Digest}); err != nil {
		t.Fatal(err)
	}
	_ = src.Close()

	sidecar := m.BuildSidecar("01HZY3", createdAt.Format(time.RFC3339), "0.0.1-test",
		envelope.Encryption.Enabled, string(envelope.Encryption.Mode), sealed.Size, sealed.Root)
	b, err := json.MarshalIndent(sidecar, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := blobs.Put(ctx, layout.ManifestKey("acme", createdAt, "01HZY3"),
		strings.NewReader(string(b)), store.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := blobs.Put(ctx, layout.DigestKey("acme", createdAt, "01HZY3"),
		strings.NewReader(sealed.Digest+"\n"), store.PutOptions{}); err != nil {
		t.Fatal(err)
	}

	req := inspect.OpenRequest{
		Storage: stCfg.Name, BundleKey: bundleKey, SnapshotID: "01HZY3",
		Passphrase: enc.Passphrase,
	}
	return home, blobs, stCfg, req
}

func openSession(t *testing.T, enc crypto.Config) *inspect.Session {
	t.Helper()
	home, blobs, _, req := seed(t, enc)
	s, err := inspect.Open(context.Background(), home, blobs, req, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// The whole library is browsable with no key at all.
func TestLibrary_AllBackendsNoKey(t *testing.T) {
	_, blobs, stCfg, _ := seed(t, crypto.Config{
		Enabled: true, Mode: snapshot.EncryptionPassphrase, Passphrase: "a passphrase",
	})

	lib := inspect.BuildLibrary(context.Background(), []config.Storage{stCfg},
		func(config.Storage) (store.BlobStore, error) { return blobs, nil })

	if len(lib.Entries) != 1 {
		t.Fatalf("library has %d entries: %+v", len(lib.Entries), lib)
	}
	e := lib.Entries[0]
	if !e.MetadataReadable {
		t.Fatalf("the sidecar was not readable: %s", e.MetadataNote)
	}
	if e.Realm != "acme" || e.Users != 5 || e.Clients != 4 {
		t.Errorf("entry = %+v", e)
	}
	if !e.Encrypted || e.EncryptionMode != string(snapshot.EncryptionPassphrase) {
		t.Errorf("encryption state not surfaced: %+v", e)
	}
	if !e.TokenContinuity {
		t.Error("token continuity should be visible without opening anything")
	}
	if !strings.Contains(lib.Summary(), "needs no decryption key") {
		t.Errorf("the summary should say the listing needs no key: %q", lib.Summary())
	}
}

// An unencrypted bundle is labelled unmissably everywhere it appears.
func TestLibrary_UnencryptedBundleIsLabelled(t *testing.T) {
	_, blobs, stCfg, _ := seed(t, crypto.Config{})

	lib := inspect.BuildLibrary(context.Background(), []config.Storage{stCfg},
		func(config.Storage) (store.BlobStore, error) { return blobs, nil })
	if lib.Entries[0].Encrypted {
		t.Fatal("the bundle was written unencrypted but reports otherwise")
	}
	warning := lib.Entries[0].Warning
	if warning == "" {
		t.Fatal("an unencrypted bundle carries no warning in the library")
	}
	for _, want := range []string{"client secrets", "signing keys", "clear"} {
		if !strings.Contains(warning, want) {
			t.Errorf("the warning does not say what is at stake (%q): %q", want, warning)
		}
	}
}

// A storage that could not be read is shown as unreachable rather than having
// its snapshots quietly omitted.
func TestLibrary_UnreachableStorageIsNotSilentlyShort(t *testing.T) {
	lib := inspect.BuildLibrary(context.Background(),
		[]config.Storage{{Name: "gone", Kind: config.StoreDisk, Folder: "/nowhere"}},
		func(config.Storage) (store.BlobStore, error) {
			return nil, errors.New("the endpoint is unreachable")
		})

	if len(lib.Storages) != 1 || lib.Storages[0].Reachable {
		t.Fatalf("storages = %+v", lib.Storages)
	}
	if lib.Storages[0].Error == "" {
		t.Error("an unreachable storage should say why")
	}
	if !strings.Contains(lib.Summary(), "may be short") {
		t.Errorf("the summary should warn that the list is incomplete: %q", lib.Summary())
	}
}

func TestOpen_Tier1_ReadsFullDetail(t *testing.T) {
	s := openSession(t, crypto.Config{})

	if s.Realm != "acme" || s.ID != "01HZY3" {
		t.Fatalf("session = %+v", s)
	}
	if !s.Verify.OK {
		t.Fatalf("a freshly sealed snapshot did not verify: %s", s.Verify.Message)
	}
	if s.Degraded() {
		t.Error("a good snapshot reported itself degraded")
	}
	if s.Manifest.Counts.Users != 5 {
		t.Errorf("manifest counts %d users", s.Manifest.Counts.Users)
	}
	if s.Provenance.CaptureMode != "offline-export" {
		t.Errorf("provenance = %+v", s.Provenance)
	}
	continuity, sentence := s.Manifest.TokenContinuity()
	if !continuity || !strings.Contains(sentence, "abc123") {
		t.Errorf("token continuity = %v / %q", continuity, sentence)
	}
}

func TestOpen_EncryptedRoundTripAndWrongKey(t *testing.T) {
	enc := crypto.Config{Enabled: true, Mode: snapshot.EncryptionPassphrase, Passphrase: "right"}
	home, blobs, _, req := seed(t, enc)

	s, err := inspect.Open(context.Background(), home, blobs, req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Realm != "acme" {
		t.Fatalf("realm = %q", s.Realm)
	}
	_ = s.Close()

	wrong := req
	wrong.Passphrase = "wrong"
	if _, err := inspect.Open(context.Background(), home, blobs, wrong, nil); err == nil {
		t.Fatal("a wrong passphrase opened the snapshot")
	} else if !strings.Contains(err.Error(), "encrypted") && !strings.Contains(err.Error(), "decrypt") {
		t.Errorf("the failure is not a plain message: %v", err)
	}

	none := req
	none.Passphrase = ""
	if _, err := inspect.Open(context.Background(), home, blobs, none, nil); err == nil {
		t.Fatal("an encrypted snapshot opened with no key")
	}
}

// Closing destroys the index and shreds the decrypted working files. "Is that
// copy of my user directory gone?" deserves a definite answer.
func TestSession_CloseDestroysTheIndexAndWorkingFiles(t *testing.T) {
	home, blobs, _, req := seed(t, crypto.Config{})
	s, err := inspect.Open(context.Background(), home, blobs, req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Index(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	workDir := home.WorkPath("open-01HZY3", "")
	if _, err := os.Stat(workDir); err != nil {
		t.Fatalf("the working directory should exist while the snapshot is open: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Fatal("decrypted realm material outlived the session")
	}
	entries, err := os.ReadDir(home.IndexDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sqlite") {
			t.Errorf("an index file survived close: %s", e.Name())
		}
	}
	// Closing twice is a no-op rather than an error, because the UI closes on
	// navigate-away and on quit.
	if err := s.Close(); err != nil {
		t.Fatalf("a second close failed: %v", err)
	}
}

// A crash prevents both close paths, so the next launch sweeps.
func TestSweep_RemovesIndexesAndWorkDirsACrashLeftBehind(t *testing.T) {
	home := config.Home{Root: t.TempDir()}
	if err := home.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"01HZY3.sqlite", "01HZY4.sqlite"} {
		if err := os.WriteFile(filepath.Join(home.IndexDir(), name), []byte("index"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(home.WorkPath("open-01HZY3", "contents"), 0o700); err != nil {
		t.Fatal(err)
	}

	// A snapshot still open is kept; everything else goes.
	removed, _, err := inspect.SweepIndexes(home, map[string]bool{"01HZY4": true})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("swept %d indexes, want 1", removed)
	}
	if _, err := os.Stat(filepath.Join(home.IndexDir(), "01HZY4.sqlite")); err != nil {
		t.Error("the sweep removed an index belonging to an open snapshot")
	}

	dirs, err := inspect.SweepWorkDirs(home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dirs != 1 {
		t.Fatalf("swept %d working directories, want 1", dirs)
	}
}

func TestIndex_SearchPageAndFacets(t *testing.T) {
	s := openSession(t, crypto.Config{})
	ctx := context.Background()

	idx, err := s.Index(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if idx.Counts().Users != 5 {
		t.Fatalf("indexed %d users, want 5", idx.Counts().Users)
	}

	page, err := idx.Users(ctx, index.UserFilter{Query: "okafor"}, index.SortUsername, false, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Rows[0].Username != "r.okafor" {
		t.Fatalf("search returned %+v", page)
	}
	// Credential presence, and nothing more.
	row := page.Rows[0]
	if !row.HasPassword || row.PasswordAlgorithm != "pbkdf2-sha512" {
		t.Errorf("password metadata = %+v", row)
	}
	if row.WebAuthnCount != 2 || !row.RecoveryCodes {
		t.Errorf("passkey and recovery-code presence = %+v", row)
	}
	if row.SecondFactor != "passkey" {
		t.Errorf("second factor = %q", row.SecondFactor)
	}

	// Search by email works too, which is what an operator actually types.
	byEmail, err := idx.Users(ctx, index.UserFilter{Query: "m.klein@acme"}, index.SortUsername, false, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if byEmail.Total != 1 {
		t.Fatalf("email search returned %d", byEmail.Total)
	}

	facets, err := idx.Facets(ctx, index.UserFilter{})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, f := range facets.Status {
		counts[f.Value] = f.Count
	}
	if counts["true"] != 4 || counts["false"] != 1 {
		t.Errorf("status facet = %+v", facets.Status)
	}

	// The origin facet is what makes LDAP-federated users visible, which
	// matters because they need their directory reachable at the destination.
	originCounts := map[string]int{}
	for _, f := range facets.Origin {
		originCounts[f.Value] = f.Count
	}
	if originCounts["LDAP · corp"] != 2 {
		t.Errorf("origin facet = %+v", facets.Origin)
	}
	if originCounts["service account"] != 1 {
		t.Errorf("a service account should be told apart from a local user: %+v", facets.Origin)
	}

	// Facet counts stay visible for the dimension being filtered on, so a
	// selection can be undone.
	enabled := true
	withFilter, err := idx.Facets(ctx, index.UserFilter{Enabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range withFilter.Status {
		if f.Value == "false" && f.Count == 0 {
			t.Error("filtering on enabled zeroed the disabled count, making the filter impossible to undo")
		}
	}
}

func TestIndex_FacetsIntersectWithSearch(t *testing.T) {
	s := openSession(t, crypto.Config{})
	ctx := context.Background()
	idx, err := s.Index(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	page, err := idx.Users(ctx, index.UserFilter{Origin: "LDAP · corp", SecondFactor: "otp"},
		index.SortUsername, false, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 {
		t.Fatalf("filters did not intersect: %d", page.Total)
	}

	group, err := idx.Users(ctx, index.UserFilter{Group: "/Everyone/Engineering"}, index.SortUsername, false, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if group.Total != 2 {
		t.Fatalf("group filter matched %d", group.Total)
	}

	role, err := idx.Users(ctx, index.UserFilter{RealmRole: "admin"}, index.SortUsername, false, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if role.Total != 1 {
		t.Fatalf("realm role filter matched %d", role.Total)
	}
}

// Adding a secret column has to require deliberately editing a test that says
// not to.
func TestIndexSchemaHasNoSecretColumns(t *testing.T) {
	s := openSession(t, crypto.Config{})
	ctx := context.Background()
	idx, err := s.Index(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	tables, err := idx.Tables(ctx)
	if err != nil {
		t.Fatal(err)
	}
	allowed := index.AllowedColumns()
	if len(tables) != len(allowed) {
		t.Fatalf("the index has %d tables but %d are allowlisted: %v", len(tables), len(allowed), tables)
	}

	for _, table := range tables {
		cols, err := idx.Columns(ctx, table)
		if err != nil {
			t.Fatal(err)
		}
		want, ok := allowed[table]
		if !ok {
			t.Errorf("the index has a table nobody allowlisted: %s", table)
			continue
		}
		if strings.Join(cols, ",") != strings.Join(want, ",") {
			t.Errorf("%s has columns %v, allowlist says %v", table, cols, want)
		}
		for _, c := range cols {
			if err := index.CheckColumn(table, c); err != nil {
				t.Errorf("%v", err)
			}
		}
	}

	// The guard itself has to work, or the assertion above proves nothing.
	for _, bad := range []string{"password_hash", "otp_seed", "client_secret", "credential_data", "attribute_value"} {
		if err := index.CheckColumn("users", bad); err == nil {
			t.Errorf("a column called %q would have been accepted", bad)
		}
	}
}

// The index is a copy of a user directory, so what it holds is worth asserting
// directly, not only by column name.
func TestIndex_HoldsNoSecretMaterial(t *testing.T) {
	home, blobs, _, req := seed(t, crypto.Config{})
	s, err := inspect.Open(context.Background(), home, blobs, req, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A large-enough realm forces the on-disk path, which is the one that could
	// leave residue.
	s.Manifest.Counts.Users = index.InMemoryThreshold + 1
	idx, err := s.Index(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	path := idx.Path()
	if path == "" {
		t.Fatal("expected an on-disk index for a large realm")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("the index file has mode %o", info.Mode().Perm())
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"T1RQU0VDUkVU", "aGFzaA==", "app-web-real-secret", "ldap-bind-password"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("the index file contains %q", forbidden)
		}
	}
	_ = s.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the index file survived close")
	}
}

// Single-user detail is re-read from the bundle rather than projected, so the
// index never becomes a second copy of the attributes.
func TestUserDetail_IsCredentialPresenceOnly(t *testing.T) {
	s := openSession(t, crypto.Config{})
	ctx := context.Background()
	idx, err := s.Index(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	page, err := idx.Users(ctx, index.UserFilter{Query: "j.doe"}, index.SortUsername, false, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := s.User(ctx, page.Rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(detail.Credentials) != 3 {
		t.Fatalf("got %d credentials, want 3", len(detail.Credentials))
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	// Presence and metadata; never a value.
	for _, forbidden := range []string{"secretData", "T1RQU0VDUkVU", "aGFzaA==", "c2FsdA=="} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("user detail leaked %q", forbidden)
		}
	}
	if !strings.Contains(string(encoded), "pbkdf2-sha512") {
		t.Error("user detail should say which algorithm hashed the password")
	}
	if len(detail.RealmRoles) != 2 {
		t.Errorf("role mappings = %v", detail.RealmRoles)
	}
}

func TestLedger_ContainsNoValuesAndSaysWhatIsRevealable(t *testing.T) {
	s := openSession(t, crypto.Config{})
	ledger := s.Ledger()

	if len(ledger) == 0 {
		t.Fatal("the ledger is empty")
	}
	encoded, err := json.Marshal(ledger)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"app-web-real-secret", "ldap-bind-password", "smtp-password-value"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("the ledger contains %q", forbidden)
		}
	}

	byLocation := map[string]inspect.LedgerEntry{}
	for _, e := range ledger {
		byLocation[e.Location] = e
	}
	// A secret masked at source has nothing behind it, so offering Reveal would
	// promise something that cannot be delivered.
	if masked := byLocation["clients[legacy-portal].secret"]; masked.Revealable {
		t.Error("a secret masked at source was offered as revealable")
	}
	if real := byLocation["clients[app-web].secret"]; !real.Revealable {
		t.Error("a carried secret should be revealable")
	}
	if !strings.Contains(s.LedgerSummary(), "carried") {
		t.Errorf("ledger summary = %q", s.LedgerSummary())
	}
}

func TestReveal_WritesAuditAndNeverLogs(t *testing.T) {
	s := openSession(t, crypto.Config{})
	dir := t.TempDir()
	audit, err := obs.NewAuditLog(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}

	value, err := s.Reveal(context.Background(), inspect.RevealRequest{
		Location: "clients[app-web].secret", Reason: "ticket OPS-12",
	}, audit, true)
	if err != nil {
		t.Fatal(err)
	}
	if value != "app-web-real-secret" {
		t.Fatalf("reveal returned %q", value)
	}

	entries, err := audit.Read(obs.AuditFilter{Action: obs.ActionSecretReveal})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d audit entries", len(entries))
	}
	if !strings.Contains(entries[0].Detail, "clients[app-web].secret") {
		t.Errorf("the audit entry does not say what was revealed: %+v", entries[0])
	}
	if entries[0].Reason != "ticket OPS-12" {
		t.Errorf("the reason was not recorded: %q", entries[0].Reason)
	}

	// The revealed value never reaches the audit log itself.
	raw, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "app-web-real-secret") {
		t.Fatal("the audit log recorded the revealed value")
	}
}

func TestReveal_RefusalsAreExplained(t *testing.T) {
	s := openSession(t, crypto.Config{})
	ctx := context.Background()

	// Reveal switched off in preferences.
	if _, err := s.Reveal(ctx, inspect.RevealRequest{Location: "clients[app-web].secret"}, nil, false); err == nil {
		t.Error("reveal ran while switched off")
	} else if !strings.Contains(err.Error(), "switched off") {
		t.Errorf("unhelpful message: %v", err)
	}

	// A masked secret has no real value to show.
	_, err := s.Reveal(ctx, inspect.RevealRequest{Location: "clients[legacy-portal].secret"}, nil, true)
	if err == nil {
		t.Error("a masked secret was revealed")
	} else if !strings.Contains(err.Error(), "masked") {
		t.Errorf("unhelpful message: %v", err)
	}

	// A location that is not in the ledger.
	if _, err := s.Reveal(ctx, inspect.RevealRequest{Location: "clients[nope].secret"}, nil, true); err == nil {
		t.Error("an unknown location was revealed")
	}
}

// Revealing from an unencrypted snapshot must not imply the reveal added
// protection that was never there.
func TestReveal_FromAnUnencryptedSnapshotSaysSo(t *testing.T) {
	s := openSession(t, crypto.Config{})
	note := s.RevealNote()
	if !strings.Contains(note, "not encrypted") {
		t.Errorf("the note should say the value was already in the clear: %q", note)
	}
}

func TestExport_IsRedactedAndAudited(t *testing.T) {
	s := openSession(t, crypto.Config{})
	ctx := context.Background()
	dir := t.TempDir()
	audit, err := obs.NewAuditLog(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}

	views := []inspect.ExportView{
		inspect.ExportUsers, inspect.ExportClients, inspect.ExportSecretLedger,
		inspect.ExportCompleteness, inspect.ExportDependencies, inspect.ExportKeys,
	}
	for _, view := range views {
		for _, format := range []inspect.ExportFormat{inspect.FormatCSV, inspect.FormatJSON} {
			path := filepath.Join(dir, fmt.Sprintf("%s.%s", view, format))
			res, err := s.Export(ctx, inspect.ExportRequest{View: view, Format: format, Path: path}, audit)
			if err != nil {
				t.Fatalf("%s as %s: %v", view, format, err)
			}
			if res.Rows == 0 && view != inspect.ExportDependencies {
				t.Errorf("%s as %s exported no rows", view, format)
			}

			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{
				"app-web-real-secret", "ldap-bind-password", "smtp-password-value",
				"azure-secret-value", "T1RQU0VDUkVU", "aGFzaA==",
			} {
				if strings.Contains(string(raw), forbidden) {
					t.Errorf("%s as %s contains %q", view, format, forbidden)
				}
			}
		}
	}

	entries, err := audit.Read(obs.AuditFilter{Action: obs.ActionExportView})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(views)*2 {
		t.Errorf("got %d export audit entries, want %d", len(entries), len(views)*2)
	}
}

func TestExport_UsersCarriesPresenceNotValues(t *testing.T) {
	s := openSession(t, crypto.Config{})
	path := filepath.Join(t.TempDir(), "users.csv")
	if _, err := s.Export(context.Background(), inspect.ExportRequest{
		View: inspect.ExportUsers, Format: inspect.FormatCSV, Path: path,
	}, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	header := strings.SplitN(string(raw), "\n", 2)[0]
	for _, want := range []string{"hasPassword", "passwordAlgorithm", "otpEnrolments", "passkeys"} {
		if !strings.Contains(header, want) {
			t.Errorf("the export is missing the %q column: %s", want, header)
		}
	}
	for _, forbidden := range []string{"hash", "seed", "secret"} {
		if strings.Contains(strings.ToLower(header), forbidden) {
			t.Errorf("the export has a column that sounds like a value: %s", header)
		}
	}
}

func TestExport_UnwritableDestinationNamesThePath(t *testing.T) {
	s := openSession(t, crypto.Config{})
	_, err := s.Export(context.Background(), inspect.ExportRequest{
		View: inspect.ExportClients, Format: inspect.FormatCSV,
		Path: "/proc/definitely-not-writable/users.csv",
	}, nil)
	if err == nil {
		t.Fatal("an unwritable destination was accepted")
	}
	if !strings.Contains(err.Error(), "definitely-not-writable") {
		t.Errorf("the failure does not name the path: %v", err)
	}
}

func TestVerify_ReportsPerArtifactAndContactsNothing(t *testing.T) {
	s := openSession(t, crypto.Config{})
	r := s.VerifyReport()

	if !r.OK {
		t.Fatalf("a good snapshot did not verify: %s", r.Message)
	}
	if len(r.Artifacts) == 0 {
		t.Fatal("verification reported no artifacts")
	}
	for _, a := range r.Artifacts {
		if !a.OK {
			t.Errorf("%s failed: %s", a.Name, a.Note)
		}
	}
	if !strings.Contains(r.Note, "No environment was contacted") {
		t.Errorf("verification should say plainly that nothing was contacted: %q", r.Note)
	}
}

// A tampered bundle opens degraded rather than being presented as readable
// content, and names the artifact that failed.
func TestOpen_TamperedBundleOpensDegradedAndNamesTheArtifact(t *testing.T) {
	home, blobs, _, req := seed(t, crypto.Config{})

	// Flip a byte inside the bundle. Compression makes the corruption spread,
	// so the open may fail outright or verify as degraded; both are acceptable,
	// and what is not acceptable is a clean open.
	root := blobs.Root()
	path := filepath.Join(root, filepath.FromSlash(req.BundleKey))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)/2] ^= 0xff
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := inspect.Open(context.Background(), home, blobs, req, nil)
	if err != nil {
		return // Refused outright, which is the stronger outcome.
	}
	defer func() { _ = s.Close() }()
	if !s.Degraded() {
		t.Fatal("a tampered bundle opened as if it were intact")
	}
	if len(s.Verify.Failures()) == 0 {
		t.Fatal("verification failed without naming an artifact")
	}
}

func TestSession_DependenciesAreTheSameRecordsAsTheManifest(t *testing.T) {
	s := openSession(t, crypto.Config{})
	deps := s.Dependencies()
	if len(deps) != len(s.Manifest.ExternalDependencies) {
		t.Fatal("the dependency view and the manifest disagree")
	}
	for _, d := range deps {
		if d.Action != manifest.ProvisionAction {
			t.Errorf("dependency %s has action %q", d.Name, d.Action)
		}
		if d.Consequence == "" {
			t.Errorf("dependency %s does not state the consequence of its absence", d.Name)
		}
	}
}

// Two snapshots open at once is the ordinary case — comparing a capture against
// the one before it is most of what the library is for — and the second one
// failed to index at all:
//
//	creating the index schema: SQL logic error: table users already exists (1)
//
// The on-disk index has always been one file per snapshot. The in-memory one,
// which is what a realm under the threshold gets, was a shared-cache database
// under a fixed name: process-wide, so the second snapshot opened the first
// one's database and found its schema already there. Worse than the error would
// have been the version that did not error and mixed two realms' users into one
// searchable table.
func TestIndex_IsOnePerSnapshot(t *testing.T) {
	ctx := context.Background()

	first := openSession(t, crypto.Config{})
	defer first.Close() //nolint:errcheck
	second := openSession(t, crypto.Config{})
	defer second.Close() //nolint:errcheck

	a, err := first.Index(ctx, nil)
	if err != nil {
		t.Fatalf("the first snapshot could not be indexed: %v", err)
	}
	b, err := second.Index(ctx, nil)
	if err != nil {
		t.Fatalf("a second snapshot open at the same time could not be indexed: %v", err)
	}

	if a.Counts().Users != b.Counts().Users {
		t.Fatalf("the same fixture indexed differently: %d and %d", a.Counts().Users, b.Counts().Users)
	}
	// Each holds its own realm's users once, not both realms' twice.
	page, err := b.Users(ctx, index.UserFilter{}, index.SortUsername, false, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != b.Counts().Users {
		t.Errorf("the second index holds %d users but counted %d; two snapshots are sharing one table",
			page.Total, b.Counts().Users)
	}

	// And closing one leaves the other usable, which a shared database would
	// not survive either.
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Users(ctx, index.UserFilter{}, index.SortUsername, false, 0, 10); err != nil {
		t.Errorf("closing one snapshot's index broke another's: %v", err)
	}
}
