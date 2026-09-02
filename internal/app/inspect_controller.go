// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"fmt"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/inspect"
	"portcloak/internal/engine/inspect/index"
	"portcloak/internal/engine/manifest"
	"portcloak/internal/engine/obs"
	"portcloak/internal/engine/snapshot"
)

// InspectController is the snapshot inspector.
type InspectController struct{ eng *Engine }

// NewInspectController binds the inspector.
func NewInspectController(eng *Engine) *InspectController { return &InspectController{eng: eng} }

// ServiceName is the name internal/desktop logs this service under. It is
// not the address a bound method is called by — see the comment on
// controllers there, which is where reading it as one caused real damage.
func (i *InspectController) ServiceName() string { return "InspectController" }

// OpenRequest is what the operator supplied to open a snapshot.
type OpenRequest struct {
	Storage    string   `json:"storage"`
	BundleKey  string   `json:"bundleKey"`
	SnapshotID string   `json:"snapshotId"`
	Passphrase string   `json:"passphrase"`
	Identities []string `json:"identities"`
}

// Overview is the inspector's first tab.
type Overview struct {
	SnapshotID string `json:"snapshotId"`
	Realm      string `json:"realm"`
	Storage    string `json:"storage"`
	BundleKey  string `json:"bundleKey"`

	Encrypted      bool   `json:"encrypted"`
	EncryptionMode string `json:"encryptionMode,omitempty"`
	// UnlockedWith names the stored key that opened this snapshot without being
	// asked for. A key used silently is still a key an operator gets to see the
	// name of.
	UnlockedWith string `json:"unlockedWith,omitempty"`
	// Warning is the unmissable label an unencrypted bundle carries.
	Warning string `json:"warning,omitempty"`

	Counts       manifest.Counts           `json:"counts"`
	Credentials  manifest.CredentialCounts `json:"credentials"`
	Settings     manifest.Settings         `json:"settings"`
	Completeness manifest.Completeness     `json:"completeness"`
	Provenance   snapshot.Provenance       `json:"provenance"`

	// TokenContinuity is the feature that stands in for session portability, so
	// it is surfaced prominently rather than buried in the manifest.
	TokenContinuity     bool   `json:"tokenContinuity"`
	TokenContinuityNote string `json:"tokenContinuityNote"`

	IntegrityOK      bool   `json:"integrityOk"`
	IntegrityMessage string `json:"integrityMessage"`
	// Degraded marks a snapshot that could not be proven intact. It opens
	// read-only for diagnosis and restore is blocked.
	Degraded     bool   `json:"degraded"`
	DegradedNote string `json:"degradedNote,omitempty"`

	Dependencies []manifest.Dependency `json:"dependencies"`
	SecretCount  int                   `json:"secretCount"`
	IndexNote    string                `json:"indexNote"`
	Failure      *Failure              `json:"failure,omitempty"`
}

