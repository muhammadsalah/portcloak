package config

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotFound is returned when an entry named in a request does not exist.
var ErrNotFound = errors.New("no entry with that name")

// InUseError blocks a deletion because something is actively using the entry.
type InUseError struct {
	Name   string
	Reason string
}

func (e *InUseError) Error() string {
	return fmt.Sprintf("%q is %s, so it cannot be deleted yet.", e.Name, e.Reason)
}

// ReferenceWarning describes what would be affected by a deletion. It is a
// warning rather than a refusal wherever the reference is historical: past
// snapshots record the environment they came from, and those records are
// immutable — deleting an environment does not rewrite history (UC-E8 A1).
type ReferenceWarning struct {
	Name    string
	Message string
}

// JobLookup reports whether a job is currently using a named environment or
// storage. The orchestrator supplies it; a nil lookup means nothing is running.
type JobLookup func(kind, name string) (jobID string, running bool)

// AddEnvironment appends a new environment.
func (s *Store) AddEnvironment(e Environment) error {
	return s.Update(func(c *Config) error {
		if _, exists := c.Environment(e.Name); exists {
			return fmt.Errorf("an environment named %q already exists", e.Name)
		}
		c.Environments = append(c.Environments, e)
		return nil
	})
}

// SaveEnvironment replaces an existing environment, matched by its original
// name so a rename is one operation.
//
// The Tested OK stamp is cleared whenever a field that affects reachability
// changes: an edited environment is untested by definition (UC-E6).
func (s *Store) SaveEnvironment(originalName string, e Environment) error {
	return s.Update(func(c *Config) error {
		idx := -1
		for i := range c.Environments {
			if c.Environments[i].Name == originalName {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("%w: environment %q", ErrNotFound, originalName)
		}
		if e.Name != originalName {
			if _, exists := c.Environment(e.Name); exists {
				return fmt.Errorf("an environment named %q already exists", e.Name)
			}
		}
		prev := c.Environments[idx]
		if environmentReachabilityChanged(prev, e) {
			e.LastProbe = nil
		} else if e.LastProbe == nil {
			e.LastProbe = prev.LastProbe
		}
		c.Environments[idx] = e
		return nil
	})
}

// DuplicateEnvironment copies everything except the credential handles.
//
// Not copying them is the point: a cloned environment prompts for its own
// credential rather than silently reusing another environment's (UC-E7).
func (s *Store) DuplicateEnvironment(name, newName string) (Environment, error) {
	var out Environment
	err := s.Update(func(c *Config) error {
		src, ok := c.Environment(name)
		if !ok {
			return fmt.Errorf("%w: environment %q", ErrNotFound, name)
		}
		if newName == "" {
			newName = uniqueName(name, func(n string) bool { _, e := c.Environment(n); return e })
		}
		if _, exists := c.Environment(newName); exists {
			return fmt.Errorf("an environment named %q already exists", newName)
		}
		dup := src
		dup.Name = newName
		dup.CredentialRef = ""
		dup.AdminCredentialRef = ""
		dup.LastProbe = nil
		if dup.JumpHost != nil {
			j := *dup.JumpHost
			j.CredentialRef = ""
			dup.JumpHost = &j
		}
		c.Environments = append(c.Environments, dup)
		out = dup
		return nil
	})
	return out, err
}

// DeleteEnvironment removes an environment and its keychain secrets.
func (s *Store) DeleteEnvironment(name string, creds CredentialStore, jobs JobLookup) error {
	var refs []string
	if jobs != nil {
		if id, running := jobs("environment", name); running {
			return &InUseError{Name: name, Reason: fmt.Sprintf("in use by job %s", id)}
		}
	}
	err := s.Update(func(c *Config) error {
		idx := -1
		for i := range c.Environments {
			if c.Environments[i].Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("%w: environment %q", ErrNotFound, name)
		}
		e := c.Environments[idx]
		if e.CredentialRef != "" {
			refs = append(refs, e.CredentialRef)
		}
		if e.AdminCredentialRef != "" {
			refs = append(refs, e.AdminCredentialRef)
		}
		if e.JumpHost != nil && e.JumpHost.CredentialRef != "" {
			refs = append(refs, e.JumpHost.CredentialRef)
		}
		c.Environments = append(c.Environments[:idx], c.Environments[idx+1:]...)
		return nil
	})
	if err != nil {
		return err
	}
	// The keychain is cleaned only after the config write succeeded, so a
	// failed save never leaves an entry pointing at a secret that is gone.
	if creds != nil {
		for _, h := range refs {
			_ = creds.Delete(h)
		}
	}
	return nil
}

// AddStorage appends a new storage definition, honouring the default flag's
// exclusivity as part of the same atomic write.
func (s *Store) AddStorage(st Storage) error {
	return s.Update(func(c *Config) error {
		if _, exists := c.StorageByName(st.Name); exists {
			return fmt.Errorf("a storage named %q already exists", st.Name)
		}
		c.Storage = append(c.Storage, st)
		if st.Default {
			makeDefaultExclusive(c, st.Name)
		} else if len(c.Storage) == 1 {
			// The first storage defined becomes the default, because a capture
			// wizard with nothing pre-selected is a worse first experience than
			// one pre-selecting the only option there is.
			c.Storage[0].Default = true
		}
		return nil
	})
}

// SaveStorage replaces an existing storage definition.
func (s *Store) SaveStorage(originalName string, st Storage) error {
	return s.Update(func(c *Config) error {
		idx := -1
		for i := range c.Storage {
			if c.Storage[i].Name == originalName {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("%w: storage %q", ErrNotFound, originalName)
		}
		if st.Name != originalName {
			if _, exists := c.StorageByName(st.Name); exists {
				return fmt.Errorf("a storage named %q already exists", st.Name)
			}
		}
		prev := c.Storage[idx]
		if storageReachabilityChanged(prev, st) {
			st.LastProbe = nil
		} else if st.LastProbe == nil {
			st.LastProbe = prev.LastProbe
		}
		c.Storage[idx] = st
		if st.Default {
			makeDefaultExclusive(c, st.Name)
		}
		return nil
	})
}

// DuplicateStorage copies a storage definition without its credentials.
func (s *Store) DuplicateStorage(name, newName string) (Storage, error) {
	var out Storage
	err := s.Update(func(c *Config) error {
		src, ok := c.StorageByName(name)
		if !ok {
			return fmt.Errorf("%w: storage %q", ErrNotFound, name)
		}
		if newName == "" {
			newName = uniqueName(name, func(n string) bool { _, e := c.StorageByName(n); return e })
		}
		if _, exists := c.StorageByName(newName); exists {
			return fmt.Errorf("a storage named %q already exists", newName)
		}
		dup := src
		dup.Name = newName
		dup.CredentialRef = ""
		dup.Default = false
		dup.LastProbe = nil
		if dup.JumpHost != nil {
			j := *dup.JumpHost
			j.CredentialRef = ""
			dup.JumpHost = &j
		}
		c.Storage = append(c.Storage, dup)
		out = dup
		return nil
	})
	return out, err
}

// DeleteStorage removes a storage definition and its keychain secret.
//
// Stored snapshot files are never touched: removing a storage definition
// forgets how to reach the data, it does not destroy it (UC-S6).
func (s *Store) DeleteStorage(name string, creds CredentialStore, jobs JobLookup) error {
	if jobs != nil {
		if id, running := jobs("storage", name); running {
			return &InUseError{Name: name, Reason: fmt.Sprintf("in use by job %s", id)}
		}
	}
	var ref string
	err := s.Update(func(c *Config) error {
		idx := -1
		for i := range c.Storage {
			if c.Storage[i].Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("%w: storage %q", ErrNotFound, name)
		}
		st := c.Storage[idx]
		if st.Default && len(c.Storage) > 1 {
			return &InUseError{Name: name, Reason: "the default storage for new captures; choose another default first"}
		}
		ref = st.CredentialRef
		c.Storage = append(c.Storage[:idx], c.Storage[idx+1:]...)
		return nil
	})
	if err != nil {
		return err
	}
	if creds != nil && ref != "" {
		_ = creds.Delete(ref)
	}
	return nil
}

// SetDefaultStorage marks one storage as the default and clears the rest, as a
// single atomic write.
func (s *Store) SetDefaultStorage(name string) error {
	return s.Update(func(c *Config) error {
		if _, ok := c.StorageByName(name); !ok {
			return fmt.Errorf("%w: storage %q", ErrNotFound, name)
		}
		makeDefaultExclusive(c, name)
		return nil
	})
}

// RecordEnvironmentProbe stamps an environment with its latest Test result.
func (s *Store) RecordEnvironmentProbe(name string, stamp ProbeStamp) error {
	return s.Update(func(c *Config) error {
		for i := range c.Environments {
			if c.Environments[i].Name == name {
				st := stamp
				c.Environments[i].LastProbe = &st
				return nil
			}
		}
		return fmt.Errorf("%w: environment %q", ErrNotFound, name)
	})
}

// RecordStorageProbe stamps a storage with its latest Test result.
func (s *Store) RecordStorageProbe(name string, stamp ProbeStamp) error {
	return s.Update(func(c *Config) error {
		for i := range c.Storage {
			if c.Storage[i].Name == name {
				st := stamp
				c.Storage[i].LastProbe = &st
				return nil
			}
		}
		return fmt.Errorf("%w: storage %q", ErrNotFound, name)
	})
}

// SetPreferences replaces the preferences block.
func (s *Store) SetPreferences(p Preferences) error {
	return s.Update(func(c *Config) error {
		c.Preferences = p
		return nil
	})
}

func makeDefaultExclusive(c *Config, name string) {
	for i := range c.Storage {
		c.Storage[i].Default = c.Storage[i].Name == name
	}
}

func uniqueName(base string, exists func(string) bool) string {
	candidate := base + " copy"
	if !exists(candidate) {
		return candidate
	}
	for n := 2; ; n++ {
		candidate = fmt.Sprintf("%s copy %d", base, n)
		if !exists(candidate) {
			return candidate
		}
	}
}

func environmentReachabilityChanged(a, b Environment) bool {
	a.LastProbe, b.LastProbe = nil, nil
	return fmt.Sprintf("%#v|%#v", a, a.JumpHost) != fmt.Sprintf("%#v|%#v", b, b.JumpHost)
}

func storageReachabilityChanged(a, b Storage) bool {
	a.LastProbe, b.LastProbe = nil, nil
	a.Default, b.Default = false, false
	return fmt.Sprintf("%#v|%#v", a, a.JumpHost) != fmt.Sprintf("%#v|%#v", b, b.JumpHost)
}

// StaleAfter is the preference-driven staleness threshold, resolved.
func (s *Store) StaleAfter() time.Duration {
	return s.Preferences().ProbeStaleAfter
}

// CredentialStatus reports, per entry, whether its keychain secret is present
// on this machine. It is what turns "copied the config across" from an obscure
// connection failure into a prompt to re-enter a credential (UC-O7).
type CredentialStatus struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Handle  string `json:"handle"`
	Present bool   `json:"present"`
}

// CheckCredentials resolves every handle in the configuration.
func (s *Store) CheckCredentials(creds CredentialStore) []CredentialStatus {
	cfg := s.Config()
	var out []CredentialStatus
	check := func(name, kind, handle string) {
		if handle == "" {
			return
		}
		_, err := creds.Get(handle)
		out = append(out, CredentialStatus{Name: name, Kind: kind, Handle: handle, Present: err == nil})
	}
	for _, e := range cfg.Environments {
		check(e.Name, "environment", e.CredentialRef)
		check(e.Name, "environment/admin", e.AdminCredentialRef)
		if e.JumpHost != nil {
			check(e.Name, "environment/jumpHost", e.JumpHost.CredentialRef)
		}
	}
	for _, st := range cfg.Storage {
		check(st.Name, "storage", st.CredentialRef)
	}
	return out
}

// SuggestHandle proposes the handle a new entry should use.
func SuggestHandle(kind EnvironmentKind, name string) string {
	return Handle(string(kind), slug(name))
}

// SuggestStorageHandle proposes the handle a new storage entry should use.
func SuggestStorageHandle(kind StorageKind, name string) string {
	return Handle(string(kind), slug(name))
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '.':
			b.WriteRune(r)
		case r == ' ' || r == '_' || r == '/':
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "unnamed"
	}
	return b.String()
}
