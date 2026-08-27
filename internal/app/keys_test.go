package app

import (
	"strings"
	"testing"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/obs"
)

// keyEngine is an engine whose credential store is in memory, so a test run
// never touches the operator's real keychain.
func keyEngine(t *testing.T) *Engine {
	t.Helper()
	eng := emptyEngine(t)
	eng.Creds = config.NewMemoryCredentials()
	return eng
}

// Generating a key used to hand the private half back once and store nothing,
// which left the operator as the key management system. The point of a key is
// that PortCloak keeps it.
func TestKeys_GeneratedKeyIsStoredAndUsable(t *testing.T) {
	eng := keyEngine(t)
	keys := NewKeysController(eng)

	gen := keys.Generate("ops-team", "the shared restore key")
	if gen.Failure != nil {
		t.Fatalf("a key could not be generated: %s", gen.Failure.Message)
	}
	if !strings.HasPrefix(gen.PublicKey, "age1") || !strings.HasPrefix(gen.PrivateKey, "AGE-SECRET-KEY-1") {
		t.Fatalf("that is not an age keypair: %q / %q", gen.PublicKey, gen.PrivateKey)
	}
	if gen.Warning == "" {
		t.Error("a stored key was handed back with nothing said about backing it up")
	}

	view := keys.List()
	if len(view.Keys) != 1 {
		t.Fatalf("got %d keys", len(view.Keys))
	}
	k := view.Keys[0]
	if !k.Present || !k.Usable {
		t.Errorf("a key just generated is not usable: %+v", k)
	}
	if k.PublicKey != gen.PublicKey {
		t.Errorf("the recorded public key is not the one generated")
	}
	if view.Unlockable != 1 {
		t.Errorf("Unlockable = %d, want 1", view.Unlockable)
	}

	// config.yaml carries the handle, never the secret.
	if strings.Contains(k.CredentialRef, gen.PrivateKey) {
		t.Fatal("the private key leaked into the credential handle")
	}
	stored, err := eng.Creds.Get(k.CredentialRef)
	if err != nil {
		t.Fatalf("the secret half is not in the keychain: %v", err)
	}
	if stored != gen.PrivateKey {
		t.Error("the stored secret is not the key that was generated")
	}

	// And it is offered to a capture as a named recipient rather than as a
	// string to paste.
	recipients := keys.Recipients()
	if len(recipients) != 1 || recipients[0].Name != "ops-team" || !recipients[0].Openable {
		t.Errorf("the key is not offered as an openable recipient: %+v", recipients)
	}
}

// An imported key derives its own public half. Asking an operator to paste both
// halves of a keypair they already hold is asking them to get it wrong.
func TestKeys_ImportDerivesThePublicHalf(t *testing.T) {
	eng := keyEngine(t)
	keys := NewKeysController(eng)

	gen := keys.Generate("original", "")
	if gen.Failure != nil {
		t.Fatal(gen.Failure.Message)
	}
	if f := keys.Delete("original"); f != nil {
		t.Fatal(f.Message)
	}

	if f := keys.ImportIdentity("re-imported", gen.PrivateKey, "brought back"); f != nil {
		t.Fatalf("a valid age key was rejected: %s", f.Message)
	}
	cfg := eng.Config.Config()
	k, ok := cfg.KeyByName("re-imported")
	if !ok {
		t.Fatal("the imported key was not recorded")
	}
	if k.PublicKey != gen.PublicKey {
		t.Errorf("the derived public key %q is not the original %q", k.PublicKey, gen.PublicKey)
	}

	if f := keys.ImportIdentity("nonsense", "not-a-key", ""); f == nil {
		t.Error("a string that is not an age key was accepted")
	}
}

// Deleting a key takes the secret with it. Leaving it in the keychain would
// mean "deleted" was only true of the part an operator can see.
func TestKeys_DeleteRemovesTheSecretToo(t *testing.T) {
	eng := keyEngine(t)
	keys := NewKeysController(eng)

	gen := keys.Generate("temporary", "")
	if gen.Failure != nil {
		t.Fatal(gen.Failure.Message)
	}
	ref := eng.Config.Config().Keys[0].CredentialRef

	if f := keys.Delete("temporary"); f != nil {
		t.Fatal(f.Message)
	}
	if len(eng.Config.Config().Keys) != 0 {
		t.Error("the key entry survived its deletion")
	}
	if _, err := eng.Creds.Get(ref); err == nil {
		t.Error("the secret half survived the deletion of the key that named it")
	}
	if keys.List().DeleteWarning == "" {
		t.Error("deletion is offered without saying what it costs")
	}
}

// A remembered passphrase becomes a candidate the same way an identity does.
func TestKeys_CandidatesCoverBothKinds(t *testing.T) {
	eng := keyEngine(t)
	keys := NewKeysController(eng)

	if gen := keys.Generate("an-identity", ""); gen.Failure != nil {
		t.Fatal(gen.Failure.Message)
	}
	if f := keys.SavePassphrase("a-passphrase", "correct horse battery staple", ""); f != nil {
		t.Fatal(f.Message)
	}

	got := map[string]string{}
	for _, c := range eng.keyCandidates() {
		switch {
		case c.Identity != "":
			got[c.Name] = "identity"
		case c.Passphrase != "":
			got[c.Name] = "passphrase"
		}
	}
	if got["an-identity"] != "identity" || got["a-passphrase"] != "passphrase" {
		t.Errorf("the stored keys do not both become candidates: %+v", got)
	}

	// A key whose secret is missing on this machine is not a candidate, and is
	// shown as what it is rather than offered as usable.
	if err := eng.Creds.Delete(eng.Config.Config().Keys[0].CredentialRef); err != nil {
		t.Fatal(err)
	}
	if n := len(eng.keyCandidates()); n != 1 {
		t.Errorf("got %d candidates after removing one secret, want 1", n)
	}
	for _, k := range keys.List().Keys {
		if k.Name == "an-identity" && k.Usable {
			t.Error("a key with no secret on this machine is offered as usable")
		}
	}
}

// Revealing the secret half is the one operation here that hands a secret back
// out, so it is audited like every other reveal in PortCloak.
func TestKeys_RevealIsAudited(t *testing.T) {
	eng := keyEngine(t)
	keys := NewKeysController(eng)

	gen := keys.Generate("ops-team", "")
	if gen.Failure != nil {
		t.Fatal(gen.Failure.Message)
	}

	got := keys.Reveal("ops-team")
	if got.Failure != nil {
		t.Fatal(got.Failure.Message)
	}
	if got.Secret != gen.PrivateKey {
		t.Error("Reveal did not return the key that was stored")
	}

	entries, err := eng.Audit.Read(obs.AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range entries {
		if e.Action == "keyRevealed" {
			found = true
			if strings.Contains(e.Detail, gen.PrivateKey) {
				t.Fatal("the audit log recorded the key itself")
			}
		}
	}
	if !found {
		t.Error("revealing a key wrote no audit entry")
	}
}
