// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Package index is the session-scoped projection store behind snapshot
// browsing.
//
// It is a disposable query accelerator and never a store of record. One file
// per open snapshot, built when the snapshot is opened and securely deleted
// when it is closed — because an index is a searchable copy of an
// organisation's entire user directory, and leaving one on a workstation
// between sessions is a worse liability than a rebuild is an inconvenience.
package index

import (
	"fmt"
	"sort"
	"strings"
)

// The credential boundary is enforced by the schema itself.
//
// The index records that a user has a password and which algorithm hashed it,
// how many OTP enrolments and passkeys they hold, and whether they have
// recovery codes. There is no column for a hash value, an OTP seed, passkey
// material or any secret — so an operator can answer "will this user's 2FA
// survive the move?" without the index becoming a second copy of the crown
// jewels.
//
// User attributes are deliberately absent too. They can hold configuration
// secrets, and single-user detail is re-read from the decrypted bundle on
// demand rather than projected here.
const schema = `
PRAGMA journal_mode = MEMORY;
PRAGMA synchronous = OFF;
PRAGMA secure_delete = ON;
PRAGMA temp_store = MEMORY;

CREATE TABLE users (
  id                  TEXT PRIMARY KEY,
  username            TEXT NOT NULL,
  username_lc         TEXT NOT NULL,
  email               TEXT,
  email_lc            TEXT,
  first_name          TEXT,
  last_name           TEXT,
  name_lc             TEXT,
  enabled             INTEGER NOT NULL,
  email_verified      INTEGER NOT NULL,
  origin              TEXT NOT NULL,
  has_password        INTEGER NOT NULL,
  password_algo       TEXT,
  password_iterations INTEGER,
  otp_count           INTEGER NOT NULL,
  webauthn_count      INTEGER NOT NULL,
  recovery_codes      INTEGER NOT NULL,
  second_factor       TEXT NOT NULL,
  required_actions    TEXT,
  created_at          INTEGER,
  service_account     TEXT,
  source_file         TEXT NOT NULL,
  source_index        INTEGER NOT NULL
);

CREATE INDEX users_username_lc ON users(username_lc);
CREATE INDEX users_email_lc ON users(email_lc);
CREATE INDEX users_enabled ON users(enabled);
CREATE INDEX users_origin ON users(origin);
CREATE INDEX users_second_factor ON users(second_factor);

CREATE TABLE user_realm_roles (
  user_id TEXT NOT NULL,
  role    TEXT NOT NULL
);
CREATE INDEX user_realm_roles_role ON user_realm_roles(role);
CREATE INDEX user_realm_roles_user ON user_realm_roles(user_id);

CREATE TABLE user_client_roles (
  user_id TEXT NOT NULL,
  client  TEXT NOT NULL,
  role    TEXT NOT NULL
);
CREATE INDEX user_client_roles_role ON user_client_roles(client, role);
CREATE INDEX user_client_roles_user ON user_client_roles(user_id);

CREATE TABLE user_groups (
  user_id TEXT NOT NULL,
  path    TEXT NOT NULL
);
CREATE INDEX user_groups_path ON user_groups(path);
CREATE INDEX user_groups_user ON user_groups(user_id);

CREATE TABLE user_required_actions (
  user_id TEXT NOT NULL,
  action  TEXT NOT NULL
);
CREATE INDEX user_required_actions_action ON user_required_actions(action);

CREATE TABLE user_federated_identities (
  user_id  TEXT NOT NULL,
  provider TEXT NOT NULL,
  username TEXT
);
CREATE INDEX user_federated_identities_user ON user_federated_identities(user_id);
`

// allowedColumns is the complete set of columns the index may ever hold.
//
// TestIndexSchemaHasNoSecretColumns fails on anything not listed here, so
// adding a column to help a feature along requires deliberately editing a test
// that says not to.
var allowedColumns = map[string][]string{
	"users": {
		"id", "username", "username_lc", "email", "email_lc",
		"first_name", "last_name", "name_lc",
		"enabled", "email_verified", "origin",
		"has_password", "password_algo", "password_iterations",
		"otp_count", "webauthn_count", "recovery_codes", "second_factor",
		"required_actions", "created_at", "service_account",
		"source_file", "source_index",
	},
	"user_realm_roles":          {"user_id", "role"},
	"user_client_roles":         {"user_id", "client", "role"},
	"user_groups":               {"user_id", "path"},
	"user_required_actions":     {"user_id", "action"},
	"user_federated_identities": {"user_id", "provider", "username"},
}

// AllowedColumns exposes the allowlist for the schema assertion.
func AllowedColumns() map[string][]string {
	out := make(map[string][]string, len(allowedColumns))
	for table, cols := range allowedColumns {
		c := append([]string(nil), cols...)
		sort.Strings(c)
		out[table] = c
	}
	return out
}

// forbiddenColumnFragments are the names that must never appear as a column,
// whatever else changes.
var forbiddenColumnFragments = []string{
	"secret", "password_hash", "hash_value", "salt", "seed",
	"credential_data", "secret_data", "private", "token", "passkey_material",
	"attribute", "attr_value",
}

// CheckColumn reports whether a column name is safe to add.
func CheckColumn(table, column string) error {
	lc := strings.ToLower(column)
	for _, bad := range forbiddenColumnFragments {
		if strings.Contains(lc, bad) {
			return fmt.Errorf("%s.%s looks like it would hold secret material; the index records presence and metadata only", table, column)
		}
	}
	allowed, ok := allowedColumns[table]
	if !ok {
		return fmt.Errorf("%s is not a table the index defines", table)
	}
	for _, c := range allowed {
		if c == column {
			return nil
		}
	}
	return fmt.Errorf("%s.%s is not on the index's column allowlist", table, column)
}
