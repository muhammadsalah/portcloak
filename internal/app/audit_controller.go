// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"time"

	"portcloak/internal/engine/obs"
)

// AuditController is the audit log screen, and only that.
//
// It used to carry the maintenance panels too — the configuration file, the
// orphan sweep, the working-data purge — which made "what has PortCloak done"
// and "what is PortCloak holding" one screen with two jobs. They are on
// Settings now. What is left here is a record: read, filtered, never acted on.
type AuditController struct{ eng *Engine }

// NewAuditController binds the audit log screen.
func NewAuditController(eng *Engine) *AuditController {
	return &AuditController{eng: eng}
}

// ServiceName is the name internal/desktop logs this service under. It is
// not the address a bound method is called by — see the comment on
// controllers there, which is where reading it as one caused real damage.
func (a *AuditController) ServiceName() string { return "AuditController" }

// AuditView is the audit log.
type AuditView struct {
	Entries []obs.AuditEntry `json:"entries"`
	Path    string           `json:"path"`
	// Note states the thing that surprises people: there is no user recorded,
	// because there is no user.
	Note    string   `json:"note"`
	Failure *Failure `json:"failure,omitempty"`
}

// Audit returns the audit log, newest first.
func (a *AuditController) Audit(action string, sinceDays int) (res AuditView) {
	defer func() { res = lists(res) }()
	filter := obs.AuditFilter{Action: obs.Action(action)}
	if sinceDays > 0 {
		filter.Since = time.Now().AddDate(0, 0, -sinceDays)
	}
	entries, err := a.eng.Audit.Read(filter)
	if err != nil {
		return AuditView{Failure: Fail(err)}
	}
	return AuditView{
		Entries: entries, Path: a.eng.Audit.Path(),
		Note: "No user is recorded, because there is none. PortCloak is a single-user local tool. Each entry says what happened and when.",
	}
}
