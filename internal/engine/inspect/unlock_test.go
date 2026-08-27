package inspect_test

import (
	"context"
	"strings"
	"testing"

	"portcloak/internal/engine/crypto"
	"portcloak/internal/engine/inspect"
	"portcloak/internal/engine/snapshot"
)

// A key PortCloak generated, stored and can read is a key the operator has
// already decided to trust it with. Asking for it again at every restore is a
// prompt that teaches people to turn encryption off, so it is not asked for.
func TestOpen_UsesAStoredIdentityWithoutBeingAsked(t *testing.T) {
	priv, pub, err := crypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	home, blobs, _, req := seed(t, crypto.Config{
		Enabled: true, Mode: snapshot.EncryptionRecipients, Recipients: []string{pub},
	})

	// Nothing supplied. Only the stored key.
	req.Passphrase = ""
	req.Identities = nil
	req.Candidates = []inspect.KeyCandidate{{Name: "ops-team", Identity: priv}}

	s, err := inspect.Open(context.Background(), home, blobs, req, nil)
	if err != nil {
		t.Fatalf("a snapshot sealed to a stored key still had to be asked about: %v", err)
	}
	defer s.Close() //nolint:errcheck
	if s.Realm != "acme" {
		t.Fatalf("realm = %q", s.Realm)
	}
	// Silent is not the same as invisible: the screen has to be able to say
	// which key opened it.
	if s.UnlockedWith != "ops-team" {
		t.Errorf("UnlockedWith = %q, want the name of the key that opened it", s.UnlockedWith)
	}
}

// The same for a remembered passphrase, which carries nothing identifying it —
// so it is simply tried.
func TestOpen_UsesAStoredPassphraseWithoutBeingAsked(t *testing.T) {
	home, blobs, _, req := seed(t, crypto.Config{
		Enabled: true, Mode: snapshot.EncryptionPassphrase, Passphrase: "correct horse battery staple",
	})

	req.Passphrase = ""
	req.Candidates = []inspect.KeyCandidate{
		{Name: "stale", Passphrase: "not this one"},
		{Name: "nightly-captures", Passphrase: "correct horse battery staple"},
	}

	s, err := inspect.Open(context.Background(), home, blobs, req, nil)
	if err != nil {
		t.Fatalf("a stored passphrase was not tried: %v", err)
	}
	defer s.Close() //nolint:errcheck
	if s.UnlockedWith != "nightly-captures" {
		t.Errorf("UnlockedWith = %q; the key that actually worked should be the one named", s.UnlockedWith)
	}
}

// What the operator types wins. A stored key is a convenience, not an
// authority: if both would open the bundle, the typed one is what was meant.
func TestOpen_ASuppliedKeyIsNotAttributedToAStoredOne(t *testing.T) {
	home, blobs, _, req := seed(t, crypto.Config{
		Enabled: true, Mode: snapshot.EncryptionPassphrase, Passphrase: "shared",
	})
	req.Passphrase = "shared"
	req.Candidates = []inspect.KeyCandidate{{Name: "also-shared", Passphrase: "shared"}}

	s, err := inspect.Open(context.Background(), home, blobs, req, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close() //nolint:errcheck
	if s.UnlockedWith != "" {
		t.Errorf("UnlockedWith = %q, but the operator supplied the key themselves", s.UnlockedWith)
	}
}

// A stored key that does not fit changes nothing about the outcome, and the
// message says what was tried — "wrong key" and "no key" are different problems
// with different fixes.
func TestOpen_SaysWhatItTried(t *testing.T) {
	home, blobs, _, req := seed(t, crypto.Config{
		Enabled: true, Mode: snapshot.EncryptionPassphrase, Passphrase: "right",
	})
	req.Passphrase = ""
	req.Candidates = []inspect.KeyCandidate{{Name: "wrong-one", Passphrase: "nope"}}

	_, err := inspect.Open(context.Background(), home, blobs, req, nil)
	if err == nil {
		t.Fatal("a snapshot opened with no key that fits it")
	}
	if !strings.Contains(err.Error(), "stored on this machine") {
		t.Errorf("the failure does not say that the stored keys were tried: %v", err)
	}
}
