// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
	"strings"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/crypto"
	"portcloak/internal/engine/inspect"
	"portcloak/internal/engine/obs"
)

// KeysController is the Keys screen.
//
// Encryption used to be something an operator supplied from outside PortCloak
// every single time: a passphrase typed at capture and typed again at every
// restore, or an age keypair generated elsewhere and pasted in. PortCloak could
// generate a keypair, but only showed it once and stored nothing — so the
// operator was still the key management system. That works exactly once, and
// then it becomes the reason encryption gets turned off.
//
// A key here has a name, a kind and a home. The secret half goes to this
// machine's OS keychain like every other secret PortCloak holds; config.yaml
// carries the name, the kind, the public half where there is one, and a handle.
type KeysController struct{ eng *Engine }

// NewKeysController binds the Keys screen.
func NewKeysController(eng *Engine) *KeysController { return &KeysController{eng: eng} }

// ServiceName is the name internal/desktop logs this service under. It is
// not the address a bound method is called by — see the comment on
// controllers there, which is where reading it as one caused real damage.
func (k *KeysController) ServiceName() string { return "KeysController" }

// KeyView is one row of the Keys screen.
type KeyView struct {
	config.Key
	// Present is whether the secret half is actually in this machine's
	// keychain. A key whose config entry survived a machine move but whose
	// secret did not is shown as what it is rather than offered as usable.
	Present bool `json:"present"`
	// Usable says whether this key can open a snapshot right now without the
	// operator typing anything.
	Usable bool `json:"usable"`
	// Age renders how long ago it was created.
	Age string `json:"age,omitempty"`
	// Summary is the one-line description the list shows.
	Summary string `json:"summary"`
}

// KeysView is the whole screen.
type KeysView struct {
	Keys []KeyView `json:"keys"`
	// Unlockable counts the keys that could open a snapshot unprompted, which
	// is what the restore and inspect screens promise on the strength of.
	Unlockable int    `json:"unlockable"`
	Note       string `json:"note"`
	// DeleteWarning is stated wherever a key is about to be removed.
	DeleteWarning string   `json:"deleteWarning"`
	Failure       *Failure `json:"failure,omitempty"`
}

// deleteWarning is what deleting a key actually means. A key is not "in use" by
// anything PortCloak can see: it is in use by every snapshot ever sealed with
// it, and those live in storage backends that may not even be configured here.
const deleteWarning = "Every snapshot sealed with this key becomes permanently unreadable unless a copy of the key exists somewhere else. PortCloak cannot check where those snapshots are, cannot warn you which ones they were, and cannot undo this."

// List returns every key.
func (k *KeysController) List() (res KeysView) {
	defer func() { res = lists(res) }()
	cfg := k.eng.Config.Config()
	now := time.Now()

	out := KeysView{DeleteWarning: deleteWarning}
	for _, key := range cfg.Keys {
		v := KeyView{Key: key}
		_, err := k.eng.Creds.Get(key.CredentialRef)
		v.Present = err == nil
		v.Usable = v.Present
		if !key.CreatedAt.IsZero() {
			v.Age = renderAge(now.Sub(key.CreatedAt))
		}
		v.Summary = summarise(key, v.Present)
		if v.Usable {
			out.Unlockable++
		}
		out.Keys = append(out.Keys, v)
	}

	switch {
	case len(out.Keys) == 0:
		out.Note = "No keys yet. A key lets PortCloak seal a snapshot and open it again later without asking you to remember anything."
	case out.Unlockable == 0:
		out.Note = "None of these keys has its secret half in this machine's keychain, so none of them can open a snapshot here."
	default:
		out.Note = fmt.Sprintf("%d of %d key(s) can open a snapshot on this machine without being asked for.",
			out.Unlockable, len(out.Keys))
	}
	return out
}

func summarise(k config.Key, present bool) string {
	if !present {
		return "The secret half is not in this machine's keychain. Configuration is portable between machines; the secrets deliberately are not."
	}
	switch k.Kind {
	case config.KeyIdentity:
		return "An age keypair. Captures can seal to it, and it opens anything sealed to it."
	case config.KeyPassphrase:
		return "A remembered passphrase. It seals a snapshot and is tried when one needs opening."
	default:
		return ""
	}
}

