package app

import (
	"fmt"
	"os"
	"sync"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/obs"
)

// Engine holds everything the controllers share: the configuration store, the
// job store, the credential store, the logger and the audit log.
//
// It is deliberately a plain struct with no Wails types in it. A test can build
// one against a temporary directory and drive every controller without a
// desktop runtime.
type Engine struct {
	Version string

	Home    config.Home
	Config  *config.Store
	Jobs    *config.JobStore
	Creds   config.CredentialStore
	Log     *obs.Logger
	Audit   *obs.AuditLog
	FirstRun bool

	mu   sync.RWMutex
	sink obs.Sink

	// LoadError is the validation failure, if any, that stopped configuration
	// from loading. The app still starts: an operator with a malformed file
	// needs to be told which line to fix, which they cannot read from a window
	// that refused to open.
	LoadError error
}

// NewEngine bootstraps the PortCloak home and loads configuration.
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
		Version:  version,
		Home:     home,
		Config:   store,
		Jobs:     config.NewJobStore(home),
		Creds:    config.NewKeychain(),
		Log:      logger,
		Audit:    audit,
		FirstRun: firstRun,
		sink:     obs.NopSink{},
		LoadError: loadErr,
	}
	if loadErr != nil {
		logger.Error("configuration could not be loaded", "err", loadErr)
	}
	return eng, nil
}

// AttachSink points engine progress events at a destination — the Wails event
// bus in the app, a recorder in tests.
func (e *Engine) AttachSink(s obs.Sink) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sink = s
}

// Sink returns the current event destination.
func (e *Engine) Sink() obs.Sink {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.sink
}

// Close releases the log file.
func (e *Engine) Close() error {
	if e.Log != nil {
		return e.Log.Close()
	}
	return nil
}

// Failure turns an engine error into something a screen can render: a sentence,
// a hint about what to do next, and whether waiting would help.
type Failure struct {
	Message   string `json:"message"`
	Hint      string `json:"hint,omitempty"`
	Retryable bool   `json:"retryable"`
}

func failure(err error) *Failure {
	if err == nil {
		return nil
	}
	return &Failure{Message: obs.RedactText(err.Error())}
}

func (e *Engine) logf(format string, args ...any) {
	e.Log.Info(fmt.Sprintf(format, args...))
}
