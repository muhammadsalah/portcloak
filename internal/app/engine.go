package app

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"portcloak/internal/engine/admin"
	"portcloak/internal/engine/config"
	"portcloak/internal/engine/inspect"
	"portcloak/internal/engine/obs"
	"portcloak/internal/engine/orchestrator"
	"portcloak/internal/engine/reliable"
	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/store"
	"portcloak/internal/engine/store/azurestore"
	"portcloak/internal/engine/store/disk"
	"portcloak/internal/engine/store/s3store"
	"portcloak/internal/engine/store/sftpstore"
	"portcloak/internal/engine/target"
	"portcloak/internal/engine/target/docker"
	"portcloak/internal/engine/target/k8s"
	"portcloak/internal/engine/target/local"
	sshtarget "portcloak/internal/engine/target/ssh"
)

// Engine holds everything the controllers share.
//
// It is a plain struct with no Wails types in it, so a test can build one
// against a temporary directory and drive every controller without a desktop
// runtime present.
type Engine struct {
	Version string
	// Build is the full identity of the running binary — version, commit
	// and compile date. Version above stays a plain string because it is
	// what goes into a snapshot manifest, and a manifest field is a
	// format promise that must not grow a struct underneath it.
	Build Build

	Config   *config.Store
	Jobs     *config.JobStore
	Creds    config.CredentialStore
	Log      *obs.Logger
	Audit    *obs.AuditLog
	Orch     *orchestrator.Orchestrator
	FirstRun bool

	// LoadError is the validation failure, if any, that stopped configuration
	// from loading. The app still starts: an operator with a malformed file
	// needs to be told which line to fix, which they cannot read from a window
	// that refused to open.
	LoadError error

	mu sync.RWMutex
	// home is guarded because it is the one thing about a running engine that
	// can change: the operator can move the PortCloak folder from Settings.
	home     config.Home
	sink     obs.Sink
	sessions map[string]*inspect.Session
	// unlocked holds the keys that have opened a snapshot during this run of
	// the application, in memory and nowhere else. See rememberKey.
	unlocked []inspect.KeyCandidate
	breakers *resil.Registry
}

// NewEngine bootstraps the PortCloak home, loads configuration and wires the
// adapter registry.
func NewEngine(version string) (*Engine, error) {
	home, err := config.DefaultHome()
	if err != nil {
		return nil, err
	}

	firstRun := false
	if _, statErr := os.Stat(home.ConfigFile()); os.IsNotExist(statErr) {
		firstRun = true
	}
	if err := home.Bootstrap(); err != nil {
		return nil, err
	}
	if err := home.Writable(); err != nil {
		return nil, err
	}

	logger, err := obs.NewLogger(obs.DefaultLogOptions(home.LogFile()))
	if err != nil {
		return nil, err
	}
	audit, err := obs.NewAuditLog(home.AuditFile())
	if err != nil {
		return nil, err
	}

	store := config.NewStore(home)
	loadErr := store.Load()

	eng := &Engine{
		Version:   version,
		Build:     NewBuild(version, "", ""),
		home:      home,
		Config:    store,
		Jobs:      config.NewJobStore(home),
		Creds:     config.NewKeychain(),
		Log:       logger,
		Audit:     audit,
		FirstRun:  firstRun,
		LoadError: loadErr,
		sink:      obs.NopSink{},
		sessions:  map[string]*inspect.Session{},
	}

	prefs := store.Preferences()
	eng.breakers = resil.NewRegistry(resil.BreakerConfig{
		Threshold: prefs.BreakerThreshold,
		Cooldown:  prefs.BreakerCooldown,
	}, nil)

	eng.Orch = orchestrator.New(orchestrator.Options{
		Home: home, Config: store, Jobs: eng.Jobs,
		Log: logger, Audit: audit, Version: version,
		Registry: orchestrator.Registry{
			Executor:    eng.executorFor,
			Store:       eng.storeFor,
			Verifier:    eng.verifierFor,
			Destination: eng.destinationFor,
		},
	})

	if loadErr != nil {
		logger.Error("configuration could not be loaded", "err", loadErr)
	}
	return eng, nil
}

// Home is where PortCloak keeps its files. It is read through a method rather
// than a field because Relocate can change it while the app is running.
func (e *Engine) Home() config.Home {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.home
}