// GeneratedKey is what a freshly created keypair hands back.
type GeneratedKey struct {
	Name      string `json:"name"`
	PublicKey string `json:"publicKey"`
	// PrivateKey is returned so it can be written down or copied elsewhere.
	// PortCloak has already stored it; this is for the copy that outlives this
	// machine.
	PrivateKey string   `json:"privateKey"`
	Warning    string   `json:"warning"`
	Failure    *Failure `json:"failure,omitempty"`
}

// backupWarning is why a stored key is still shown once.
const backupWarning = "PortCloak has stored this key in this machine's keychain, so you will not be asked for it again here. That is not a backup: a lost machine is a lost key, and every snapshot sealed with it goes with it. Keep a copy somewhere the machine is not."

// Generate creates a new age keypair, stores the private half and records the
// public half.
func (k *KeysController) Generate(name, note string) GeneratedKey {
	name = strings.TrimSpace(name)
	if name == "" {
		return GeneratedKey{Failure: &Failure{Message: "A key needs a name. It is how every other screen refers to it."}}
	}
	priv, pub, err := crypto.GenerateIdentity()
	if err != nil {
		return GeneratedKey{Failure: Fail(err)}
	}
	if f := k.store(config.Key{
		Name: name, Kind: config.KeyIdentity, PublicKey: pub,
		Note: note, CreatedAt: time.Now(),
	}, priv); f != nil {
		return GeneratedKey{Failure: f}
	}

	_ = k.eng.Audit.Record(obs.AuditEntry{
		Action: obs.ActionKeyCreated, Outcome: "created",
		Detail: fmt.Sprintf("age identity %q (%s)", name, pub),
	})
	return GeneratedKey{Name: name, PublicKey: pub, PrivateKey: priv, Warning: backupWarning}
}

// ImportIdentity records a keypair the operator already holds.
//
// Only the private half is asked for: the public half is derived from it, so
// there is no way to store a pair whose two halves do not match.
func (k *KeysController) ImportIdentity(name, privateKey, note string) *Failure {
	name = strings.TrimSpace(name)
	if name == "" {
		return &Failure{Message: "A key needs a name. It is how every other screen refers to it."}
	}
	pub, err := crypto.IdentityPublicKey(privateKey)
	if err != nil {
		return Fail(err)
	}
	if f := k.store(config.Key{
		Name: name, Kind: config.KeyIdentity, PublicKey: pub,
		Note: note, CreatedAt: time.Now(),
	}, strings.TrimSpace(privateKey)); f != nil {
		return f
	}

	_ = k.eng.Audit.Record(obs.AuditEntry{
		Action: obs.ActionKeyImported, Outcome: "imported",
		Detail: fmt.Sprintf("age identity %q (%s)", name, pub),
	})
	return nil
}

// SavePassphrase remembers a passphrase under a name.
func (k *KeysController) SavePassphrase(name, passphrase, note string) *Failure {
	name = strings.TrimSpace(name)
	if name == "" {
		return &Failure{Message: "A key needs a name. It is how every other screen refers to it."}
	}
	if strings.TrimSpace(passphrase) == "" {
		return &Failure{Message: "There is no passphrase to remember."}
	}
	if f := k.store(config.Key{
		Name: name, Kind: config.KeyPassphrase, Note: note, CreatedAt: time.Now(),
	}, passphrase); f != nil {
		return f
	}

	_ = k.eng.Audit.Record(obs.AuditEntry{
		Action: obs.ActionKeyCreated, Outcome: "created",
		Detail: fmt.Sprintf("passphrase %q", name),
	})
	return nil
}

// store writes one key: the secret to the keychain, the entry to config.yaml.
//
// The secret goes first. A config entry pointing at a handle with nothing
// behind it is the failure mode that looks like a working key until the day it
// is needed.
func (k *KeysController) store(key config.Key, secret string) *Failure {
	cfg := k.eng.Config.Config()
	existing, replacing := cfg.KeyByName(key.Name)
	if replacing {
		key.CredentialRef = existing.CredentialRef
		if existing.CreatedAt.IsZero() {
			key.CreatedAt = existing.CreatedAt
		}
	}
	if key.CredentialRef == "" {
		key.CredentialRef = config.SuggestKeyHandle(key.Kind, key.Name)
	}
	if err := k.eng.Creds.Set(key.CredentialRef, secret); err != nil {
		return Fail(err)
	}
	if replacing {
		return Fail(k.eng.Config.SaveKey(key.Name, key))
	}
	return Fail(k.eng.Config.AddKey(key))
}

