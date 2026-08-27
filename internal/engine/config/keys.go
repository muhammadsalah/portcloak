// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"strings"
	"time"
)

// KeyKind is what a stored key actually is.
type KeyKind string

const (
	// KeyIdentity is an age keypair. The public half is a recipient a capture
	// seals to; the private half opens anything sealed to it.
	KeyIdentity KeyKind = "identity"
	// KeyPassphrase is a remembered passphrase.
	KeyPassphrase KeyKind = "passphrase"
)

// KeyKinds is the full set, in the order the UI offers them.
var KeyKinds = []KeyKind{KeyIdentity, KeyPassphrase}

// Key is one named way of sealing and opening a snapshot.
//
// Encryption used to be something an operator supplied from outside PortCloak
// on every single occasion: a passphrase typed at capture and typed again at
// every restore, or an age keypair generated elsewhere and pasted in. That
// works exactly once and then becomes the reason encryption gets turned off.
//
// A key gives the material a name and a home. The secret half lives in this
// machine's OS keychain like every other secret PortCloak holds; config.yaml
// carries only the name, the kind, the public half where there is one, and the
// handle. Which means the file stays portable between machines and the secrets
// deliberately do not — the same bargain the rest of the configuration makes.
type Key struct {
	Name string  `yaml:"name" json:"name"`
	Kind KeyKind `yaml:"kind" json:"kind"`
	// PublicKey is the age recipient, for an identity. It is not a secret: it
	// is recorded so a capture can seal to this key without unlocking it, and
	// so a snapshot's recipient list can be read back as names.
	PublicKey string `yaml:"publicKey,omitempty" json:"publicKey,omitempty"`
	// CredentialRef is the handle the secret half is filed under.
	CredentialRef string `yaml:"credentialRef" json:"credentialRef"`
	// Note is whatever the operator wants to remember about it — whose key it
	// is, which environment it belongs to.
	Note      string    `yaml:"note,omitempty" json:"note,omitempty"`
	CreatedAt time.Time `yaml:"createdAt,omitempty" json:"createdAt,omitempty"`

	Extra map[string]any `yaml:",inline" json:"-"`
}

// KeyByName finds a key by name.
func (c Config) KeyByName(name string) (Key, bool) {
	for _, k := range c.Keys {
		if k.Name == name {
			return k, true
		}
	}
	return Key{}, false
}

// Identities returns the age keypairs, which are the keys a capture can seal to
// without unlocking anything.
func (c Config) Identities() []Key {
	var out []Key
	for _, k := range c.Keys {
		if k.Kind == KeyIdentity {
			out = append(out, k)
		}
	}
	return out
}

// AddKey appends a new key.
func (s *Store) AddKey(k Key) error {
	return s.Update(func(c *Config) error {
		if _, exists := c.KeyByName(k.Name); exists {
			return fmt.Errorf("a key named %q already exists", k.Name)
		}
		c.Keys = append(c.Keys, k)
		return nil
	})
}

// SaveKey replaces an existing key, matched by its original name so a rename is
// one operation.
func (s *Store) SaveKey(originalName string, k Key) error {
	return s.Update(func(c *Config) error {
		idx := -1
		for i := range c.Keys {
			if c.Keys[i].Name == originalName {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("%w: key %q", ErrNotFound, originalName)
		}
		if k.Name != originalName {
			if _, exists := c.KeyByName(k.Name); exists {
				return fmt.Errorf("a key named %q already exists", k.Name)
			}
		}
		c.Keys[idx] = k
		return nil
	})
}

// DeleteKey removes a key and the secret behind it.
//
// There is nothing to check it against first, which is the point worth stating
// where it is called: a key is not "in use" by anything PortCloak can see. It is
// in use by every snapshot ever sealed with it, and those live in storage
// backends that may not even be configured here. Deleting one is not undoable
// by any means PortCloak has.
func (s *Store) DeleteKey(name string, creds CredentialStore) error {
	var ref string
	err := s.Update(func(c *Config) error {
		idx := -1
		for i := range c.Keys {
			if c.Keys[i].Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("%w: key %q", ErrNotFound, name)
		}
		ref = c.Keys[idx].CredentialRef
		c.Keys = append(c.Keys[:idx], c.Keys[idx+1:]...)
		return nil
	})
	if err != nil {
		return err
	}
	// The keychain is cleaned only after the config write succeeded, so a
	// failed save never leaves an entry pointing at a secret that is gone.
	if creds != nil && ref != "" {
		_ = creds.Delete(ref)
	}
	return nil
}

// SuggestKeyHandle proposes the handle a new key should use.
func SuggestKeyHandle(kind KeyKind, name string) string {
	return Handle("key-"+string(kind), slug(name))
}

// validateKey checks one entry of the keys list.
func validateKey(k Key, base string, add addFunc) {
	if strings.TrimSpace(k.Name) == "" {
		add(base+".name", "this key has no name. PortCloak refers to keys by name everywhere, so it needs one.")
	}
	switch k.Kind {
	case KeyIdentity:
		if k.PublicKey == "" {
			add(base+".publicKey", "the key %q is an age identity but records no public key. Without it PortCloak cannot seal anything to this key.", k.Name)
		} else if !strings.HasPrefix(k.PublicKey, "age1") {
			add(base+".publicKey", "%q is not an age public key. They start with age1.", k.PublicKey)
		}
	case KeyPassphrase:
		if k.PublicKey != "" {
			add(base+".publicKey", "the key %q is a passphrase, which has no public half.", k.Name)
		}
	case "":
		add(base+".kind", "the key %q has no kind. It has to be identity or passphrase.", k.Name)
	default:
		add(base+".kind", "%q is not a key kind PortCloak knows. It has to be identity or passphrase.", k.Kind)
	}
	if k.CredentialRef == "" {
		add(base+".credentialRef", "the key %q has no credential handle, so its secret half cannot be found.", k.Name)
	} else if !ValidHandle(k.CredentialRef) {
		add(base+".credentialRef", "%q is not a credential handle. Handles look like keychain://portcloak/<kind>/<name> and the value itself stays in this machine's keychain.", k.CredentialRef)
	}
}