// policy is the retry policy every adapter is wrapped with.
func (e *Engine) policy() resil.Policy {
	p := e.Config.Preferences()
	return resil.Policy{
		MaxAttempts: p.RetryMaxAttempts,
		BaseDelay:   p.RetryBaseDelay,
		MaxDelay:    p.RetryMaxDelay,
	}
}

func (e *Engine) reliableOptions() reliable.Options {
	return reliable.Options{Policy: e.policy(), Breakers: e.breakers}
}

// executorFor is the target half of the registry.
//
// Everything it returns is wrapped, so nothing reaches the orchestrator with an
// unguarded remote call in it.
func (e *Engine) executorFor(env config.Environment) (target.Executor, error) {
	inner, err := e.rawExecutor(env)
	if err != nil {
		return nil, err
	}
	return reliable.WrapExecutor(inner, endpointFor(env), e.reliableOptions()), nil
}

func (e *Engine) rawExecutor(env config.Environment) (target.Executor, error) {
	switch env.Kind {
	case config.EnvLocal:
		return local.New(env), nil
	case config.EnvSSH:
		return sshtarget.New(env, e.Creds)
	case config.EnvDocker:
		return docker.NewExecutor(env)
	case config.EnvKubernetes:
		return k8s.NewExecutor(env)
	default:
		return nil, resil.Fatal("reach the environment",
			fmt.Sprintf("%q is not an environment kind PortCloak knows.", env.Kind), nil)
	}
}

func endpointFor(env config.Environment) string {
	return string(env.Kind) + ":" + env.Target()
}

// storeFor is the storage half of the registry.
func (e *Engine) storeFor(st config.Storage) (store.BlobStore, error) {
	inner, err := e.rawStore(st)
	if err != nil {
		return nil, err
	}
	return reliable.WrapStore(inner, e.reliableOptions()), nil
}

func (e *Engine) rawStore(st config.Storage) (store.BlobStore, error) {
	switch st.Kind {
	case config.StoreDisk:
		return disk.New(st.Folder)
	case config.StoreSSH:
		return sftpstore.New(st, e.Creds)
	case config.StoreS3:
		return s3store.New(context.Background(), st, e.Creds)
	case config.StoreAzure:
		return azurestore.New(st, e.Creds)
	default:
		return nil, resil.Fatal("reach the storage",
			fmt.Sprintf("%q is not a storage kind PortCloak knows.", st.Kind), nil)
	}
}

// adminFor builds the concrete Admin API client.
//
// The probe uses this rather than verifierFor because it reports *why* the
// Admin API did not answer, and the Verifier interface only says whether it
// did — which is all a capture needs and not enough for an editor.
func (e *Engine) adminFor(env config.Environment) (*admin.Client, error) {
	return admin.New(env, e.Creds)
}

// verifierFor builds the optional Admin API client. A nil client is a supported
// configuration, not an error.
func (e *Engine) verifierFor(env config.Environment) (orchestrator.Verifier, error) {
	c, err := admin.New(env, e.Creds)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, nil
	}
	return c, nil
}

// destinationFor adapts the Admin client to the restore path's view of a
// destination.
func (e *Engine) destinationFor(env config.Environment) (orchestrator.Destination, error) {
	c, err := admin.New(env, e.Creds)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, fmt.Errorf("no Admin API is configured on %s", env.Name)
	}
	return &destination{c: c}, nil
}

// destination bridges the Admin client's shape to the orchestrator's, so the
// orchestrator does not import the admin package.
type destination struct{ c *admin.Client }

func (d *destination) Reachable(ctx context.Context) bool { return d.c.Reachable(ctx) }

func (d *destination) ReadRealm(ctx context.Context, realmName string) (orchestrator.RealmShape, error) {
	counts, err := d.c.ReadRealm(ctx, realmName)
	if err != nil {
		return orchestrator.RealmShape{}, err
	}
	return orchestrator.RealmShape{
		Exists: counts.Exists, Users: counts.Users, Clients: counts.Clients,
		ClientScopes: counts.ClientScopes, RealmRoles: counts.RealmRoles,
		Groups: counts.Groups, IdentityProviders: counts.IdentityProviders,
		Federations: counts.Federations, KeyIDs: counts.KeyIDs,
	}, nil
}

func (d *destination) PartialImport(ctx context.Context, realmName string, body []byte, policy string) (int, int, int, error) {
	res, err := d.c.PartialImport(ctx, realmName, body, policy)
	if err != nil {
		return 0, 0, 0, err
	}
	return res.Added, res.Overwritten, res.Skipped, nil
}