// RevealedKey is the secret half, handed back once and recorded in the audit
// log.
type RevealedKey struct {
	Name string `json:"name"`
	// Secret is the age private key, or the passphrase.
	Secret  string   `json:"secret"`
	Warning string   `json:"warning"`
	Failure *Failure `json:"failure,omitempty"`
}

// Reveal returns the secret half of a key so it can be backed up or handed to
// somebody else.
//
// This is the one operation here that hands a secret back out, so it is audited
// like every other reveal in PortCloak.
func (k *KeysController) Reveal(name string) RevealedKey {
	cfg := k.eng.Config.Config()
	key, ok := cfg.KeyByName(name)
	if !ok {
		return RevealedKey{Failure: Fail(config.ErrNotFound)}
	}
	secret, err := config.Resolve(k.eng.Creds, key.CredentialRef, "the key "+name)
	if err != nil {
		return RevealedKey{Failure: Fail(err)}
	}

	_ = k.eng.Audit.Record(obs.AuditEntry{
		Action: obs.ActionKeyRevealed, Outcome: "revealed",
		Detail: fmt.Sprintf("%s key %q", key.Kind, name),
	})
	return RevealedKey{Name: name, Secret: secret, Warning: backupWarning}
}

// Rename changes a key's name and note without touching the material.
func (k *KeysController) Rename(originalName, name, note string) *Failure {
	cfg := k.eng.Config.Config()
	key, ok := cfg.KeyByName(originalName)
	if !ok {
		return Fail(config.ErrNotFound)
	}
	key.Name = strings.TrimSpace(name)
	key.Note = note
	if key.Name == "" {
		return &Failure{Message: "A key needs a name."}
	}
	return Fail(k.eng.Config.SaveKey(originalName, key))
}

// Delete removes a key and the secret behind it.
func (k *KeysController) Delete(name string) *Failure {
	cfg := k.eng.Config.Config()
	key, ok := cfg.KeyByName(name)
	if !ok {
		return Fail(config.ErrNotFound)
	}
	if err := k.eng.Config.DeleteKey(name, k.eng.Creds); err != nil {
		return Fail(err)
	}

	_ = k.eng.Audit.Record(obs.AuditEntry{
		Action: obs.ActionKeyDeleted, Outcome: "deleted",
		Detail: fmt.Sprintf("%s key %q", key.Kind, name),
		Reason: deleteWarning,
	})
	return nil
}

// Recipients lists the keys a capture can seal to, so the wizard offers names
// rather than asking for public keys to be pasted.
type Recipient struct {
	Name      string `json:"name"`
	PublicKey string `json:"publicKey"`
	Note      string `json:"note,omitempty"`
	// Openable says whether this machine also holds the private half, which
	// decides whether a snapshot sealed to it can be opened here afterwards.
	Openable bool `json:"openable"`
}

// Recipients returns the age identities available as capture recipients.
func (k *KeysController) Recipients() (res []Recipient) {
	defer func() { res = lists(res) }()
	out := []Recipient{}
	for _, key := range k.eng.Config.Config().Identities() {
		_, err := k.eng.Creds.Get(key.CredentialRef)
		out = append(out, Recipient{
			Name: key.Name, PublicKey: key.PublicKey, Note: key.Note,
			Openable: err == nil,
		})
	}
	return out
}