// Open fetches, decrypts, verifies and reads a snapshot.
func (i *InspectController) Open(req OpenRequest) (res Overview) {
	defer func() { res = lists(res) }()
	cfg := i.eng.Config.Config()
	st, ok := cfg.StorageByName(req.Storage)
	if !ok {
		return Overview{Failure: Fail(config.ErrNotFound)}
	}
	blobs, err := i.eng.storeFor(st)
	if err != nil {
		return Overview{Failure: Fail(err)}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	rep := obs.NewReporter(req.SnapshotID, i.eng.Sink())
	session, err := inspect.Open(ctx, i.eng.Home(), blobs, inspect.OpenRequest{
		Storage: req.Storage, BundleKey: req.BundleKey, SnapshotID: req.SnapshotID,
		Passphrase: req.Passphrase, Identities: req.Identities,
		// The keys already in this machine's keychain are tried without being
		// asked for. Whichever one worked is named on the way back.
		Candidates: i.eng.keyCandidates(),
	}, rep)
	_ = blobs.Close()
	if err != nil {
		return Overview{Failure: Fail(err)}
	}

	// A key that has just opened a real bundle has proven itself, so it is not
	// asked for again in this session — least of all by the restore wizard two
	// clicks later. It is kept in memory only; quitting forgets it.
	if session.UnlockedWith == "" {
		i.eng.rememberKey(req.Passphrase, req.Identities)
	}

	i.eng.putSession(session)
	return i.overview(session)
}

func (i *InspectController) overview(s *inspect.Session) Overview {
	continuity, note := s.Manifest.TokenContinuity()
	total, _, _ := s.Manifest.SecretCounts()

	o := Overview{
		SnapshotID: s.ID, Realm: s.Realm, Storage: s.Storage, BundleKey: s.BundleKey,
		Encrypted:       s.Envelope.Encryption.Enabled,
		EncryptionMode:  string(s.Envelope.Encryption.Mode),
		UnlockedWith:    s.UnlockedWith,
		Counts:          s.Manifest.Counts,
		Credentials:     s.Manifest.Credentials,
		Settings:        s.Manifest.Settings,
		Completeness:    s.Manifest.Completeness,
		Provenance:      s.Provenance,
		TokenContinuity: continuity, TokenContinuityNote: note,
		IntegrityOK:      s.Verify.OK,
		IntegrityMessage: s.Verify.Message,
		Degraded:         s.Degraded(),
		Dependencies:     s.Dependencies(),
		SecretCount:      total,
		IndexNote:        "Index: built on open, destroyed when this snapshot is closed.",
	}
	if !s.Envelope.Encryption.Enabled {
		o.Warning = snapshot.UnencryptedWarning
	}
	if o.Degraded {
		o.DegradedNote = "This snapshot could not be proven intact, so it is open for diagnosis only. Restore is blocked until it verifies."
	}
	return o
}

// Reopen returns the overview for a snapshot that is already open.
func (i *InspectController) Reopen(snapshotID string) (res Overview) {
	defer func() { res = lists(res) }()
	s, err := i.eng.Session(snapshotID)
	if err != nil {
		return Overview{Failure: Fail(err)}
	}
	return i.overview(s)
}

// UsersQuery is one page request from the Users tab.
type UsersQuery struct {
	SnapshotID     string `json:"snapshotId"`
	Query          string `json:"query"`
	Enabled        string `json:"enabled"`
	Origin         string `json:"origin"`
	SecondFactor   string `json:"secondFactor"`
	RealmRole      string `json:"realmRole"`
	ClientRole     string `json:"clientRole"`
	Client         string `json:"client"`
	Group          string `json:"group"`
	RequiredAction string `json:"requiredAction"`
	Sort           string `json:"sort"`
	Descending     bool   `json:"descending"`
	Offset         int    `json:"offset"`
	Limit          int    `json:"limit"`
}

func (q UsersQuery) filter() index.UserFilter {
	f := index.UserFilter{
		Query: q.Query, Origin: q.Origin, SecondFactor: q.SecondFactor,
		RealmRole: q.RealmRole, ClientRole: q.ClientRole, Client: q.Client,
		Group: q.Group, RequiredAction: q.RequiredAction,
	}
	switch q.Enabled {
	case "true":
		t := true
		f.Enabled = &t
	case "false":
		fa := false
		f.Enabled = &fa
	}
	return f
}

// UsersResult is a page of users plus the facet panel.
type UsersResult struct {
	Page   index.UserPage `json:"page"`
	Facets index.Facets   `json:"facets"`
	// Note states the credential boundary wherever the table appears.
	Note string `json:"note"`
	// Empty explains a result with nothing in it, listing the active filters,
	// so an over-narrow filter set is obvious rather than mysterious.
	Empty   string   `json:"empty,omitempty"`
	Failure *Failure `json:"failure,omitempty"`
}

// Users returns a page of the users table.
func (i *InspectController) Users(q UsersQuery) (res UsersResult) {
	defer func() { res = lists(res) }()
	s, err := i.eng.Session(q.SnapshotID)
	if err != nil {
		return UsersResult{Failure: Fail(err)}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	rep := obs.NewReporter(q.SnapshotID, i.eng.Sink())
	idx, err := s.Index(ctx, rep)
	if err != nil {
		return UsersResult{Failure: Fail(err)}
	}

	filter := q.filter()
	sortBy := index.SortUsername
	switch q.Sort {
	case "email":
		sortBy = index.SortEmail
	case "enabled":
		sortBy = index.SortEnabled
	case "created":
		sortBy = index.SortCreated
	}

	page, err := idx.Users(ctx, filter, sortBy, q.Descending, q.Offset, q.Limit)
	if err != nil {
		return UsersResult{Failure: Fail(err)}
	}
	facets, err := idx.Facets(ctx, filter)
	if err != nil {
		return UsersResult{Failure: Fail(err)}
	}

	out := UsersResult{
		Page: page, Facets: facets,
		Note: "Credential presence only. Hashes, OTP seeds and passkey material are never shown.",
	}
	if page.Total == 0 {
		out.Empty = describeActiveFilters(q)
	}
	return out
}

// describeActiveFilters lists what is narrowing a result to nothing.
func describeActiveFilters(q UsersQuery) string {
	var parts []string
	if q.Query != "" {
		parts = append(parts, fmt.Sprintf("search %q", q.Query))
	}
	if q.Enabled != "" {
		parts = append(parts, "status "+q.Enabled)
	}
	if q.Origin != "" {
		parts = append(parts, "origin "+q.Origin)
	}
	if q.SecondFactor != "" {
		parts = append(parts, "second factor "+q.SecondFactor)
	}
	if q.RealmRole != "" {
		parts = append(parts, "realm role "+q.RealmRole)
	}
	if q.ClientRole != "" {
		parts = append(parts, "client role "+q.ClientRole)
	}
	if q.Group != "" {
		parts = append(parts, "group "+q.Group)
	}
	if q.RequiredAction != "" {
		parts = append(parts, "required action "+q.RequiredAction)
	}
	if len(parts) == 0 {
		return "This snapshot carries no users."
	}
	return "No users match. Active filters: " + join(parts, ", ") + "."
}

func join(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}

// User returns one account's full detail.
func (i *InspectController) User(snapshotID, userID string) (res inspect.UserDetail, fail *Failure) {
	defer func() { res = lists(res) }()
	s, err := i.eng.Session(snapshotID)
	if err != nil {
		return inspect.UserDetail{}, Fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	detail, err := s.User(ctx, userID)
	if err != nil {
		return inspect.UserDetail{}, Fail(err)
	}
	return detail, nil
}

// Entities is every non-user view the inspector shows.
type Entities struct {
	Clients           []manifest.ClientSummary           `json:"clients"`
	ClientScopes      []manifest.ClientScopeSummary      `json:"clientScopes"`
	RealmRoles        []manifest.RoleSummary             `json:"realmRoles"`
	ClientRoles       []manifest.RoleSummary             `json:"clientRoles"`
	Groups            []manifest.GroupSummary            `json:"groups"`
	Keys              []manifest.Key                     `json:"keys"`
	IdentityProviders []manifest.IdentityProviderSummary `json:"identityProviders"`
	Federations       []manifest.FederationSummary       `json:"federations"`
	Flows             []manifest.FlowSummary             `json:"flows"`
	Dependencies      []manifest.Dependency              `json:"dependencies"`
	// DependencyNote distinguishes "none" from "not checked".
	DependencyNote string   `json:"dependencyNote"`
	Failure        *Failure `json:"failure,omitempty"`
}

// Entities returns everything except users.
func (i *InspectController) Entities(snapshotID string) (res Entities) {
	defer func() { res = lists(res) }()
	s, err := i.eng.Session(snapshotID)
	if err != nil {
		return Entities{Failure: Fail(err)}
	}
	m := s.Manifest
	out := Entities{
		Clients: m.Clients, ClientScopes: m.ClientScopes,
		RealmRoles: m.RealmRoles, ClientRoles: m.ClientRoles,
		Groups: m.Groups, Keys: m.Keys,
		IdentityProviders: m.IdentityProviders, Federations: m.Federations,
		Flows: m.Flows, Dependencies: m.ExternalDependencies,
	}
	switch {
	case m.Source.DependencyScan != "completed":
		out.DependencyNote = "Not checked. Dependency detection did not run when this snapshot was captured, so the absence of a list here does not mean the realm has none."
	case len(m.ExternalDependencies) == 0:
		out.DependencyNote = "None. This realm references no themes, provider JARs or keystore files outside the realm itself."
	default:
		out.DependencyNote = "Provision these on the destination before importing, or the realm imports cleanly and then fails at login."
	}
	return out
}

// LedgerView is the secret ledger tab.
type LedgerView struct {
	Entries []inspect.LedgerEntry `json:"entries"`
	Summary string                `json:"summary"`
	Note    string                `json:"note"`
	// RevealAllowed reflects the preference that lets an operator rule out
	// secret extraction while keeping inspection available.
	RevealAllowed bool     `json:"revealAllowed"`
	Failure       *Failure `json:"failure,omitempty"`
}

// Ledger returns the secret ledger.
func (i *InspectController) Ledger(snapshotID string) (res LedgerView) {
	defer func() { res = lists(res) }()
	s, err := i.eng.Session(snapshotID)
	if err != nil {
		return LedgerView{Failure: Fail(err)}
	}
	prefs := i.eng.Config.Preferences()
	return LedgerView{
		Entries: s.Ledger(), Summary: s.LedgerSummary(),
		Note:          "This ledger records where each secret lives and what kind it is. It never holds a value. Revealing one is a deliberate, audited action.",
		RevealAllowed: prefs.AllowSecretReveal != nil && *prefs.AllowSecretReveal,
	}
}

// RevealResult is one disclosed value.
type RevealResult struct {
	Value string `json:"value"`
	// Note says what was recorded, and — for an unencrypted snapshot — that the
	// value was already in the clear inside it.
	Note    string   `json:"note"`
	Failure *Failure `json:"failure,omitempty"`
}

// Reveal discloses one secret, once, under audit.
func (i *InspectController) Reveal(snapshotID, location, reason string) RevealResult {
	s, err := i.eng.Session(snapshotID)
	if err != nil {
		return RevealResult{Failure: Fail(err)}
	}
	prefs := i.eng.Config.Preferences()
	allowed := prefs.AllowSecretReveal != nil && *prefs.AllowSecretReveal

	value, err := s.Reveal(context.Background(), inspect.RevealRequest{
		Location: location, Reason: reason,
	}, i.eng.Audit, allowed)
	if err != nil {
		return RevealResult{Failure: Fail(err)}
	}
	return RevealResult{Value: value, Note: s.RevealNote()}
}

// Verify recomputes the integrity tree without touching any environment.
func (i *InspectController) Verify(snapshotID string) (res inspect.VerifyReport, fail *Failure) {
	defer func() { res = lists(res) }()
	s, err := i.eng.Session(snapshotID)
	if err != nil {
		return inspect.VerifyReport{}, Fail(err)
	}
	report := s.VerifyReport()
	_ = i.eng.Audit.Record(obs.AuditEntry{
		Action: obs.ActionVerify, Outcome: verifyOutcome(report.OK),
		Realm: s.Realm, SnapshotID: s.ID, Storage: s.Storage,
		Detail: report.Message,
	})
	return report, nil
}

func verifyOutcome(ok bool) string {
	if ok {
		return "verified"
	}
	return "failed verification"
}

// CloseResult confirms what closing removed.
type CloseResult struct {
	Confirmed string   `json:"confirmed"`
	Failure   *Failure `json:"failure,omitempty"`
}

// Close destroys the index and shreds the decrypted working files.
//
// Closing is a visible action rather than an implicit consequence of navigating
// away, because "is that copy of my user directory gone?" deserves a definite
// answer.
func (i *InspectController) Close(snapshotID string) CloseResult {
	s := i.eng.dropSession(snapshotID)
	if s == nil {
		return CloseResult{Confirmed: "That snapshot was not open."}
	}
	if err := s.Close(); err != nil {
		return CloseResult{Failure: Fail(err)}
	}
	return CloseResult{
		Confirmed: "The inspection index and every decrypted working file for this snapshot have been removed from this machine. The snapshot itself is untouched in storage.",
	}
}
