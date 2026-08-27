// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"portcloak/internal/engine/admin"
	"portcloak/internal/engine/config"
	"portcloak/internal/engine/resil"
)

// An internal Keycloak behind a self-signed or private-CA certificate is an
// ordinary deployment, and it is the one this tool is most often pointed at.
// httptest.NewTLSServer serves exactly that: a certificate signed by an
// authority no machine carries.

func selfSignedKeycloak(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/realms/master/protocol/openid-connect/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "a-token", "expires_in": 60})
	})
	mux.HandleFunc("/admin/realms", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"realm": "acme"}})
	})
	s := httptest.NewTLSServer(mux)
	t.Cleanup(s.Close)
	return s
}

func clientFor(t *testing.T, url string, insecure bool) *admin.Client {
	t.Helper()
	creds := config.NewMemoryCredentials()
	handle := config.Handle("k8s", "prod")
	if err := creds.Set(handle, "an-admin-password"); err != nil {
		t.Fatal(err)
	}
	c, err := admin.New(config.Environment{
		Name: "prod", Kind: config.EnvKubernetes,
		AdminBaseURL: url, AdminUser: "admin", AdminCredentialRef: handle,
		AdminInsecureTLS: insecure,
	}, creds)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Without the setting the certificate is refused — which is the correct default
// and must stay one. What matters is that the refusal says so.
func TestSelfSignedCertificate_IsRefusedAndSaysWhy(t *testing.T) {
	c := clientFor(t, selfSignedKeycloak(t).URL, false)

	err := c.Check(context.Background())
	if err == nil {
		t.Fatal("a certificate signed by an unknown authority was accepted by default")
	}

	msg := err.Error()
	if !strings.Contains(msg, "certificate") {
		t.Errorf("the failure does not mention a certificate, so it reads as an unreachable server: %q", msg)
	}
	if !strings.Contains(strings.ToLower(resil.Hint(err)), "self-signed") {
		t.Errorf("the advice does not name the setting that accepts one: %q", resil.Hint(err))
	}

	// And it is terminal. A certificate this machine does not trust will not
	// become trusted on the fourth attempt, so retrying spends the whole budget
	// proving that — which is what happened before every transport failure here
	// was classified as retryable.
	if resil.IsRetryable(err) {
		t.Error("an untrusted certificate is being retried")
	}
}

// With it, the same server works — and the flag reaches the transport rather
// than merely being stored.
func TestSelfSignedCertificate_IsAcceptedWhenTheEnvironmentAsksForIt(t *testing.T) {
	c := clientFor(t, selfSignedKeycloak(t).URL, true)

	if err := c.Check(context.Background()); err != nil {
		t.Fatalf("the environment opted in and the certificate was still refused: %v", err)
	}
	realms, err := c.Realms(context.Background())
	if err != nil {
		t.Fatalf("a request over the accepted connection failed: %v", err)
	}
	if len(realms) != 1 || realms[0] != "acme" {
		t.Errorf("expected the one realm the server serves, got %v", realms)
	}
}

// The opt-in is per environment. One environment accepting a certificate must
// not quietly relax the next, which a shared or default transport would do.
func TestSelfSignedCertificate_TheOptInDoesNotLeakToOtherEnvironments(t *testing.T) {
	server := selfSignedKeycloak(t)

	if err := clientFor(t, server.URL, true).Check(context.Background()); err != nil {
		t.Fatalf("the opted-in environment failed: %v", err)
	}
	if err := clientFor(t, server.URL, false).Check(context.Background()); err == nil {
		t.Error("an environment that did not opt in accepted the certificate after another one did")
	}
}
