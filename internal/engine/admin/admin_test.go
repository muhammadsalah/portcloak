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
	"portcloak/internal/engine/manifest"
	"portcloak/internal/engine/realm"
)

// fakeKeycloak stands in for a running server. It is a real HTTP server rather
// than a stubbed client, so the token exchange and error handling are exercised
// as they would be against Keycloak itself.
type fakeKeycloak struct {
	*httptest.Server
	clients    []map[string]any
	serverInfo map[string]any
	rejectAuth bool
	realmData  map[string]any
	requests   []string
}

func newFakeKeycloak(t *testing.T) *fakeKeycloak {
	t.Helper()
	f := &fakeKeycloak{
		serverInfo: map[string]any{
			"themes": map[string]any{
				"login": []map[string]any{{"name": "keycloak"}, {"name": "acme-login"}},
			},
			"deployments": []map[string]any{
				{"name": "acme-authenticator-2.1.jar", "providers": []string{"acme-step-up"}},
			},
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/realms/master/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		if f.rejectAuth {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "a-token", "expires_in": 60})
	})
	mux.HandleFunc("/admin/serverinfo", func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.URL.Path)
		_ = json.NewEncoder(w).Encode(f.serverInfo)
	})
	mux.HandleFunc("/admin/realms/acme/clients", func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.URL.Path)
		_ = json.NewEncoder(w).Encode(f.clients)
	})
	mux.HandleFunc("/admin/realms/acme", func(w http.ResponseWriter, r *http.Request) {
		if f.realmData == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(f.realmData)
	})
	mux.HandleFunc("/admin/realms/", func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/users/count"):
			_, _ = w.Write([]byte("48213"))
		case strings.HasSuffix(r.URL.Path, "/keys"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"keys": []map[string]any{{"kid": "abc123", "algorithm": "RS256", "use": "SIG"}},
			})
		default:
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		}
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func newClient(t *testing.T, f *fakeKeycloak) *admin.Client {
	t.Helper()
	creds := config.NewMemoryCredentials()
	handle := config.Handle("k8s", "prod")
	if err := creds.Set(handle, "an-admin-password"); err != nil {
		t.Fatal(err)
	}
	c, err := admin.New(config.Environment{
		Name: "prod", Kind: config.EnvKubernetes,
		AdminBaseURL: f.URL, AdminUser: "admin", AdminCredentialRef: handle,
	}, creds)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// An environment with no Admin API is a supported configuration, not an error.
func TestNew_NoAdminAPIConfiguredIsNotAnError(t *testing.T) {
	c, err := admin.New(config.Environment{Name: "laptop", Kind: config.EnvLocal}, config.NewMemoryCredentials())
	if err != nil {
		t.Fatalf("an environment without an Admin API produced an error: %v", err)
	}
	if c != nil {
		t.Fatal("expected no client")
	}
	// Every method has to be safe on the nil client, because the capture path
	// calls them without checking.
	if c.Reachable(context.Background()) {
		t.Error("a nil client reported itself reachable")
	}
	if _, err := c.Realms(context.Background()); err != nil {
		t.Errorf("Realms on a nil client: %v", err)
	}
	if _, err := c.VerifySecrets(context.Background(), "acme", nil); err != nil {
		t.Errorf("VerifySecrets on a nil client: %v", err)
	}
	if _, err := c.DetectDependencies(context.Background(), "acme", &realm.Representation{}); err != nil {
		t.Errorf("DetectDependencies on a nil client: %v", err)
	}
}

func TestReachable(t *testing.T) {
	f := newFakeKeycloak(t)
	c := newClient(t, f)

	if !c.Reachable(context.Background()) {
		t.Fatal("a running server reported unreachable")
	}
	f.rejectAuth = true
	// The token is cached, so a fresh client is needed to see the rejection.
	fresh := newClient(t, f)
	if fresh.Reachable(context.Background()) {
		t.Fatal("rejected credentials reported as reachable")
	}
}

// A client secret exported as a placeholder imports perfectly and then fails
// silently at the first authentication.
func TestSecretVerification_DetectsMask(t *testing.T) {
	f := newFakeKeycloak(t)
	f.clients = []map[string]any{
		{"clientId": "app-web", "secret": "a-real-secret"},
		{"clientId": "legacy-portal", "secret": "**********"},
	}
	c := newClient(t, f)

	masked, err := c.VerifySecrets(context.Background(), "acme", []manifest.Secret{
		{Kind: manifest.SecretClient, Location: "clients[app-web].secret", Carried: true},
		{Kind: manifest.SecretClient, Location: "clients[legacy-portal].secret", Carried: false, Masked: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(masked) != 1 {
		t.Fatalf("got %d findings: %v", len(masked), masked)
	}
	reason, found := masked["clients[legacy-portal].secret"]
	if !found {
		t.Fatalf("the masked secret was not flagged: %v", masked)
	}
	if !strings.Contains(reason, "legacy-portal") {
		t.Errorf("the reason does not name the client: %q", reason)
	}
}

// The Admin API masks on some versions, which says nothing about the export.
// Treating that as a finding would flag every client on those versions.
func TestSecretVerification_AdminAPIMaskingIsNotAFinding(t *testing.T) {
	f := newFakeKeycloak(t)
	f.clients = []map[string]any{{"clientId": "app-web", "secret": "**********"}}
	c := newClient(t, f)

	masked, err := c.VerifySecrets(context.Background(), "acme", []manifest.Secret{
		{Kind: manifest.SecretClient, Location: "clients[app-web].secret", Carried: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(masked) != 0 {
		t.Fatalf("a masked Admin API response was reported as an export problem: %v", masked)
	}
}

// An unrecognised state is reported as "could not verify" rather than assumed
// good. Assuming good is how a dud secret ships.
func TestSecretVerification_UnknownClientIsReportedNotAssumedGood(t *testing.T) {
	f := newFakeKeycloak(t)
	f.clients = []map[string]any{{"clientId": "app-web", "secret": "a-real-secret"}}
	c := newClient(t, f)

	masked, err := c.VerifySecrets(context.Background(), "acme", []manifest.Secret{
		{Kind: manifest.SecretClient, Location: "clients[retired-app].secret", Carried: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	reason, found := masked["clients[retired-app].secret"]
	if !found {
		t.Fatal("a client that could not be checked was silently passed")
	}
	if !strings.Contains(reason, "could not confirm") {
		t.Errorf("the reason should say it could not confirm: %q", reason)
	}
}

// A masked private key means tokens signed before the move stop verifying, so
// it is a finding of its own.
func TestSecretVerification_MaskedKeyMaterialIsFlagged(t *testing.T) {
	f := newFakeKeycloak(t)
	c := newClient(t, f)

	masked, err := c.VerifySecrets(context.Background(), "acme", []manifest.Secret{
		{Kind: manifest.SecretKeyPrivate, Location: "components[keys/rsa].config.privateKey", Masked: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	reason := masked["components[keys/rsa].config.privateKey"]
	if !strings.Contains(reason, "token continuity") {
		t.Errorf("the reason does not say what is lost: %q", reason)
	}
}

func TestDependencyScan_FindsWhatTheRealmActuallyReferences(t *testing.T) {
	f := newFakeKeycloak(t)
	c := newClient(t, f)

	rep := &realm.Representation{
		Realm:        "acme",
		LoginTheme:   "acme-login",
		AccountTheme: "keycloak.v2",
		AuthenticationFlows: []realm.AuthenticationFlow{
			{Alias: "browser", Executions: []realm.FlowExecution{
				{Authenticator: "auth-cookie"},
				{Authenticator: "acme-step-up"},
			}},
		},
	}

	deps, err := c.DetectDependencies(context.Background(), "acme", rep)
	if err != nil {
		t.Fatal(err)
	}

	byName := map[string]manifest.Dependency{}
	for _, d := range deps {
		byName[d.Name] = d
	}

	theme, ok := byName["acme-login"]
	if !ok {
		t.Fatalf("the custom theme the realm selects was not detected: %+v", deps)
	}
	if theme.Type != manifest.DependencyTheme {
		t.Errorf("theme detected as %q", theme.Type)
	}
	if theme.DetectedAt == "" {
		t.Error("the theme's detected path was not recorded")
	}
	if !strings.Contains(theme.Consequence, "fails at login") {
		t.Errorf("the consequence is not stated plainly: %q", theme.Consequence)
	}

	jar, ok := byName["acme-authenticator-2.1.jar"]
	if !ok {
		t.Fatalf("the provider JAR the realm's flow needs was not detected: %+v", deps)
	}
	if jar.Type != manifest.DependencyProvider {
		t.Errorf("provider detected as %q", jar.Type)
	}
	if !strings.Contains(jar.ReferencedBy, "acme-step-up") {
		t.Errorf("the dependency does not say what needs it: %q", jar.ReferencedBy)
	}

	// A built-in theme is not a dependency, and neither is a built-in
	// authenticator.
	if _, present := byName["keycloak.v2"]; present {
		t.Error("a built-in theme was reported as a dependency")
	}
	if _, present := byName["auth-cookie"]; present {
		t.Error("a built-in authenticator was reported as a dependency")
	}
}

// Every deployed theme reported as a dependency would make the list worthless.
func TestDependencyScan_NoFalsePositives(t *testing.T) {
	f := newFakeKeycloak(t)
	// A theme deployed on the server but not referenced by this realm.
	f.serverInfo["themes"] = map[string]any{
		"login": []map[string]any{{"name": "keycloak"}, {"name": "someone-elses-theme"}},
	}
	c := newClient(t, f)

	deps, err := c.DetectDependencies(context.Background(), "acme", &realm.Representation{
		Realm: "acme", LoginTheme: "keycloak", AccountTheme: "keycloak.v2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 0 {
		t.Fatalf("a realm using only built-in themes reported %d dependencies: %+v", len(deps), deps)
	}
}

func TestReadRealm_MissingRealmIsNotAnError(t *testing.T) {
	f := newFakeKeycloak(t)
	c := newClient(t, f)

	counts, err := c.ReadRealm(context.Background(), "acme")
	if err != nil {
		t.Fatalf("reading a realm that does not exist should not be an error: %v", err)
	}
	if counts.Exists {
		t.Fatal("a missing realm reported as existing")
	}
}

func TestReadRealm_ReadsCountsAndKeys(t *testing.T) {
	f := newFakeKeycloak(t)
	f.realmData = map[string]any{"realm": "acme", "enabled": true}
	c := newClient(t, f)

	counts, err := c.ReadRealm(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if !counts.Exists {
		t.Fatal("an existing realm reported as missing")
	}
	if counts.Users != 48213 {
		t.Errorf("user count = %d", counts.Users)
	}
	// The key ids are the token-continuity check after a restore.
	if len(counts.KeyIDs) != 1 || counts.KeyIDs[0] != "abc123" {
		t.Errorf("key ids = %v", counts.KeyIDs)
	}
}

func TestAuthFailure_IsPlainAndNotRetried(t *testing.T) {
	f := newFakeKeycloak(t)
	f.rejectAuth = true
	c := newClient(t, f)

	_, err := c.Realms(context.Background())
	if err == nil {
		t.Fatal("rejected credentials were accepted")
	}
	if !strings.Contains(err.Error(), "rejected the credentials") {
		t.Errorf("the message is not a sentence about the operator's situation: %v", err)
	}
}
