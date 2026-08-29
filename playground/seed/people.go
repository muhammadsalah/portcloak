// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"fmt"
	mathrand "math/rand"
	"strings"
)

// person is one generated human, in the parts both Keycloak and LDAP need.
type person struct {
	Username   string
	First      string
	Last       string
	Email      string
	Dept       string
	Location   string
	Employee   string
	Groups     []string
	Roles      []string
	OTP        bool
	Passkey    bool
	Disabled   bool
	Unverified bool
	// Awkward marks a user whose data contains something somebody assumed
	// would never appear in it. See awkward().
	Awkward bool
}

var (
	firstNames = []string{
		"Amina", "Bjorn", "Chidi", "Dagny", "Elif", "Farid", "Greta", "Hiro",
		"Ines", "Jomo", "Katarzyna", "Lars", "Mei", "Nadia", "Omar", "Priya",
		"Quentin", "Rania", "Sanjay", "Tove", "Ugo", "Vera", "Wei", "Yara", "Zoë",
	}
	lastNames = []string{
		"Adeyemi", "Bianchi", "Chen", "Dvořák", "Eriksen", "Fernández", "Gupta",
		"Haddad", "Ivanova", "Jónsdóttir", "Kowalski", "Lindqvist", "Mbeki",
		"Nakamura", "O'Brien", "Petrov", "Quiroga", "Rossi", "Silva", "Tanaka",
	}
	departments = []string{
		"Platform", "Payments", "Identity", "Support", "Field Engineering",
		"Data", "Security", "Finance", "People", "Legal",
	}
	locations = []string{
		"Amsterdam", "Bengaluru", "Cairo", "Dublin", "Helsinki", "Lagos",
		"Montréal", "Osaka", "São Paulo", "Tallinn",
	}
)

// awkward is the set of usernames and attribute values that have broken
// something somewhere: a plus in an address, a quote in a name, an apostrophe,
// a character outside the basic plane, a string long enough to hit a column
// limit. They are a fixed fraction of every generated realm rather than an
// option, because the realm nobody opts into is the realm nobody tests.
func awkward(r *mathrand.Rand) (username, first, last string) {
	set := [][3]string{
		{"o'brien.sean", "Seán", "O'Brien"},
		{"user+tagged", "Plus", "Tagged"},
		{"quote\"name", "Quo\"te", "Nameson"},
		{"unicode.zoë", "Zoë", "Ünïcodé"},
		{"very.long." + strings.Repeat("segment.", 12) + "end", "Long", "Name"},
		{"backslash\\user", "Back", "Slash"},
		{"spaced name", "Spaced", "Name"},
		{"日本語.user", "太郎", "山田"},
	}
	pick := set[r.Intn(len(set))]
	return pick[0], pick[1], pick[2]
}

// people generates n humans, deterministically from the seed.
//
// The distributions are the point. Roughly a third carry OTP, a fifth carry a
// passkey, a handful carry both, a few are disabled and a few have an
// unverified address — because a realm where every user is identical proves
// only that one shape survives a round trip.
func people(r *mathrand.Rand, n int, groups, roles []string) []person {
	out := make([]person, 0, n)
	for i := range n {
		p := person{
			First:    firstNames[r.Intn(len(firstNames))],
			Last:     lastNames[r.Intn(len(lastNames))],
			Dept:     departments[r.Intn(len(departments))],
			Location: locations[r.Intn(len(locations))],
			Employee: fmt.Sprintf("E%06d", 100000+i),
		}
		p.Username = fmt.Sprintf("%s.%s%d",
			strings.ToLower(ascii(p.First)), strings.ToLower(ascii(p.Last)), i)

		// One in forty is deliberately difficult.
		if r.Intn(40) == 0 {
			p.Username, p.First, p.Last = awkward(r)
			p.Username = fmt.Sprintf("%s%d", p.Username, i)
			p.Awkward = true
		}
		p.Email = fmt.Sprintf("%s@example.com", strings.ToLower(ascii(p.Username)))

		p.OTP = r.Intn(3) == 0
		p.Passkey = r.Intn(5) == 0
		p.Disabled = r.Intn(50) == 0
		p.Unverified = r.Intn(12) == 0

		for _, g := range pickSome(r, groups, 0, 3) {
			p.Groups = append(p.Groups, g)
		}
		for _, role := range pickSome(r, roles, 0, 4) {
			p.Roles = append(p.Roles, role)
		}
		out = append(out, p)
	}
	return out
}

