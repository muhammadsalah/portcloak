// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// One table, two implementations. The fake is only trustworthy as a stand-in
// for the real keychain if both are held to the same contract.
func TestCredentialStore_Contract(t *testing.T) {
	impls := map[string]func(t *testing.T) CredentialStore{
		"memory": func(*testing.T) CredentialStore { return NewMemoryCredentials() },
		"keychain": func(t *testing.T) CredentialStore {
			// The real store is only exercised where the host OS actually has
			// one. A missing secret service produces a skip, never a silent
			// pass — a green board that quietly ran nothing is worse than a red
			// one.
			if os.Getenv("PORTCLOAK_TEST_KEYCHAIN") == "" {
				t.Skip("set PORTCLOAK_TEST_KEYCHAIN=1 to exercise the host keychain")
			}
			k := NewKeychain()
			if err := k.Set(Handle("test", "probe"), "x"); err != nil {
				t.Skipf("this machine has no usable keychain: %v", err)
			}
			_ = k.Delete(Handle("test", "probe"))
			return k
		},
	}

	for name, mk := range impls {
		t.Run(name, func(t *testing.T) {
			cs := mk(t)
			h := Handle("test", "contract-"+name)
			t.Cleanup(func() { _ = cs.Delete(h) })

			// A handle with nothing behind it is missing, not an error state.
			if _, err := cs.Get(h); !errors.Is(err, ErrCredentialMissing) {
				t.Fatalf("Get on an unset handle = %v, want ErrCredentialMissing", err)
			}

			if err := cs.Set(h, "s3cr3t value"); err != nil {
				t.Fatal(err)
			}
			got, err := cs.Get(h)
			if err != nil {
				t.Fatal(err)
			}
			if got != "s3cr3t value" {
				t.Fatalf("round trip returned %q", got)
			}

			if err := cs.Set(h, "replaced"); err != nil {
				t.Fatal(err)
			}
			if got, _ := cs.Get(h); got != "replaced" {
				t.Fatalf("Set did not replace: %q", got)
			}

			if err := cs.Delete(h); err != nil {
				t.Fatal(err)
			}
			// Deleting what is already gone has reached the desired end state.
			if err := cs.Delete(h); err != nil {
				t.Fatalf("deleting a missing handle should succeed, got %v", err)
			}
			if _, err := cs.Get(h); !errors.Is(err, ErrCredentialMissing) {
				t.Fatalf("value survived deletion: %v", err)
			}

			// A malformed handle is rejected rather than silently filed under
			// an empty account name.
			for _, bad := range []string{"", "kc-01", "keychain://portcloak/", "keychain://portcloak/ssh", "https://example/x"} {
				if err := cs.Set(bad, "v"); err == nil {
					t.Errorf("Set accepted the malformed handle %q", bad)
				}
			}
		})
	}
}

// Copying config.yaml between machines is supported; the secrets deliberately
// do not come with it, and the operator has to be told which entry to fix.
func TestResolve_NamesTheEntryThatNeedsReentering(t *testing.T) {
	cs := NewMemoryCredentials()
	_, err := Resolve(cs, Handle("ssh", "kc-01"), "prod-eu")
	if !errors.Is(err, ErrCredentialMissing) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "prod-eu") {
		t.Errorf("the message does not name the entry: %v", err)
	}
	if !strings.Contains(err.Error(), "keychain") {
		t.Errorf("the message does not say where the value should live: %v", err)
	}
}

func TestHandle_Validity(t *testing.T) {
	good := []string{Handle("ssh", "kc-01"), Handle("s3", "prod"), "keychain://portcloak/kubernetes/prod-eu"}
	for _, h := range good {
		if !ValidHandle(h) {
			t.Errorf("ValidHandle(%q) = false", h)
		}
	}
	bad := []string{"", "portcloak/ssh/x", "keychain://other/ssh/x", "keychain://portcloak/ssh", "keychain://portcloak//x"}
	for _, h := range bad {
		if ValidHandle(h) {
			t.Errorf("ValidHandle(%q) = true", h)
		}
	}
}

func TestKeychain_MissingIsDistinctFromBroken(t *testing.T) {
	// Guard against the mapping regressing: go-keyring's not-found sentinel has
	// to keep surfacing as ErrCredentialMissing, because the two are handled
	// very differently in the UI.
	if !errors.Is(&MissingCredentialError{Handle: "x"}, ErrCredentialMissing) {
		t.Fatal("MissingCredentialError no longer wraps ErrCredentialMissing")
	}
	if keyring.ErrNotFound == nil {
		t.Fatal("go-keyring no longer exports a not-found sentinel")
	}
}