// AttachSink points engine progress events at a destination.
func (e *Engine) AttachSink(s obs.Sink) {
	e.mu.Lock()
	e.sink = s
	e.mu.Unlock()
	e.Orch.SetSink(s)
}

// Sink returns the current event destination.
func (e *Engine) Sink() obs.Sink {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.sink
}

// Session returns an open snapshot by id.
func (e *Engine) Session(id string) (*inspect.Session, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	s, ok := e.sessions[id]
	if !ok {
		return nil, resil.Fatal("read the snapshot",
			"That snapshot is not open. Open it from the library first.", nil)
	}
	return s, nil
}

// putSession registers an open snapshot, closing any earlier session for the
// same snapshot first.
//
// Re-opening a snapshot that is already open is ordinary — navigating back into
// the inspector does it — and replacing the map entry silently orphaned the
// previous session: its decrypted working directory stayed on disk until the
// next launch swept it, and its index file, named after the same snapshot, was
// truncated underneath it by the new one.
func (e *Engine) putSession(s *inspect.Session) {
	e.mu.Lock()
	previous := e.sessions[s.ID]
	e.sessions[s.ID] = s
	e.mu.Unlock()

	if previous != nil && previous != s {
		if err := previous.Close(); err != nil {
			e.Log.Error("an earlier session for this snapshot could not be closed",
				"snapshot", s.ID, "err", err)
		}
	}
}

func (e *Engine) dropSession(id string) *inspect.Session {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.sessions[id]
	delete(e.sessions, id)
	return s
}

// OpenSessionIDs lists the snapshots currently open, which the sweep uses to
// avoid deleting an index that is in use.
func (e *Engine) OpenSessionIDs() map[string]bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make(map[string]bool, len(e.sessions))
	for id := range e.sessions {
		out[id] = true
	}
	return out
}

// StartupSweep is the housekeeping an operator should never have to ask for.
//
// Jobs that were running when the process died become Interrupted so they are
// offered rather than appearing to still be in flight; index files and
// decrypted working directories a crash left behind are removed.
//
// Orphaned ephemeral clones are found but never removed automatically — the
// operator's cluster is not ours to garbage-collect without asking.
func (e *Engine) StartupSweep() {
	adopted, err := e.Jobs.AdoptRunning()
	if err != nil {
		e.Log.Error("interrupted jobs could not be adopted", "err", err)
	} else if len(adopted) > 0 {
		e.Log.Info("interrupted jobs are available to resume", "count", len(adopted))
	}

	open := e.OpenSessionIDs()
	if removed, bytes, err := inspect.SweepIndexes(e.Home(), open); err != nil {
		e.Log.Error("inspection indexes could not be swept", "err", err)
	} else if removed > 0 {
		e.Log.Info("removed inspection indexes left by a previous session", "count", removed, "bytes", bytes)
	}
	if removed, err := inspect.SweepWorkDirs(e.Home(), open); err != nil {
		e.Log.Error("working directories could not be swept", "err", err)
	} else if removed > 0 {
		e.Log.Info("removed decrypted working files left by a previous session", "count", removed)
	}
}

// Close releases the log file and closes any snapshot still open.
func (e *Engine) Close() error {
	e.mu.Lock()
	sessions := e.sessions
	e.sessions = map[string]*inspect.Session{}
	e.mu.Unlock()

	// The same teardown that runs on an explicit close runs on exit, so
	// quitting with a snapshot open leaves nothing behind either.
	for _, s := range sessions {
		if err := s.Close(); err != nil {
			e.Log.Error("a snapshot could not be closed cleanly", "snapshot", s.ID, "err", err)
		}
	}
	if e.Log != nil {
		return e.Log.Close()
	}
	return nil
}

// Failure is how an engine error reaches a screen: a sentence, what to do next,
// and whether waiting would help.
type Failure struct {
	Message   string `json:"message"`
	Hint      string `json:"hint,omitempty"`
	Retryable bool   `json:"retryable"`
}

// Fail turns an error into the shape the UI renders.
func Fail(err error) *Failure {
	if err == nil {
		return nil
	}
	return &Failure{
		Message:   obs.RedactText(err.Error()),
		Hint:      resil.Hint(err),
		Retryable: resil.IsRetryable(err),
	}
}

// staleAfter is the preference-driven staleness threshold.
func (e *Engine) staleAfter() time.Duration { return e.Config.Preferences().ProbeStaleAfter }