// pickSome returns between min and max distinct members of from.
func pickSome(r *mathrand.Rand, from []string, minN, maxN int) []string {
	if len(from) == 0 {
		return nil
	}
	n := minN + r.Intn(maxN-minN+1)
	if n > len(from) {
		n = len(from)
	}
	perm := r.Perm(len(from))[:n]
	out := make([]string, 0, n)
	for _, i := range perm {
		out = append(out, from[i])
	}
	return out
}

// ascii strips what a username generated from a name cannot carry, so the
// awkward cases are the ones chosen deliberately rather than the ones that
// arrive by accident from a name with a diacritic in it.
func ascii(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		case r == 'ø' || r == 'ó' || r == 'ö':
			b.WriteRune('o')
		case r == 'é' || r == 'ë' || r == 'ě':
			b.WriteRune('e')
		case r == 'á' || r == 'à' || r == 'ä':
			b.WriteRune('a')
		case r == 'ř' || r == 'ß':
			b.WriteRune('s')
		}
	}
	return b.String()
}

// ── Credentials ─────────────────────────────────────────────────────────────
//
// These are written in the shape Keycloak's importer reads, which is not the
// shape its admin API returns. A password is given in the clear and hashed on
// import — the alternative is reimplementing PBKDF2 to produce something the
// server will immediately rehash.
//
// The OTP secret is real: a base32 seed a phone would accept, so a user
// generated here can genuinely be logged in as. The passkey is not — a WebAuthn
// credential is bound to an authenticator that exists, and no generator can
// produce a private key held in somebody's laptop. It is structurally valid,
// it imports, it exports, and PortCloak carries it; it will not authenticate.
// That is the right trade for a fixture whose job is to prove the enrolment
// survives a move.

type credential struct {
	Type           string `json:"type"`
	UserLabel      string `json:"userLabel,omitempty"`
	Value          string `json:"value,omitempty"`
	Temporary      bool   `json:"temporary,omitempty"`
	SecretData     string `json:"secretData,omitempty"`
	CredentialData string `json:"credentialData,omitempty"`
}

func passwordCredential(username string) credential {
	return credential{Type: "password", Value: username + "-secret", Temporary: false}
}

func otpCredential(r *mathrand.Rand) credential {
	raw := make([]byte, 20)
	for i := range raw {
		raw[i] = byte(r.Intn(256))
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)

	secretData, _ := json.Marshal(map[string]string{"value": secret})
	credentialData, _ := json.Marshal(map[string]any{
		"subType":   "totp",
		"digits":    6,
		"counter":   0,
		"period":    30,
		"algorithm": "HmacSHA1",
	})
	return credential{
		Type:           "otp",
		UserLabel:      "Authenticator app",
		SecretData:     string(secretData),
		CredentialData: string(credentialData),
	}
}

func passkeyCredential(r *mathrand.Rand) credential {
	id := make([]byte, 32)
	key := make([]byte, 64)
	aaguid := make([]byte, 16)
	for _, b := range [][]byte{id, key, aaguid} {
		for i := range b {
			b[i] = byte(r.Intn(256))
		}
	}
	secretData, _ := json.Marshal(map[string]string{})
	credentialData, _ := json.Marshal(map[string]any{
		"credentialId":         base64.RawURLEncoding.EncodeToString(id),
		"credentialPublicKey":  base64.RawURLEncoding.EncodeToString(key),
		"aaguid":               fmt.Sprintf("%x-%x-%x-%x-%x", aaguid[0:4], aaguid[4:6], aaguid[6:8], aaguid[8:10], aaguid[10:16]),
		"counter":              0,
		"attestationStatement": "",
		// No credentialType. WebAuthnCredentialData accepts exactly seven
		// properties and this is not one of them, and Jackson is configured to
		// fail on an unknown field rather than skip it — so one extra key here
		// takes down the whole realm import with a 500 and "Cannot parse the
		// JSON", which says nothing about which JSON or which field.
		"transports":                 []string{"internal", "hybrid"},
		"attestationStatementFormat": "none",
	})
	return credential{
		Type:           "webauthn-passwordless",
		UserLabel:      "Passkey (generated, not usable for sign-in)",
		SecretData:     string(secretData),
		CredentialData: string(credentialData),
	}
}

// randomID is used where Keycloak wants an id it would otherwise generate. It
// does not come from the seeded generator: an id has to be unique across runs,
// while everything the fidelity check compares has to be reproducible within
// one.
func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
