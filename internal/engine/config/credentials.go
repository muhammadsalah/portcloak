// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

// HandleScheme is the prefix every credential handle carries.
const HandleScheme = "keychain://portcloak/"

// keyringService is the service name every PortCloak secret is filed under in
// the OS keychain, so an operator can find and remove them by hand.
const keyringService = "portcloak"

// ErrCredentialMissing is returned when a handle resolves to nothing on this
// machine. It is a distinct error because it is not a fault — it is exactly
// what happens after copying a config.yaml between machines, and the UI says so
// rather than failing obscurely (UC-O7).
var ErrCredentialMissing = errors.New("credential not in this machine's keychain")

// MissingCredentialError names which entry needs re-entering.
type MissingCredentialError struct {
	Handle string
	Owner  string
}

func (e *MissingCredentialError) Error() string {
	if e.Owner != "" {
		return fmt.Sprintf("the credential for %q is not in this machine's keychain. Configuration is portable between machines; the secrets deliberately are not, so re-enter it here.", e.Owner)
	}
	return fmt.Sprintf("%s is not in this machine's keychain.", e.Handle)
}

func (e *MissingCredentialError) Unwrap() error { return ErrCredentialMissing }

// CredentialStore holds PortCloak's own connection credentials. Values are
// resolved at use time and never written to config.yaml.
type CredentialStore interface {
	// Get resolves a handle. A handle with no value returns an error wrapping
	// ErrCredentialMissing.
	Get(handle string) (string, error)
	// Set stores a value against a handle, replacing any previous one.
	Set(handle, value string) error
	// Delete removes a handle's value. Deleting one that does not exist is not
	// an error: the desired end state has been reached either way.
	Delete(handle string) error
}

// Handle builds a credential handle for a kind and a name.
func Handle(kind, name string) string {
	return HandleScheme + kind + "/" + name
}

// ValidHandle reports whether a string is a well-formed credential handle.
func ValidHandle(h string) bool {
	rest, ok := strings.CutPrefix(h, HandleScheme)
	if !ok {
		return false
	}
	kind, name, ok := strings.Cut(rest, "/")
	return ok && kind != "" && name != ""
}

// handleKey turns a handle into the keychain account name.
func handleKey(handle string) (string, error) {
	rest, ok := strings.CutPrefix(handle, HandleScheme)
	if !ok || rest == "" {
		return "", fmt.Errorf("%q is not a credential handle; handles look like %s<kind>/<name>", handle, HandleScheme)
	}
	if kind, name, ok := strings.Cut(rest, "/"); !ok || kind == "" || name == "" {
		return "", fmt.Errorf("%q is not a credential handle; handles look like %s<kind>/<name>", handle, HandleScheme)
	}
	return rest, nil
}

// Keychain is the CredentialStore backed by the OS keychain — macOS Keychain,
// Windows Credential Manager, or libsecret on Linux.
type Keychain struct{}

// NewKeychain returns the real credential store.
func NewKeychain() *Keychain { return &Keychain{} }

func (k *Keychain) Get(handle string) (string, error) {
	key, err := handleKey(handle)
	if err != nil {
		return "", err
	}
	v, err := keyring.Get(keyringService, key)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", &MissingCredentialError{Handle: handle}
		}
		return "", fmt.Errorf("reading %s from this machine's keychain: %w", handle, err)
	}
	return v, nil
}

func (k *Keychain) Set(handle, value string) error {
	key, err := handleKey(handle)
	if err != nil {
		return err
	}
	if err := keyring.Set(keyringService, key, value); err != nil {
		return fmt.Errorf("storing %s in this machine's keychain: %w", handle, err)
	}
	return nil
}

func (k *Keychain) Delete(handle string) error {
	key, err := handleKey(handle)
	if err != nil {
		return err
	}
	if err := keyring.Delete(keyringService, key); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("removing %s from this machine's keychain: %w", handle, err)
	}
	return nil
}

// MemoryCredentials is the in-memory CredentialStore used by every test, so a
// test run never touches the operator's real keychain.
type MemoryCredentials struct {
	mu     sync.RWMutex
	values map[string]string
}

// NewMemoryCredentials returns an empty in-memory store.
func NewMemoryCredentials() *MemoryCredentials {
	return &MemoryCredentials{values: map[string]string{}}
}

func (m *MemoryCredentials) Get(handle string) (string, error) {
	key, err := handleKey(handle)
	if err != nil {
		return "", err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.values[key]
	if !ok {
		return "", &MissingCredentialError{Handle: handle}
	}
	return v, nil
}

func (m *MemoryCredentials) Set(handle, value string) error {
	key, err := handleKey(handle)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[key] = value
	return nil
}

func (m *MemoryCredentials) Delete(handle string) error {
	key, err := handleKey(handle)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, key)
	return nil
}

// Resolve fetches a handle's value, naming the owner in any error so the
// operator knows which entry to fix rather than which handle string failed.
func Resolve(cs CredentialStore, handle, owner string) (string, error) {
	if handle == "" {
		return "", nil
	}
	v, err := cs.Get(handle)
	if err != nil {
		var missing *MissingCredentialError
		if errors.As(err, &missing) {
			missing.Owner = owner
			return "", missing
		}
		return "", err
	}
	return v, nil
}

// Overlay answers one handle from memory and passes everything else through.
//
// It is what lets a definition be tested before it is saved. The editor holds a
// draft and, if the operator typed one, a secret that is not in the keychain
// yet — and a test that read the keychain would be testing the definition as it
// was rather than as it is being written. Handing the store an overlay tests
// what is on screen.
//
// An empty value overlays nothing, so editing a definition without retyping its
// secret still tests against the one already stored.
type Overlay struct {
	Handle string
	Value  string
	Base   CredentialStore
}

// NewOverlay wraps base so that handle resolves to value. A blank handle or a
// blank value returns base unchanged, which keeps the caller free of the test.
func NewOverlay(base CredentialStore, handle, value string) CredentialStore {
	if handle == "" || value == "" {
		return base
	}
	return &Overlay{Handle: handle, Value: value, Base: base}
}

func (o *Overlay) Get(handle string) (string, error) {
	if handle == o.Handle {
		return o.Value, nil
	}
	return o.Base.Get(handle)
}

// Set and Delete go to the base store. A test never writes, so neither is
// reached on the path this exists for; forwarding them keeps the overlay a
// credential store rather than a partial one that fails in surprising places.
func (o *Overlay) Set(handle, value string) error { return o.Base.Set(handle, value) }
func (o *Overlay) Delete(handle string) error     { return o.Base.Delete(handle) }
