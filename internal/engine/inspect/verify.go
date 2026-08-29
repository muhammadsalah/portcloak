// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Checking a snapshot without restoring it, and the dependency view that goes
// with it. Both read the open session and contact nothing.
package inspect

import (
	"portcloak/internal/engine/manifest"
)

// VerifyReport is the result of checking a snapshot without restoring it.
type VerifyReport struct {
	SnapshotID string `json:"snapshotId"`
	Realm      string `json:"realm"`
	Message    string `json:"message"`
	Root       string `json:"root"`
	// Note states plainly that nothing was contacted, because "is my backup
	// good" should be a routine, safe check rather than a restore drill.
	Note        string             `json:"note"`
	Artifacts   []VerifiedArtifact `json:"artifacts"`
	OK          bool               `json:"ok"`
	Decryptable bool               `json:"decryptable"`
}

// VerifiedArtifact is one artifact's result.
type VerifiedArtifact struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Digest string `json:"digest,omitempty"`
	Note   string `json:"note,omitempty"`
}

// VerifyReport recomputes the integrity tree for an open snapshot.
func (s *Session) VerifyReport() VerifyReport {
	r := VerifyReport{
		SnapshotID:  s.ID,
		Realm:       s.Realm,
		OK:          s.Verify.OK,
		Message:     s.Verify.Message,
		Decryptable: s.Verify.Decryptable,
		Root:        s.Verify.RootActual,
		Note:        "No environment was contacted. Verification reads the snapshot only.",
	}
	for _, a := range s.Verify.Artifacts {
		r.Artifacts = append(r.Artifacts, VerifiedArtifact{
			Name: a.Name, OK: a.OK, Digest: a.Actual, Note: a.Note,
		})
	}
	return r
}

// Dependencies is the external dependency view, shared by capture, inspection
// and the restore preconditions step — one set of records, three views.
func (s *Session) Dependencies() []manifest.Dependency { return s.Manifest.ExternalDependencies }
