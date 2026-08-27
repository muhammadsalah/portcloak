package obs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Action names something PortCloak did that is worth a permanent record.
type Action string

const (
	ActionCapture          Action = "capture"
	ActionRestore          Action = "restore"
	ActionSecretReveal     Action = "secretReveal"
	ActionExportView       Action = "exportView"
	ActionSnapshotDelete   Action = "snapshotDelete"
	ActionEncryptionDeclin Action = "encryptionDeclined"
	ActionOrphanRemoved    Action = "orphanRemoved"
	ActionPurge            Action = "purgeLocalData"
	ActionVerify           Action = "verifySnapshot"
	ActionJobDiscarded     Action = "jobDiscarded"
	// Key lifecycle. A key is the difference between a bundle and a realm, so
	// creating, importing, revealing and — above all — deleting one belongs in
	// the permanent record next to the captures it makes readable.
	ActionKeyCreated  Action = "keyCreated"
	ActionKeyImported Action = "keyImported"
	ActionKeyRevealed Action = "keyRevealed"
	ActionKeyDeleted  Action = "keyDeleted"
)

// AuditEntry is one line of the append-only log.
//
// There is no actor field. PortCloak is a single-user local tool with no login
// (N8), so the honest record is what happened and when, on which machine —
// inventing a user identity would be worse than omitting one.
type AuditEntry struct {
	At          time.Time `json:"at"`
	Action      Action    `json:"action"`
	Outcome     string    `json:"outcome"`
	Realm       string    `json:"realm,omitempty"`
	SnapshotID  string    `json:"snapshotId,omitempty"`
	Environment string    `json:"environment,omitempty"`
	Storage     string    `json:"storage,omitempty"`
	// Detail describes the thing acted on by location and kind — never by value.
	Detail string `json:"detail,omitempty"`
	Reason string `json:"reason,omitempty"`
	Host   string `json:"host,omitempty"`
}

// AuditLog is an append-only JSON-lines file.
type AuditLog struct {
	mu   sync.Mutex
	path string
	now  func() time.Time
	host string
}

// NewAuditLog opens (or creates) the log at path.
func NewAuditLog(path string) (*AuditLog, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("creating the audit log directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening the audit log %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	return &AuditLog{path: path, now: time.Now, host: host}, nil
}

// Record appends one entry. Every free-text field goes through the same
// redaction as the log, because the audit log is an output like any other and
// is the one most likely to be attached to a ticket.
func (a *AuditLog) Record(e AuditEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if e.At.IsZero() {
		e.At = a.now()
	}
	if e.Host == "" {
		e.Host = a.host
	}
	e.Detail = RedactText(e.Detail)
	e.Reason = RedactText(e.Reason)

	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("opening the audit log: %w", err)
	}
	defer f.Close() //nolint:errcheck // the Sync below is what actually commits.
	if _, err := f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("writing the audit log: %w", err)
	}
	return f.Sync()
}

// Read returns the entries, newest first, optionally filtered.
func (a *AuditLog) Read(filter AuditFilter) ([]AuditEntry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	b, err := os.ReadFile(a.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []AuditEntry
	for _, line := range splitLines(b) {
		if len(line) == 0 {
			continue
		}
		var e AuditEntry
		if err := json.Unmarshal(line, &e); err != nil {
			// A corrupt line is skipped rather than failing the whole read: the
			// point of the log is what it does contain.
			continue
		}
		if filter.matches(e) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out, nil
}

// Path is where the log lives, so the UI can point an operator at the file.
func (a *AuditLog) Path() string { return a.path }

// AuditFilter narrows a read (UC-O8 A1).
type AuditFilter struct {
	Action Action
	Since  time.Time
	Until  time.Time
}

func (f AuditFilter) matches(e AuditEntry) bool {
	if f.Action != "" && e.Action != f.Action {
		return false
	}
	if !f.Since.IsZero() && e.At.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && e.At.After(f.Until) {
		return false
	}
	return true
}

func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			out = append(out, b[start:i])
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}
