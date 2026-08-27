// Package obs provides structured logging, redaction, progress events and the
// audit log. It is the lowest layer in the engine: it imports nothing else from
// engine/, so every other package can log without creating an import cycle.
package obs

import (
	"regexp"
	"strings"
)

// Placeholder is what replaces a value that must not be logged. It is a fixed
// string rather than a length-preserving mask, because a mask that preserves
// length leaks the length.
const Placeholder = "[REDACTED]"

// sensitiveKeys are matched as substrings of the lower-cased, punctuation-stripped
// attribute key. A match redacts the value outright, whatever it looks like.
//
// This list is deliberately blunt. The cost of redacting something harmless is a
// slightly less useful log line; the cost of missing something is a client secret
// in a file that gets attached to a bug report.
var sensitiveKeys = []string{
	"secret",
	"password",
	"passwd",
	"pwd",
	"passphrase",
	"privatekey",
	"credential",
	"token",
	"apikey",
	"accesskey",
	"secretkey",
	"sessionkey",
	"otpseed",
	"totpseed",
	"salt",
	"hashvalue",
	"authorization",
	"cookie",
}

// benignKeys never get shape-based redaction. These are naming and identity
// fields whose values are chosen by the operator's Keycloak, not by us — and a
// realm is entirely free to contain a client literally named
// "-----BEGIN RSA PRIVATE KEY-----".
//
// Over-redaction is a real bug too, just a quieter one: it corrupts the log
// exactly when someone is using it to debug something strange.
var benignKeys = map[string]bool{
	"name": true, "clientname": true, "clientid": true, "id": true,
	"realm": true, "realmname": true, "kid": true, "alias": true,
	"path": true, "file": true, "dir": true, "host": true, "url": true,
	"msg": true, "level": true, "time": true, "source": true,
	"job": true, "jobid": true, "stage": true, "kind": true,
	"provider": true, "algorithm": true, "alg": true, "version": true,
	"namespace": true, "workload": true, "container": true, "image": true,
	"username": true, "email": true, "group": true, "role": true,
}

var (
	// A PEM block header is unambiguous: nothing benign is shaped like this
	// except a deliberately hostile name, which benignKeys already protects.
	pemRe = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*(PRIVATE KEY|CERTIFICATE|RSA|EC)`)

	// header.payload.signature, all base64url, each segment non-trivial.
	jwtRe = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)

	// A long unbroken run of base64/hex alphabet. The 40-character floor is
	// chosen to sit above the things that legitimately look like this: a UUID
	// is 36, a SHA-256 hex digest is 64 but is only ever logged under a benign
	// key, and snapshot ids are shorter still.
	blobRe = regexp.MustCompile(`^[A-Za-z0-9+/=_-]{40,}$`)

	// A path or URL is made of the same alphabet and can easily exceed the
	// floor. It is not a secret and redacting it wrecks the log line that was
	// about to tell someone which directory failed.
	pathishRe = regexp.MustCompile(`^(~|\.{1,2})?/|^[A-Za-z]:[\\/]|://`)
)

// blobAlphabet is the free-text scanning alphabet. It deliberately excludes
// '/' and '.', which appear in every filesystem path and would otherwise make
// any deep directory look like secret material. The structural rules (PEM, JWT)
// carry the cases that matter most; this is the backstop for a bare blob.
func blobAlphabet(r rune) bool {
	switch {
	case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return true
	case r == '+', r == '=', r == '_', r == '-':
		return true
	}
	return false
}

// isHexRun reports whether a run is entirely hexadecimal. A SHA-256 digest is
// 64 hex characters and is genuinely useful in a log line, so it is kept.
func isHexRun(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// normalizeKey lower-cases and strips separators so "bind_credential",
// "bindCredential" and "Bind-Credential" all reduce to "bindcredential".
func normalizeKey(k string) string {
	var b strings.Builder
	b.Grow(len(k))
	for _, r := range k {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r == '_' || r == '-' || r == '.' || r == ' ':
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// IsSensitiveKey reports whether an attribute with this key must never have its
// value logged.
func IsSensitiveKey(key string) bool {
	n := normalizeKey(key)
	if benignKeys[n] {
		return false
	}
	for _, s := range sensitiveKeys {
		if strings.Contains(n, s) {
			return true
		}
	}
	return false
}

// LooksSecret reports whether a value is shaped like secret material. This is the
// safety net for keys nobody thought to add to sensitiveKeys — it catches the
// private key that turns up under an attribute named "data".
func LooksSecret(v string) bool {
	if pemRe.MatchString(v) || jwtRe.MatchString(v) {
		return true
	}
	t := strings.TrimSpace(v)
	if pathishRe.MatchString(t) {
		return false
	}
	return blobRe.MatchString(t)
}

// RedactString applies both rules to one key/value pair and returns the value
// that is safe to write.
func RedactString(key, val string) string {
	if IsSensitiveKey(key) {
		return Placeholder
	}
	if benignKeys[normalizeKey(key)] {
		return val
	}
	if LooksSecret(val) {
		return Placeholder
	}
	return val
}

// RedactText scrubs secret-shaped substrings out of free text — an error message,
// a captured stderr line from kc.sh. Unlike RedactString this has no key to go
// on, so it can only work by shape.
func RedactText(s string) string {
	s = jwtRe.ReplaceAllString(s, Placeholder)
	s = redactPEMBlocks(s)
	return redactBlobRuns(s)
}

func redactPEMBlocks(s string) string {
	var b strings.Builder
	for {
		loc := pemRe.FindStringIndex(s)
		if loc == nil {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:loc[0]])
		b.WriteString(Placeholder)

		// Skip from the BEGIN marker to past the matching END line, or to the
		// end of the string if the block is truncated.
		rest := s[loc[0]:]
		end := strings.Index(rest, "-----END")
		if end < 0 {
			return b.String()
		}
		tail := rest[end:]
		nl := strings.Index(tail, "\n")
		if nl < 0 {
			return b.String()
		}
		s = tail[nl:]
	}
}

// redactBlobRuns replaces long unbroken runs of base64-ish characters, which is
// what a raw key or seed looks like when it lands in an error message.
func redactBlobRuns(s string) string {
	const floor = 40

	var b strings.Builder
	runes := []rune(s)
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		run := string(runes[start:end])
		if len(run) >= floor && !isHexRun(run) {
			b.WriteString(Placeholder)
		} else {
			b.WriteString(run)
		}
		start = -1
	}
	for i, r := range runes {
		if blobAlphabet(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
		b.WriteRune(r)
	}
	flush(len(runes))
	return b.String()
}
