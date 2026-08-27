package obs

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// LogOptions configures the engine's logger.
type LogOptions struct {
	// File is the path to write structured logs to. Empty means stderr only.
	File string
	// MaxBytes rotates the file once it exceeds this size. Zero disables rotation.
	MaxBytes int64
	// Keep is how many rotated files to retain.
	Keep int
	// Level is the minimum level written.
	Level slog.Level
	// AlsoStderr mirrors output to stderr, which is what a developer running the
	// binary from a terminal expects.
	AlsoStderr bool
}

// DefaultLogOptions returns the options the application uses: a rotated file in
// the PortCloak home, at info level.
func DefaultLogOptions(file string) LogOptions {
	return LogOptions{
		File:     file,
		MaxBytes: 8 << 20,
		Keep:     3,
		Level:    slog.LevelInfo,
	}
}

// Logger is the engine's logger together with the resources it owns, so an
// application can shut it down cleanly.
type Logger struct {
	*slog.Logger
	closer io.Closer
	// rot is the same writer as closer when there is a file, kept under its own
	// name so the log can follow the home folder to a new location without the
	// handler behind it being rebuilt.
	rot *rotatingWriter
}

// Close releases the underlying log file.
func (l *Logger) Close() error {
	if l.closer == nil {
		return nil
	}
	return l.closer.Close()
}

// Suspend closes the log file while keeping the logger usable. Records written
// meanwhile go to stderr if it is attached and are otherwise dropped.
//
// It exists for one moment: the home folder is being moved, and Windows will
// not rename a directory that has a file open inside it.
func (l *Logger) Suspend() error {
	if l.rot == nil {
		return nil
	}
	return l.rot.suspend()
}

// Reopen points the log at a new path, creating it if need be.
func (l *Logger) Reopen(path string) error {
	if l.rot == nil {
		return nil
	}
	return l.rot.reopen(path)
}

// NewLogger builds a logger whose every record passes through RedactingHandler.
// There is deliberately no way to construct an engine logger that skips it.
func NewLogger(opts LogOptions) (*Logger, error) {
	var sinks []io.Writer
	var closer io.Closer
	var rot *rotatingWriter

	if opts.File != "" {
		if err := os.MkdirAll(filepath.Dir(opts.File), 0o700); err != nil {
			return nil, fmt.Errorf("creating the log directory %s: %w", filepath.Dir(opts.File), err)
		}
		rw, err := newRotatingWriter(opts.File, opts.MaxBytes, opts.Keep)
		if err != nil {
			return nil, err
		}
		sinks = append(sinks, rw)
		closer = rw
		rot = rw
	}
	if opts.AlsoStderr || opts.File == "" {
		sinks = append(sinks, os.Stderr)
	}

	var w io.Writer
	switch len(sinks) {
	case 1:
		w = sinks[0]
	default:
		w = io.MultiWriter(sinks...)
	}

	base := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: opts.Level})
	return &Logger{Logger: slog.New(NewRedactingHandler(base)), closer: closer, rot: rot}, nil
}

// Discard returns a logger that writes nowhere. Tests that do not assert on log
// output use it so they do not litter the terminal.
func Discard() *Logger {
	h := slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 1})
	return &Logger{Logger: slog.New(NewRedactingHandler(h))}
}

// rotatingWriter caps the log file's size. A desktop tool that runs for months
// should not be able to fill a disk with its own logs.
type rotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	keep     int
	f        *os.File
	size     int64
}

func newRotatingWriter(path string, maxBytes int64, keep int) (*rotatingWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening the log file %s: %w", path, err)
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("reading the size of %s: %w", path, err)
	}
	return &rotatingWriter{path: path, maxBytes: maxBytes, keep: keep, f: f, size: st.Size()}, nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Suspended: the home folder is moving underneath us. Dropping the record
	// beats failing the write, which slog reports by writing to stderr about
	// having failed to write.
	if w.f == nil {
		return len(p), nil
	}
	if w.maxBytes > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) rotate() error {
	if err := w.f.Close(); err != nil {
		return err
	}
	for i := w.keep - 1; i >= 1; i-- {
		older := fmt.Sprintf("%s.%d", w.path, i+1)
		newer := fmt.Sprintf("%s.%d", w.path, i)
		_ = os.Remove(older)
		_ = os.Rename(newer, older)
	}
	if w.keep > 0 {
		_ = os.Rename(w.path, w.path+".1")
	} else {
		_ = os.Remove(w.path)
	}

	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	w.f, w.size = f, 0
	return nil
}

func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	f := w.f
	w.f = nil
	return f.Close()
}

// suspend releases the file without ending the writer's life.
func (w *rotatingWriter) suspend() error { return w.Close() }

// reopen attaches the writer to a new path.
func (w *rotatingWriter) reopen(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.f != nil {
		_ = w.f.Close()
		w.f = nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating the log directory %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("opening the log file %s: %w", path, err)
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("reading the size of %s: %w", path, err)
	}
	w.path, w.f, w.size = path, f, st.Size()
	return nil
}
