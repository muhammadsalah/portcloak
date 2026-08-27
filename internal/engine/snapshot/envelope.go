// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Package snapshot seals a realm export into the portable, self-describing,
// integrity-protected unit PortCloak produces and consumes.
//
// One snapshot contains exactly one realm. That is why the realm's artifacts
// sit at the root of the bundle with no realms/<name>/ nesting to navigate, and
// why the realm can partition a storage backend cleanly.
package snapshot

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// SchemaVersion is the compatibility contract that actually matters while the
// major version is 0: bundles outlive the tool that wrote them, so a 0.0.1
// bundle must stay readable by later 0.x builds. Changing the envelope requires
// bumping this and keeping a read path for the previous version.
const SchemaVersion = "1.0"

// Paths inside the bundle.
const (
	EnvelopePath     = "envelope.json"
	ManifestPath     = "manifest.json"
	ProvenancePath   = "provenance.json"
	DependenciesPath = "dependencies.json"
	IntegrityPath    = "integrity.json"
	// RealmDir holds the artifacts kc.sh export produced, unaltered.
	RealmDir = "realm/"
)

// EncryptionMode is how a bundle was encrypted, when it was.
type EncryptionMode string

const (
	EncryptionNone       EncryptionMode = ""
	EncryptionPassphrase EncryptionMode = "passphrase"
	EncryptionRecipients EncryptionMode = "recipients"
)

// Encryption records what kind of artifact this is — enough to know whether you
// can open it before you try.
type Encryption struct {
	Enabled bool           `json:"enabled"`
	Mode    EncryptionMode `json:"mode,omitempty"`
	// Recipients is how many age public keys can open it. The keys themselves
	// are recorded so an operator can tell whose they are.
	Recipients []string `json:"recipients,omitempty"`
}

// Warning is the sentence shown wherever an unencrypted bundle appears.
//
// This is the one place a slightly uncomfortable label is the correct design:
// an operator must never be able to say afterwards that they did not realise
// the file held unmasked secrets.
const UnencryptedWarning = "This snapshot is not encrypted. It holds unmasked client secrets, LDAP bind credentials, IdP secrets, SMTP passwords and RSA private signing keys in the clear — holding the file is equivalent to holding the realm."

// Envelope is the bundle's self-description. It carries no secret, so it can be
// read and rendered the moment a bundle is decrypted, and its non-secret subset
// can be published as a sidecar for keyless listing.
type Envelope struct {
	SchemaVersion    string     `json:"schemaVersion"`
	SnapshotID       string     `json:"snapshotId"`
	Realm            string     `json:"realm"`
	CreatedAt        time.Time  `json:"createdAt"`
	PortCloakVersion string     `json:"portcloakVersion"`
	KeycloakVersion  string     `json:"keycloakVersion,omitempty"`
	Encryption       Encryption `json:"encryption"`
	// IntegrityRoot is the root of the checksum tree over the bundle's
	// artifacts, repeated here so a reader that has the envelope knows what to
	// expect before reading integrity.json.
	IntegrityRoot string `json:"integrityRoot"`
	ArtifactCount int    `json:"artifactCount"`
	PayloadBytes  int64  `json:"payloadBytes"`
}

// Provenance records where a snapshot came from and how it was taken.
type Provenance struct {
	EnvironmentName string    `json:"environmentName"`
	EnvironmentKind string    `json:"environmentKind"`
	Target          string    `json:"target,omitempty"`
	KeycloakVersion string    `json:"keycloakVersion,omitempty"`
	CaptureMode     string    `json:"captureMode"`
	ExecutionMode   string    `json:"executionMode"`
	CloneRef        string    `json:"cloneRef,omitempty"`
	CloneDestroyed  bool      `json:"cloneDestroyed"`
	Ports           string    `json:"ports,omitempty"`
	UsersMode       string    `json:"usersMode,omitempty"`
	StartedAt       time.Time `json:"startedAt"`
	FinishedAt      time.Time `json:"finishedAt"`
	// SecretVerification is passed, partial, or skipped. It is never omitted:
	// "not checked" and "fine" are different answers.
	SecretVerification string `json:"secretVerification"`
	DependencyScan     string `json:"dependencyScan"`
	JobID              string `json:"jobId,omitempty"`
	// IntegrityRoot is repeated so a provenance record read on its own is
	// still tied to the bundle it describes.
	IntegrityRoot string `json:"integrityRoot,omitempty"`
}

// NewID produces a time-ordered, sortable snapshot identifier.
//
// Time-ordered matters because the storage layout puts the id after a timestamp
// and an operator scanning a bucket with ls should see chronological order even
// if two snapshots land in the same minute.
func NewID(at time.Time) string {
	var buf [10]byte
	binary.BigEndian.PutUint64(buf[:8], uint64(at.UTC().UnixMilli()))
	if _, err := rand.Read(buf[6:]); err != nil {
		// crypto/rand failing is not a condition worth degrading for: an id
		// without entropy would collide inside a single millisecond.
		panic(fmt.Sprintf("reading random bytes for a snapshot id: %v", err))
	}
	return crockford(buf[:])
}

const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func crockford(b []byte) string {
	var sb strings.Builder
	var acc, bits uint32
	for _, c := range b {
		acc = acc<<8 | uint32(c)
		bits += 8
		for bits >= 5 {
			bits -= 5
			sb.WriteByte(crockfordAlphabet[(acc>>bits)&0x1f])
		}
	}
	if bits > 0 {
		sb.WriteByte(crockfordAlphabet[(acc<<(5-bits))&0x1f])
	}
	return sb.String()
}