// keyCandidates is everything an open may try without being asked: the keys
// stored in this machine's keychain, and the keys that have already opened a
// snapshot during this run of the application.
//
// A key PortCloak generated, stored and can read is a key the operator has
// already decided to trust it with. Asking for it again at every restore is a
// prompt that teaches people to turn encryption off, so it is not asked for —
// but the name of whichever key opened the snapshot is carried back and shown.
func (e *Engine) keyCandidates() []inspect.KeyCandidate {
	var out []inspect.KeyCandidate
	for _, key := range e.Config.Config().Keys {
		secret, err := e.Creds.Get(key.CredentialRef)
		if err != nil || secret == "" {
			// A key whose secret is not on this machine is simply not a
			// candidate. That is the expected state after moving config.yaml
			// between machines, not a fault worth reporting here.
			continue
		}
		switch key.Kind {
		case config.KeyIdentity:
			out = append(out, inspect.KeyCandidate{Name: key.Name, Identity: secret})
		case config.KeyPassphrase:
			out = append(out, inspect.KeyCandidate{Name: key.Name, Passphrase: secret})
		}
	}
	return append(out, e.sessionKeys()...)
}

// sessionKeyName is what a key typed on a screen is called afterwards. It is
// deliberately a description rather than a name: nobody named this key, and
// claiming otherwise on the restore screen would be worse than saying plainly
// where it came from.
const sessionKeyName = "the key you entered earlier in this session"

// sessionKeys returns the keys that have opened a snapshot during this run.
func (e *Engine) sessionKeys() []inspect.KeyCandidate {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]inspect.KeyCandidate, len(e.unlocked))
	copy(out, e.unlocked)
	return out
}

// rememberKey keeps a key that has just proven itself against a real bundle.
//
// Unlocking a snapshot in the library and then being asked for the same key
// again by the restore wizard two clicks later is the version of this prompt
// that is hardest to defend: PortCloak had the key, used it, watched it work,
// and threw it away. It keeps it now — in memory, for the life of the process,
// written nowhere. Quitting forgets it, which is the difference between this
// and a stored key, and is why the Keys screen still exists.
//
// It is not scoped to the snapshot it opened. A passphrase that opened last
// night's capture of one realm is overwhelmingly likely to open last night's
// capture of the next, and the material is already in this process either way.
func (e *Engine) rememberKey(passphrase string, identities []string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	add := func(c inspect.KeyCandidate) {
		for _, existing := range e.unlocked {
			if existing.Passphrase == c.Passphrase && existing.Identity == c.Identity {
				return
			}
		}
		e.unlocked = append(e.unlocked, c)
	}
	if passphrase != "" {
		add(inspect.KeyCandidate{Name: sessionKeyName, Passphrase: passphrase})
	}
	for _, id := range identities {
		if strings.TrimSpace(id) != "" {
			add(inspect.KeyCandidate{Name: sessionKeyName, Identity: strings.TrimSpace(id)})
		}
	}
}

// ForgetSessionKeys drops the keys typed during this run, without touching
// anything stored. It is what "lock this again" means.
func (k *KeysController) ForgetSessionKeys() int {
	k.eng.mu.Lock()
	defer k.eng.mu.Unlock()
	n := len(k.eng.unlocked)
	k.eng.unlocked = nil
	return n
}

// Availability says whether a snapshot can be opened without asking for
// anything, and on the strength of what.
//
// The restore wizard gates its Next button on this. It cannot derive the answer
// itself: which keys exist is the engine's business, and whether one of them
// fits is not knowable until a bundle is actually read.
type Availability struct {
	// Candidates is how many keys would be tried.
	Candidates int `json:"candidates"`
	// FromSession is how many of those were typed during this run rather than
	// stored, which is worth distinguishing because quitting forgets them.
	FromSession int    `json:"fromSession"`
	Note        string `json:"note"`
}

// Availability reports what an open would have to work with.
func (k *KeysController) Availability() Availability {
	session := len(k.eng.sessionKeys())
	total := len(k.eng.keyCandidates())

	out := Availability{Candidates: total, FromSession: session}
	switch {
	case total == 0:
		out.Note = "There are no keys on this machine, so an encrypted snapshot needs the key it was sealed with."
	case session == total:
		out.Note = fmt.Sprintf("PortCloak will try the %d key(s) you have already used in this session. Quitting forgets them. Save one under Keys to keep it.", session)
	case session == 0:
		out.Note = fmt.Sprintf("PortCloak will try the %d key(s) stored on this machine.", total)
	default:
		out.Note = fmt.Sprintf("PortCloak will try %d key(s): %d stored on this machine and %d you have already used in this session.",
			total, total-session, session)
	}
	return out
}
