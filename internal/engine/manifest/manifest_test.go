// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package manifest_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"portcloak/internal/engine/manifest"
	"portcloak/internal/engine/realm"
)

const richDir = "../../../testdata/exports/rich"

func buildRich(t *testing.T, opts manifest.BuildOptions) *manifest.Manifest {
	t.Helper()
	rep, err := realm.Load(filepath.Join(richDir, "acme-realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	if opts.UserFiles == nil {
		opts.UserFiles = []string{
			filepath.Join(richDir, "acme-users-0.json"),
			filepath.Join(richDir, "acme-users-1.json"),
		}
	}
	if opts.Source.CaptureMode == "" {
		opts.Source = manifest.Source{
			Kind: "local", KeycloakVersion: "25.0.2",
			CaptureMode: "offline-export", ExecutionMode: "in-place",
			SecretVerification: "skipped", DependencyScan: "skipped",
			UsersMode: "different_files",
		}
	}
	m, err := manifest.Build(context.Background(), rep, opts)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestBuild_InventoryMatchesTheSource(t *testing.T) {
	m := buildRich(t, manifest.BuildOptions{})

	if m.Realm != "acme" {
		t.Fatalf("realm parsed as %q", m.Realm)
	}
	want := manifest.Counts{
		Users: 5, Clients: 4, ClientScopes: 2, RealmRoles: 2, ClientRoles: 2,
		Groups: 3, IdentityProviders: 2, Federations: 1, KeyProviders: 5,
		AuthFlows: 2, RequiredActions: 1,
	}
	if m.Counts != want {
		t.Errorf("counts = %+v\nwant     %+v", m.Counts, want)
	}
}

// Credential presence is what answers "will this user's 2FA survive the move",
// and it has to be counted without any value being read.
func TestBuild_CredentialCounts(t *testing.T) {
	m := buildRich(t, manifest.BuildOptions{})
	c := m.Credentials

	if c.PasswordHashes != 3 {
		t.Errorf("password hashes = %d, want 3", c.PasswordHashes)
	}
	if c.OTP != 3 {
		t.Errorf("otp enrolments = %d, want 3", c.OTP)
	}
	if c.WebAuthn != 3 {
		t.Errorf("passkeys = %d, want 3", c.WebAuthn)
	}
	if c.RecoveryCodes != 1 {
		t.Errorf("recovery codes = %d, want 1", c.RecoveryCodes)
	}
	// Both credential shapes Keycloak has used must be read.
	if c.Algorithms["pbkdf2-sha512"] != 2 || c.Algorithms["pbkdf2-sha256"] != 1 {
		t.Errorf("password algorithms = %v", c.Algorithms)
	}
}

// The ledger is safe to read, screenshot and export precisely because it
// enumerates locations and kinds without ever holding a value.
func TestLedger_ContainsNoValues(t *testing.T) {
	m := buildRich(t, manifest.BuildOptions{})

	encoded, err := json.Marshal(m.Secrets)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{
		"app-web-real-secret", "backoffice-real-secret", "ldap-bind-password",
		"azure-secret-value", "google-secret-value", "smtp-password-value",
		"authenticator-secret-value", "keystore-password-value",
		"s3cr3t-webhook-value", "MIIEowIBAAKCAQEAxxxx",
	} {
		if strings.Contains(string(encoded), secret) {
			t.Errorf("the secret ledger contains the value %q", secret)
		}
	}
	// And the whole manifest, since the ledger is not the only place a value
	// could leak into.
	whole, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"app-web-real-secret", "ldap-bind-password", "smtp-password-value"} {
		if strings.Contains(string(whole), secret) {
			t.Errorf("the manifest contains the value %q", secret)
		}
	}
}

func TestLedger_EnumeratesEverySecretBearingLocation(t *testing.T) {
	m := buildRich(t, manifest.BuildOptions{})

	byLocation := map[string]manifest.Secret{}
	for _, s := range m.Secrets {
		byLocation[s.Location] = s
	}

	expect := map[string]manifest.SecretKind{
		"clients[app-web].secret":                                manifest.SecretClient,
		"clients[backoffice-api].secret":                         manifest.SecretClient,
		"clients[legacy-portal].secret":                          manifest.SecretClient,
		"components[keys/rsa-generated].config.privateKey":       manifest.SecretKeyPrivate,
		"components[keys/hmac-generated].config.secret":          manifest.SecretKeyPrivate,
		"components[keys/corp-keystore].config.keystorePassword": manifest.SecretKeyPrivate,
		"components[ldap/corp].config.bindCredential":            manifest.SecretLDAPBind,
		"identityProviders[azure-ad].config.clientSecret":        manifest.SecretIdP,
		"identityProviders[google].config.clientSecret":          manifest.SecretIdP,
		"smtpServer.password":                                    manifest.SecretSMTP,
		"authenticatorConfig[step-up-otp].config.secret":         manifest.SecretAuthConfig,
		"attributes.acme.webhookSecret":                          manifest.SecretAttribute,
	}
	for loc, kind := range expect {
		got, ok := byLocation[loc]
		if !ok {
			t.Errorf("the ledger is missing %s", loc)
			continue
		}
		if got.Kind != kind {
			t.Errorf("%s is recorded as %s, want %s", loc, got.Kind, kind)
		}
	}

	// A public client has no secret to carry, so it must not appear at all.
	if _, present := byLocation["clients[app-spa].secret"]; present {
		t.Error("a public client was given a ledger entry")
	}
}

// A client secret exported as a placeholder imports perfectly and then fails
// silently at the first authentication, so it must be flagged, not shipped.
func TestBuild_MaskedSecretIsFlaggedPartialAndNamesTheClient(t *testing.T) {
	m := buildRich(t, manifest.BuildOptions{VerificationRan: true})

	var legacy manifest.Secret
	for _, s := range m.Secrets {
		if s.Location == "clients[legacy-portal].secret" {
			legacy = s
		}
	}
	if !legacy.Masked || legacy.Carried {
		t.Fatalf("the masked secret was not flagged: %+v", legacy)
	}
	if legacy.Note == "" {
		t.Error("a masked secret should say what happened")
	}

	var clients manifest.Category
	for _, c := range m.Completeness.Categories {
		if c.Name == "Clients" {
			clients = c
		}
	}
	if clients.Status != manifest.Partial {
		t.Fatalf("clients came out %q, want partial", clients.Status)
	}
	if !strings.Contains(clients.Reason, "legacy-portal") {
		t.Errorf("the reason does not name the client: %q", clients.Reason)
	}
	if m.Completeness.Verdict != manifest.VerdictPartial {
		t.Errorf("verdict = %q, want Partial", m.Completeness.Verdict)
	}

	// The corresponding client row loses its secret-present flag, which is the
	// column an operator actually reads.
	for _, c := range m.Clients {
		if c.ClientID == "legacy-portal" && c.SecretPresent {
			t.Error("a client whose secret was masked still reports secretPresent")
		}
	}
}

// A tool that reports "sessions: missing" on every capture trains its operator
// to ignore the report, and then the one real missing goes unread.
func TestCompleteness_OutOfScopeIsNotMissing(t *testing.T) {
	m := buildRich(t, manifest.BuildOptions{VerificationRan: true, DependencyScanRan: true})

	if len(m.Completeness.By(manifest.Missing)) != 0 {
		t.Fatalf("a healthy snapshot reported missing categories: %+v", m.Completeness.By(manifest.Missing))
	}
	outOfScope := m.Completeness.By(manifest.OutOfScope)
	names := map[string]bool{}
	for _, c := range outOfScope {
		names[c.Name] = true
		if c.Reason == "" {
			t.Errorf("%s is out of scope with no explanation", c.Name)
		}
	}
	for _, want := range []string{"Online sessions", "Offline sessions", "Custom theme files", "Provider and SPI JARs"} {
		if !names[want] {
			t.Errorf("%q is not recorded as out of scope", want)
		}
	}
}

// "Not checked" and "there are none" are different answers, and only one of
// them is safe to act on.
func TestCompleteness_SkippedChecksAreNotReportedAsClean(t *testing.T) {
	m := buildRich(t, manifest.BuildOptions{})

	var verification, dependencies manifest.Category
	for _, c := range m.Completeness.Categories {
		switch c.Name {
		case "Secret verification":
			verification = c
		case "External dependency detection":
			dependencies = c
		}
	}
	if verification.Status != manifest.NotChecked {
		t.Errorf("secret verification came out %q when it did not run", verification.Status)
	}
	if dependencies.Status != manifest.NotChecked {
		t.Errorf("dependency detection came out %q when it did not run", dependencies.Status)
	}
	if !strings.Contains(dependencies.Reason, "does not mean") {
		t.Errorf("the reason should say that silence is not evidence: %q", dependencies.Reason)
	}
}

// This is the feature that stands in for session portability, so it has to be
// derivable from the manifest alone.
func TestManifest_TokenContinuity(t *testing.T) {
	m := buildRich(t, manifest.BuildOptions{})

	key, ok := m.ActiveSigningKey()
	if !ok {
		t.Fatal("no active signing key was identified")
	}
	if key.KID != "abc123" || key.Algorithm != "RS256" {
		t.Fatalf("the active signing key is %+v", key)
	}
	if !key.PrivateCarried {
		t.Error("the active signing key did not report its private material as carried")
	}

	preserved, sentence := m.TokenContinuity()
	if !preserved {
		t.Fatalf("token continuity reported lost: %s", sentence)
	}
	if !strings.Contains(sentence, "abc123") {
		t.Errorf("the sentence does not name the key: %q", sentence)
	}
}

// An encryption key is not a signing key, and picking it would report
// continuity that does not exist.
func TestManifest_ActiveSigningKeyIgnoresEncryptionKeys(t *testing.T) {
	m := buildRich(t, manifest.BuildOptions{})
	key, _ := m.ActiveSigningKey()
	if strings.Contains(key.Provider, "enc") || key.Use == "enc" {
		t.Fatalf("an encryption key was chosen as the signing key: %+v", key)
	}
}

func TestBuild_KeyProvidersCarryTheirTypeAndPrivateFlag(t *testing.T) {
	m := buildRich(t, manifest.BuildOptions{})

	byName := map[string]manifest.Key{}
	for _, k := range m.Keys {
		byName[k.Name] = k
	}
	cases := map[string]struct {
		kind    string
		private bool
	}{
		"rsa-generated":     {"RSA", true},
		"rsa-enc-generated": {"RSA", true},
		"hmac-generated":    {"HMAC", true},
		"aes-generated":     {"AES", true},
		"corp-keystore":     {"keystore", true},
	}
	for name, want := range cases {
		got, ok := byName[name]
		if !ok {
			t.Errorf("key provider %s is missing", name)
			continue
		}
		if got.Type != want.kind {
			t.Errorf("%s has type %q, want %q", name, got.Type, want.kind)
		}
		if got.PrivateCarried != want.private {
			t.Errorf("%s reports privateCarried=%v", name, got.PrivateCarried)
		}
	}
	if byName["corp-keystore"].KeystoreFile != "/opt/keycloak/conf/corp.p12" {
		t.Error("a java-keystore provider should record the file it points at")
	}
}

func TestBuild_FederationRecordsWhetherTheBindTravelled(t *testing.T) {
	m := buildRich(t, manifest.BuildOptions{})
	if len(m.Federations) != 1 {
		t.Fatalf("got %d federations", len(m.Federations))
	}
	f := m.Federations[0]
	if f.Name != "corp" || f.Provider != "ldap" {
		t.Fatalf("federation = %+v", f)
	}
	if !f.BindCarried {
		t.Error("the LDAP bind credential travelled but was not recorded as carried")
	}
	if f.Mappers != 3 {
		t.Errorf("mapper count = %d, want 3", f.Mappers)
	}
	if f.BindDN == "" || f.ConnectionURL == "" {
		t.Error("the federation summary lost its non-secret configuration")
	}
}

// A keystore file is a dependency detectable from the realm alone, without any
// Admin API involved.
func TestBuild_KeystoreFileIsReportedAsADependency(t *testing.T) {
	m := buildRich(t, manifest.BuildOptions{DependencyScanRan: true})

	found := false
	for _, d := range m.ExternalDependencies {
		if d.Type == manifest.DependencyKeystore && d.Name == "/opt/keycloak/conf/corp.p12" {
			found = true
			if d.Action != manifest.ProvisionAction {
				t.Errorf("action = %q", d.Action)
			}
			if d.ReferencedBy == "" {
				t.Error("a dependency should say what in the realm needs it")
			}
			if d.Consequence == "" {
				t.Error("a dependency should state the consequence of its absence")
			}
		}
	}
	if !found {
		t.Error("the keystore a key provider points at was not reported")
	}
}

func TestBuild_SidecarIsSecretFree(t *testing.T) {
	m := buildRich(t, manifest.BuildOptions{VerificationRan: true, DependencyScanRan: true})
	sidecar := m.BuildSidecar("01HZY3", "2026-08-27T09:14:00Z", "0.0.1", true, "recipients", 1024, "root-hash")

	encoded, err := json.Marshal(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	// The sidecar is the one artifact deliberately written in the clear, and
	// therefore the one most likely to leak by accident.
	for _, forbidden := range []string{
		"app-web-real-secret", "ldap-bind-password", "smtp-password-value",
		"clients[app-web].secret", "components[ldap/corp]", "j.doe", "jane.doe@acme.example",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("the sidecar contains %q", forbidden)
		}
	}
	if sidecar.SecretCount == 0 {
		t.Error("the sidecar should say how many secrets the bundle carries, by count")
	}
	if sidecar.Realm != "acme" || sidecar.Counts.Users != 5 {
		t.Errorf("the sidecar lost the facts the library needs: %+v", sidecar)
	}
	if !sidecar.TokenContinuity {
		t.Error("the sidecar should surface token continuity without opening the bundle")
	}
}

func TestIsMasked(t *testing.T) {
	masked := []string{"**********", "*****", "********************", "MASKED", "<masked>"}
	for _, v := range masked {
		if !IsMaskedHelper(v) {
			t.Errorf("IsMasked(%q) = false", v)
		}
	}
	real := []string{"", "app-web-real-secret", "s3cr3t*", "a*b*c"}
	for _, v := range real {
		if IsMaskedHelper(v) {
			t.Errorf("IsMasked(%q) = true, which would discard a real secret", v)
		}
	}
}

func IsMaskedHelper(v string) bool { return manifest.IsMasked(v) }

func TestBuild_UsersInTheRealmFileAreCountedToo(t *testing.T) {
	rep := &realm.Representation{
		Realm: "small",
		Users: []realm.User{
			{Username: "a", Credentials: []realm.Credential{{Type: "password", Algorithm: "pbkdf2-sha512"}}},
			{Username: "b"},
		},
	}
	m, err := manifest.Build(context.Background(), rep, manifest.BuildOptions{
		Source: manifest.Source{Kind: "local", CaptureMode: "offline-export", ExecutionMode: "in-place", UsersMode: "realm_file"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.Counts.Users != 2 || m.Credentials.PasswordHashes != 1 {
		t.Fatalf("realm_file users were not counted: %+v / %+v", m.Counts, m.Credentials)
	}
	// A realm with no users at all is worth a word, in case it is unexpected.
	empty, err := manifest.Build(context.Background(), &realm.Representation{Realm: "empty"}, manifest.BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(empty.Completeness.Warnings, " ")
	if !strings.Contains(joined, "no users") {
		t.Errorf("an empty realm produced no warning: %v", empty.Completeness.Warnings)
	}
}
