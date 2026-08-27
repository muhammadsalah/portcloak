// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package obs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestRedaction_SensitiveKeys(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"clientSecret", true},
		{"client_secret", true},
		{"Client-Secret", true},
		{"bindCredential", true},
		{"password", true},
		{"smtpPassword", true},
		{"privateKey", true},
		{"accessKey", true},
		{"secretKey", true},
		{"apiKey", true},
		{"authorization", true},
		{"cookie", true},
		{"otpSeed", true},
		{"salt", true},
		{"token", true},

		{"clientId", false},
		{"clientName", false},
		{"realm", false},
		{"kid", false},
		{"algorithm", false},
		{"namespace", false},
		{"workload", false},
		{"path", false},
		{"username", false},
		{"email", false},
	}
	for _, c := range cases {
		if got := IsSensitiveKey(c.key); got != c.want {
			t.Errorf("IsSensitiveKey(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}

func TestRedaction_ValueShapes(t *testing.T) {
	secrets := []string{
		"-----BEGIN RSA PRIVATE KEY-----\nMIIEow==\n-----END RSA PRIVATE KEY-----",
		"-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----",
		"eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36P",
		strings.Repeat("A", 44),
	}
	for _, s := range secrets {
		if !LooksSecret(s) {
			t.Errorf("LooksSecret(%.30q) = false, want true", s)
		}
	}

	benign := []string{
		"acme",
		"/opt/keycloak/bin/kc.sh",
		"550e8400-e29b-41d4-a716-446655440000", // a UUID is 36 chars, under the floor
		"pbkdf2-sha512",
		"statefulset/keycloak",
	}
	for _, s := range benign {
		if LooksSecret(s) {
			t.Errorf("LooksSecret(%q) = true, want false", s)
		}
	}
}

// A realm is entirely free to contain a client whose *name* looks like a PEM
// block. Over-redaction corrupts the log exactly when someone is using it to
// debug something strange, so the benign key wins.
func TestRedaction_PEMShapedClientName(t *testing.T) {
	name := "-----BEGIN RSA PRIVATE KEY----- (yes, really)"
	if got := RedactString("clientName", name); got != name {
		t.Fatalf("a client name shaped like a PEM block was redacted: %q", got)
	}
	if got := RedactString("name", name); got != name {
		t.Fatalf("a name shaped like a PEM block was redacted: %q", got)
	}
	// The same text under a key nobody thought about must still be caught.
	if got := RedactString("data", name); got != Placeholder {
		t.Fatalf("RedactString(data, pem) = %q, want the placeholder", got)
	}
}

func TestRedaction_HandlerScrubsAttrs(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewRedactingHandler(slog.NewJSONHandler(&buf, nil)))

	log.Info("connecting",
		slog.String("host", "kc-01.internal"),
		slog.String("bindCredential", "hunter2"),
		slog.Group("storage",
			slog.String("bucket", "iam-snapshots"),
			slog.String("secretKey", "AKIA-not-really"),
		),
		slog.Group("credentials", slog.String("anything", "still-a-secret")),
		slog.Any("err", errors.New("ssh: handshake failed for eyJhbGciOiJIUzI1NiJ9.eyJhIjoxfQ.c2lnbmF0dXJlAAAA")),
	)

	out := buf.String()
	for _, leaked := range []string{"hunter2", "AKIA-not-really", "still-a-secret", "eyJhbGciOiJIUzI1NiJ9"} {
		if strings.Contains(out, leaked) {
			t.Errorf("log line leaked %q:\n%s", leaked, out)
		}
	}
	for _, kept := range []string{"kc-01.internal", "iam-snapshots", "ssh: handshake failed"} {
		if !strings.Contains(out, kept) {
			t.Errorf("log line lost the useful value %q:\n%s", kept, out)
		}
	}
}

func TestRedaction_HandlerScrubsMessage(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewRedactingHandler(slog.NewJSONHandler(&buf, nil)))
	log.Info("kc.sh wrote: -----BEGIN EC PRIVATE KEY-----\nabc\n-----END EC PRIVATE KEY-----\ndone")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	msg, _ := rec["msg"].(string)
	if strings.Contains(msg, "BEGIN EC PRIVATE KEY") {
		t.Fatalf("message kept a PEM block: %q", msg)
	}
	if !strings.Contains(msg, "done") {
		t.Fatalf("message lost the text after the PEM block: %q", msg)
	}
}

// A type that formats itself is exactly where a secret would otherwise slip
// past a key-based check.
type selfFormatting struct{ v string }

func (s selfFormatting) String() string { return s.v }

func TestRedaction_LogValuerAndStringer(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewRedactingHandler(slog.NewJSONHandler(&buf, nil)))
	log.Info("x", slog.Any("payload", selfFormatting{v: strings.Repeat("Z", 50)}))
	if strings.Contains(buf.String(), strings.Repeat("Z", 50)) {
		t.Fatalf("a Stringer leaked secret-shaped material: %s", buf.String())
	}
}

func TestRedaction_WithAttrsAndGroups(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewRedactingHandler(slog.NewJSONHandler(&buf, nil))).
		With(slog.String("password", "hunter2")).
		WithGroup("job")
	log.Info("started", slog.String("realm", "acme"))
	if strings.Contains(buf.String(), "hunter2") {
		t.Fatalf("WithAttrs leaked: %s", buf.String())
	}
}

func TestRedactText_TruncatedPEM(t *testing.T) {
	in := "warning: -----BEGIN PRIVATE KEY-----\nMIIE"
	got := RedactText(in)
	if strings.Contains(got, "BEGIN PRIVATE KEY") {
		t.Fatalf("truncated PEM block survived: %q", got)
	}
	if !strings.HasPrefix(got, "warning: ") {
		t.Fatalf("text before the block was lost: %q", got)
	}
}

func TestAuditLog_IsRedacted(t *testing.T) {
	dir := t.TempDir()
	a, err := NewAuditLog(dir + "/audit.log")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Record(AuditEntry{
		Action:  ActionSecretReveal,
		Outcome: "revealed",
		Detail:  "clients[app-web].secret",
		Reason:  fmt.Sprintf("ticket OPS-12 %s", strings.Repeat("Q", 48)),
	}); err != nil {
		t.Fatal(err)
	}
	entries, err := a.Read(AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if strings.Contains(entries[0].Reason, strings.Repeat("Q", 48)) {
		t.Fatalf("audit entry kept secret-shaped material: %q", entries[0].Reason)
	}
	if entries[0].Detail != "clients[app-web].secret" {
		t.Fatalf("audit entry lost its location: %q", entries[0].Detail)
	}
	if entries[0].Host == "" {
		t.Error("audit entry should record the machine, since there is no user to record")
	}
}
