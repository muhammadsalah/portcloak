package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	home := Home{Root: t.TempDir()}
	if err := home.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	s := NewStore(home)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestHome_BootstrapIsIdempotentAndSelfHealing(t *testing.T) {
	home := Home{Root: filepath.Join(t.TempDir(), "nested", ".portcloak")}
	if err := home.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	for _, d := range home.Dirs() {
		st, err := os.Stat(d)
		if err != nil {
			t.Fatalf("%s was not created: %v", d, err)
		}
		if got := st.Mode().Perm(); got != 0o700 {
			t.Errorf("%s has mode %o, want 700", d, got)
		}
	}
	cfgBefore, err := os.ReadFile(home.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}

	// Deleting a directory by hand must not brick the app.
	if err := os.RemoveAll(home.IndexDir()); err != nil {
		t.Fatal(err)
	}
	if err := home.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(home.IndexDir()); err != nil {
		t.Fatalf("index dir was not recreated: %v", err)
	}
	cfgAfter, err := os.ReadFile(home.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if string(cfgBefore) != string(cfgAfter) {
		t.Error("an existing config.yaml was rewritten by a second bootstrap")
	}
}

// A file an operator maintains by hand has to come back exactly as it went in,
// comments and all, or the tool is not safe to leave running next to it.
func TestConfigRoundTrip_IsByteStable(t *testing.T) {
	home := Home{Root: t.TempDir()}
	if err := home.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	original := `version: 1

# The laptop install I test against.
environments:
  - name: laptop
    kind: local
    serverFolder: /opt/keycloak   # where bin/kc.sh lives

storage:
  - name: local-disk
    kind: disk
    folder: ~/PortCloak/snapshots
    default: true

preferences:
  usersPerFile: 500
`
	if err := os.WriteFile(home.ConfigFile(), []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	s := NewStore(home)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(home.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("config.yaml was reformatted by a load/save cycle:\n--- want ---\n%s\n--- got ---\n%s", original, got)
	}
	if s.Preferences().UsersPerFile != 500 {
		t.Errorf("hand-edited preference was not picked up: %d", s.Preferences().UsersPerFile)
	}
}

// A file written by a newer build must not lose entries when an older one saves.
func TestConfig_UnknownFieldsSurviveAChange(t *testing.T) {
	home := Home{Root: t.TempDir()}
	if err := home.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	original := `version: 1
futureTopLevel: kept
environments:
  - name: laptop
    kind: local
    serverFolder: /opt/keycloak
    futureField: also-kept
storage: []
preferences: {}
`
	if err := os.WriteFile(home.ConfigFile(), []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(home)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	// Make a real change so the file is rewritten from the schema.
	if err := s.AddStorage(Storage{Name: "disk", Kind: StoreDisk, Folder: "/tmp/snaps"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(home.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"futureTopLevel: kept", "futureField: also-kept"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("a rewrite dropped %q:\n%s", want, got)
		}
	}
}

func TestConfig_MalformedFileNamesEveryProblemWithALine(t *testing.T) {
	home := Home{Root: t.TempDir()}
	if err := home.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	bad := `version: 1
environments:
  - name: a
    kind: telepathy
  - name: b
    kind: ssh
    host: kc-01
  - name: a
    kind: local
    serverFolder: /opt/keycloak
storage:
  - name: one
    kind: disk
    folder: /tmp/a
    default: true
  - name: two
    kind: disk
    folder: /tmp/b
    default: true
`
	if err := os.WriteFile(home.ConfigFile(), []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	err := NewStore(home).Load()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected a ValidationError, got %v", err)
	}
	if len(ve.Problems) < 5 {
		t.Fatalf("expected every problem at once, got %d:\n%s", len(ve.Problems), ve)
	}
	joined := ve.Error()
	for _, want := range []string{
		"not an environment kind",
		"no user to connect as",
		"does not say where Keycloak is installed",
		"both named",
		"marked default",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("problem list is missing %q:\n%s", want, joined)
		}
	}
	for _, p := range ve.Problems {
		if p.Line == 0 {
			t.Errorf("problem %q has no line number, so an operator cannot find it", p.Message)
		}
	}
}

func TestConfig_EnvironmentKinds_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	envs := []Environment{
		{Name: "laptop", Kind: EnvLocal, ServerFolder: "/opt/keycloak"},
		{Name: "kc-01", Kind: EnvSSH, Host: "kc-01.internal", Port: 22, User: "deploy",
			ServerFolder: "/opt/keycloak", Auth: SSHKey, CredentialRef: Handle("ssh", "kc-01"),
			JumpHost: &SSHHop{Host: "bastion", User: "jump", Auth: SSHAgent}},
		{Name: "compose", Kind: EnvDocker, DockerEndpoint: "unix:///var/run/docker.sock", Container: "keycloak"},
		{Name: "prod-eu", Kind: EnvKubernetes, Context: "prod", Namespace: "iam-prod", Workload: "statefulset/keycloak"},
	}
	for _, e := range envs {
		if err := s.AddEnvironment(e); err != nil {
			t.Fatalf("adding %s: %v", e.Name, err)
		}
	}

	reloaded := NewStore(s.Home())
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	cfg := reloaded.Config()
	if len(cfg.Environments) != len(envs) {
		t.Fatalf("got %d environments, want %d", len(cfg.Environments), len(envs))
	}
	for i, want := range envs {
		got := cfg.Environments[i]
		if got.Name != want.Name || got.Kind != want.Kind || got.Target() != want.Target() {
			t.Errorf("environment %d round-tripped as %+v, want %+v", i, got, want)
		}
	}
	if cfg.Environments[1].JumpHost == nil || cfg.Environments[1].JumpHost.Host != "bastion" {
		t.Error("the jump host did not survive the round trip")
	}
}

func TestConfig_StorageKinds_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	stores := []Storage{
		{Name: "local-disk", Kind: StoreDisk, Folder: "/tmp/snaps"},
		{Name: "backup-host", Kind: StoreSSH, Host: "backup", User: "deploy", Folder: "/srv/snaps"},
		{Name: "prod-backups", Kind: StoreS3, Endpoint: "s3.eu-west-1.amazonaws.com", Region: "eu-west-1",
			Bucket: "iam-snapshots", Prefix: "portcloak/", EncryptionRequired: true},
		{Name: "azure", Kind: StoreAzure, Account: "iamsnaps", Container: "snapshots", Prefix: "portcloak/"},
	}
	for _, st := range stores {
		if err := s.AddStorage(st); err != nil {
			t.Fatalf("adding %s: %v", st.Name, err)
		}
	}
	reloaded := NewStore(s.Home())
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	cfg := reloaded.Config()
	if len(cfg.Storage) != 4 {
		t.Fatalf("got %d storage definitions, want 4", len(cfg.Storage))
	}
	if cfg.Storage[2].Prefix != "portcloak/" || !cfg.Storage[2].EncryptionRequired {
		t.Errorf("the S3 prefix and encryption-required flag did not survive: %+v", cfg.Storage[2])
	}
	if got := cfg.Storage[3].Root(); got != "snapshots/portcloak" {
		t.Errorf("azure root rendered as %q", got)
	}
}

func TestConfig_DefaultStorageIsExclusive(t *testing.T) {
	s := newTestStore(t)
	for _, n := range []string{"a", "b", "c"} {
		if err := s.AddStorage(Storage{Name: n, Kind: StoreDisk, Folder: "/tmp/" + n}); err != nil {
			t.Fatal(err)
		}
	}
	// The first storage defined becomes the default on its own.
	if d, ok := s.Config().DefaultStorage(); !ok || d.Name != "a" {
		t.Fatalf("expected the first storage to become default, got %+v (%v)", d, ok)
	}
	if err := s.SetDefaultStorage("c"); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, st := range s.Config().Storage {
		if st.Default {
			count++
			if st.Name != "c" {
				t.Errorf("wrong storage is default: %s", st.Name)
			}
		}
	}
	if count != 1 {
		t.Fatalf("%d storage definitions are default, want exactly 1", count)
	}
}

func TestConfig_CRUD(t *testing.T) {
	s := newTestStore(t)
	creds := NewMemoryCredentials()

	handle := Handle("ssh", "kc-01")
	if err := creds.Set(handle, "a-private-key"); err != nil {
		t.Fatal(err)
	}
	env := Environment{Name: "kc-01", Kind: EnvSSH, Host: "kc-01.internal", User: "deploy",
		ServerFolder: "/opt/keycloak", Auth: SSHKey, CredentialRef: handle}
	if err := s.AddEnvironment(env); err != nil {
		t.Fatal(err)
	}

	// Editing clears the tested stamp: an edited environment is untested by
	// definition.
	if err := s.RecordEnvironmentProbe("kc-01", ProbeStamp{At: time.Now(), OK: true}); err != nil {
		t.Fatal(err)
	}
	edited := env
	edited.Host = "kc-02.internal"
	if err := s.SaveEnvironment("kc-01", edited); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Config().Environment("kc-01")
	if got.LastProbe != nil {
		t.Error("editing an environment kept its stale Tested OK stamp")
	}

	// Duplicating copies fields but never the credential.
	dup, err := s.DuplicateEnvironment("kc-01", "")
	if err != nil {
		t.Fatal(err)
	}
	if dup.CredentialRef != "" {
		t.Errorf("duplicate silently reused another environment's credential: %q", dup.CredentialRef)
	}
	if dup.Host != "kc-02.internal" {
		t.Errorf("duplicate lost a non-secret field: %+v", dup)
	}
	if dup.Name != "kc-01 copy" {
		t.Errorf("duplicate got the name %q", dup.Name)
	}

	// A running job blocks deletion.
	busy := JobLookup(func(kind, name string) (string, bool) {
		return "job-7", kind == "environment" && name == "kc-01"
	})
	var inUse *InUseError
	if err := s.DeleteEnvironment("kc-01", creds, busy); !errors.As(err, &inUse) {
		t.Fatalf("expected an in-use refusal, got %v", err)
	}

	// Deleting removes the entry and its keychain secret.
	if err := s.DeleteEnvironment("kc-01", creds, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Config().Environment("kc-01"); ok {
		t.Error("environment survived deletion")
	}
	if _, err := creds.Get(handle); !errors.Is(err, ErrCredentialMissing) {
		t.Errorf("the keychain secret outlived its environment: %v", err)
	}
}

func TestConfig_DeleteStorageKeepsTheDataAndGuardsTheDefault(t *testing.T) {
	s := newTestStore(t)
	if err := s.AddStorage(Storage{Name: "a", Kind: StoreDisk, Folder: "/tmp/a"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddStorage(Storage{Name: "b", Kind: StoreDisk, Folder: "/tmp/b"}); err != nil {
		t.Fatal(err)
	}
	var inUse *InUseError
	if err := s.DeleteStorage("a", nil, nil); !errors.As(err, &inUse) {
		t.Fatalf("deleting the default storage should require choosing another first, got %v", err)
	}
	if err := s.SetDefaultStorage("b"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteStorage("a", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Config().StorageByName("a"); ok {
		t.Error("storage survived deletion")
	}
}

// The whole point of the atomic write: a crash mid-save leaves the previous
// configuration intact rather than a truncated file.
func TestConfig_SaveIsAtomic(t *testing.T) {
	home := Home{Root: t.TempDir()}
	if err := home.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	s := NewStore(home)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.AddEnvironment(Environment{Name: "good", Kind: EnvLocal, ServerFolder: "/opt/keycloak"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(home.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a crash by rejecting the update partway through: nothing is
	// written, and every temp file is cleaned up.
	wantErr := errors.New("interrupted")
	if err := s.Update(func(c *Config) error {
		c.Environments = append(c.Environments, Environment{Name: "half", Kind: EnvLocal, ServerFolder: "/x"})
		return wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatalf("got %v", err)
	}
	after, err := os.ReadFile(home.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("an interrupted update changed config.yaml")
	}
	entries, err := os.ReadDir(home.Root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("a temporary file was left behind: %s", e.Name())
		}
	}
}

func TestConfig_ContainsNoSecretMaterial(t *testing.T) {
	s := newTestStore(t)
	creds := NewMemoryCredentials()
	handle := Handle("s3", "prod")
	if err := creds.Set(handle, "AKIAIOSFODNN7EXAMPLE/wJalrXUtnFEMI"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddStorage(Storage{Name: "prod", Kind: StoreS3, Bucket: "b", Region: "eu-west-1",
		Prefix: "portcloak/", CredentialRef: handle}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.Home().ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("config.yaml contains a secret:\n%s", raw)
	}
	if !strings.Contains(string(raw), HandleScheme) {
		t.Error("config.yaml should reference the credential by handle")
	}
}
