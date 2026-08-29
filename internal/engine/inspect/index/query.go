// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// UserRow is one line of the users table.
//
// Every credential field is presence or metadata. There is no field a hash, an
// OTP seed or passkey material could occupy.
type UserRow struct {
	ID                string `json:"id"`
	Username          string `json:"username"`
	Email             string `json:"email,omitempty"`
	FirstName         string `json:"firstName,omitempty"`
	LastName          string `json:"lastName,omitempty"`
	Origin            string `json:"origin"`
	PasswordAlgorithm string `json:"passwordAlgorithm,omitempty"`
	SecondFactor      string `json:"secondFactor"`
	ServiceAccount    string `json:"serviceAccount,omitempty"`
	// SourceFile and SourceIndex say where the full record lives, so single-user
	// detail is re-read from the decrypted bundle rather than projected here.
	SourceFile         string   `json:"sourceFile"`
	RequiredActions    []string `json:"requiredActions,omitempty"`
	Groups             []string `json:"groups,omitempty"`
	PasswordIterations int      `json:"passwordIterations,omitempty"`
	OTPCount           int      `json:"otpCount"`
	WebAuthnCount      int      `json:"webauthnCount"`
	SourceIndex        int      `json:"sourceIndex"`
	Enabled            bool     `json:"enabled"`
	EmailVerified      bool     `json:"emailVerified"`
	HasPassword        bool     `json:"hasPassword"`
	RecoveryCodes      bool     `json:"recoveryCodes"`
}

// UserFilter narrows a listing. Every field intersects with the others, and
// with the free-text query.
type UserFilter struct {
	Query string
	// Enabled is nil for "either".
	Enabled *bool
	Origin  string
	// SecondFactor is none, otp, passkey or both.
	SecondFactor   string
	RealmRole      string
	ClientRole     string
	Client         string
	Group          string
	RequiredAction string
}

// UserPage is one page of results.
type UserPage struct {
	Rows   []UserRow `json:"rows"`
	Total  int       `json:"total"`
	Offset int       `json:"offset"`
	Limit  int       `json:"limit"`
}

// SortBy names an ordering.
type SortBy string

const (
	SortUsername SortBy = "username"
	SortEmail    SortBy = "email"
	SortEnabled  SortBy = "enabled"
	SortCreated  SortBy = "created"
)

// Users returns a page of matching users.
//
// Search is served from the index rather than by rescanning the bundle, which
// is the whole reason the index exists.
func (i *Index) Users(ctx context.Context, f UserFilter, sortBy SortBy, desc bool, offset, limit int) (UserPage, error) {
	where, args := f.clause()

	var total int
	if err := i.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users u "+where, args...).Scan(&total); err != nil {
		return UserPage{}, fmt.Errorf("counting users: %w", err)
	}

	if limit <= 0 {
		limit = 50
	}
	order := "u.username_lc"
	switch sortBy {
	case SortEmail:
		order = "u.email_lc"
	case SortEnabled:
		order = "u.enabled"
	case SortCreated:
		order = "u.created_at"
	}
	if desc {
		order += " DESC"
	}

	rows, err := i.db.QueryContext(ctx, `
		SELECT u.id, u.username, COALESCE(u.email,''), COALESCE(u.first_name,''), COALESCE(u.last_name,''),
		       u.enabled, u.email_verified, u.origin, u.has_password, COALESCE(u.password_algo,''),
		       COALESCE(u.password_iterations,0), u.otp_count, u.webauthn_count, u.recovery_codes,
		       u.second_factor, COALESCE(u.required_actions,''), COALESCE(u.service_account,''),
		       u.source_file, u.source_index
		FROM users u `+where+`
		ORDER BY `+order+`
		LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return UserPage{}, fmt.Errorf("listing users: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	page := UserPage{Total: total, Offset: offset, Limit: limit}
	var ids []string
	for rows.Next() {
		var r UserRow
		var actions string
		if err := rows.Scan(&r.ID, &r.Username, &r.Email, &r.FirstName, &r.LastName,
			&r.Enabled, &r.EmailVerified, &r.Origin, &r.HasPassword, &r.PasswordAlgorithm,
			&r.PasswordIterations, &r.OTPCount, &r.WebAuthnCount, &r.RecoveryCodes,
			&r.SecondFactor, &actions, &r.ServiceAccount, &r.SourceFile, &r.SourceIndex); err != nil {
			return UserPage{}, err
		}
		if actions != "" {
			r.RequiredActions = strings.Split(actions, ",")
		}
		page.Rows = append(page.Rows, r)
		ids = append(ids, r.ID)
	}
	if err := rows.Err(); err != nil {
		return UserPage{}, err
	}

	// Group membership is fetched for the page rather than per row, so a page
	// of fifty costs one extra query instead of fifty.
	groups, err := i.groupsFor(ctx, ids)
	if err != nil {
		return UserPage{}, err
	}
	for idx := range page.Rows {
		page.Rows[idx].Groups = groups[page.Rows[idx].ID]
	}
	return page, nil
}

func (i *Index) groupsFor(ctx context.Context, ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(ids) == 0 {
		return out, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := i.db.QueryContext(ctx,
		"SELECT user_id, path FROM user_groups WHERE user_id IN ("+placeholders+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		out[id] = append(out[id], path)
	}
	return out, rows.Err()
}

// clause renders the filter as SQL. Every value is a bound parameter: a realm's
// group paths and role names come from someone else's Keycloak and are never
// interpolated into a statement.
func (f UserFilter) clause() (string, []any) {
	var conds []string
	var args []any

	if q := strings.ToLower(strings.TrimSpace(f.Query)); q != "" {
		like := "%" + q + "%"
		conds = append(conds, `(u.username_lc LIKE ? OR u.email_lc LIKE ? OR u.name_lc LIKE ? OR u.id = ?)`)
		args = append(args, like, like, like, strings.TrimSpace(f.Query))
	}
	if f.Enabled != nil {
		conds = append(conds, "u.enabled = ?")
		args = append(args, boolInt(*f.Enabled))
	}
	if f.Origin != "" {
		conds = append(conds, "u.origin = ?")
		args = append(args, f.Origin)
	}
	if f.SecondFactor != "" {
		conds = append(conds, "u.second_factor = ?")
		args = append(args, f.SecondFactor)
	}
	if f.RealmRole != "" {
		conds = append(conds, "EXISTS (SELECT 1 FROM user_realm_roles r WHERE r.user_id = u.id AND r.role = ?)")
		args = append(args, f.RealmRole)
	}
	if f.ClientRole != "" {
		if f.Client != "" {
			conds = append(conds, "EXISTS (SELECT 1 FROM user_client_roles r WHERE r.user_id = u.id AND r.client = ? AND r.role = ?)")
			args = append(args, f.Client, f.ClientRole)
		} else {
			conds = append(conds, "EXISTS (SELECT 1 FROM user_client_roles r WHERE r.user_id = u.id AND r.role = ?)")
			args = append(args, f.ClientRole)
		}
	}
	if f.Group != "" {
		conds = append(conds, "EXISTS (SELECT 1 FROM user_groups g WHERE g.user_id = u.id AND g.path = ?)")
		args = append(args, f.Group)
	}
	if f.RequiredAction != "" {
		conds = append(conds, "EXISTS (SELECT 1 FROM user_required_actions a WHERE a.user_id = u.id AND a.action = ?)")
		args = append(args, f.RequiredAction)
	}

	if len(conds) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

// FacetValue is one selectable filter value and how many users it matches.
type FacetValue struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

// Facets is the whole facet panel.
type Facets struct {
	Status          []FacetValue `json:"status"`
	Origin          []FacetValue `json:"origin"`
	SecondFactor    []FacetValue `json:"secondFactor"`
	RealmRoles      []FacetValue `json:"realmRoles"`
	ClientRoles     []FacetValue `json:"clientRoles"`
	Groups          []FacetValue `json:"groups"`
	RequiredActions []FacetValue `json:"requiredActions"`
}

// Facets computes the counts shown beside each filter.
//
// They are computed against the current filter minus that facet's own
// dimension, so selecting "enabled" does not zero out the disabled count and
// make the filter impossible to undo.
func (i *Index) Facets(ctx context.Context, f UserFilter) (Facets, error) {
	var out Facets

	statusFilter := f
	statusFilter.Enabled = nil
	enabled, err := i.countBy(ctx, statusFilter, "u.enabled")
	if err != nil {
		return out, err
	}
	for _, v := range enabled {
		label := "Enabled"
		value := "true"
		if v.Value == "0" {
			label, value = "Disabled", "false"
		}
		out.Status = append(out.Status, FacetValue{Value: value, Label: label, Count: v.Count})
	}
	sort.Slice(out.Status, func(a, b int) bool { return out.Status[a].Value > out.Status[b].Value })

	originFilter := f
	originFilter.Origin = ""
	if out.Origin, err = i.countBy(ctx, originFilter, "u.origin"); err != nil {
		return out, err
	}

	factorFilter := f
	factorFilter.SecondFactor = ""
	if out.SecondFactor, err = i.countBy(ctx, factorFilter, "u.second_factor"); err != nil {
		return out, err
	}

	realmRoleFilter := f
	realmRoleFilter.RealmRole = ""
	if out.RealmRoles, err = i.countByJoin(ctx, realmRoleFilter, "user_realm_roles"); err != nil {
		return out, err
	}

	clientRoleFilter := f
	clientRoleFilter.ClientRole, clientRoleFilter.Client = "", ""
	if out.ClientRoles, err = i.countByJoin(ctx, clientRoleFilter, "user_client_roles"); err != nil {
		return out, err
	}

	groupFilter := f
	groupFilter.Group = ""
	if out.Groups, err = i.countByJoin(ctx, groupFilter, "user_groups"); err != nil {
		return out, err
	}

	actionFilter := f
	actionFilter.RequiredAction = ""
	if out.RequiredActions, err = i.countByJoin(ctx, actionFilter, "user_required_actions"); err != nil {
		return out, err
	}

	return out, nil
}

func (i *Index) countBy(ctx context.Context, f UserFilter, column string) ([]FacetValue, error) {
	where, args := f.clause()
	rows, err := i.db.QueryContext(ctx,
		"SELECT "+column+" AS v, COUNT(*) FROM users u "+where+" GROUP BY v ORDER BY COUNT(*) DESC, v", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var out []FacetValue
	for rows.Next() {
		var v FacetValue
		if err := rows.Scan(&v.Value, &v.Count); err != nil {
			return nil, err
		}
		v.Label = v.Value
		out = append(out, v)
	}
	return out, rows.Err()
}

func (i *Index) countByJoin(ctx context.Context, f UserFilter, table string) ([]FacetValue, error) {
	where, args := f.clause()
	// The projection is chosen from this package's own constants rather than
	// interpolated, so nothing an operator's realm contains can reach the SQL.
	join := "JOIN " + table + " j ON j.user_id = u.id"
	var query string
	switch table {
	case "user_groups":
		query = "SELECT j.path AS v, COUNT(DISTINCT u.id) FROM users u " + join + " " + where +
			" GROUP BY v ORDER BY COUNT(DISTINCT u.id) DESC, v LIMIT 200"
	case "user_required_actions":
		query = "SELECT j.action AS v, COUNT(DISTINCT u.id) FROM users u " + join + " " + where +
			" GROUP BY v ORDER BY COUNT(DISTINCT u.id) DESC, v LIMIT 200"
	case "user_client_roles":
		query = "SELECT j.client || ' · ' || j.role AS v, COUNT(DISTINCT u.id) FROM users u " + join + " " + where +
			" GROUP BY v ORDER BY COUNT(DISTINCT u.id) DESC, v LIMIT 200"
	case "user_realm_roles":
		query = "SELECT j.role AS v, COUNT(DISTINCT u.id) FROM users u " + join + " " + where +
			" GROUP BY v ORDER BY COUNT(DISTINCT u.id) DESC, v LIMIT 200"
	default:
		return nil, fmt.Errorf("%s is not a table the facet builder knows", table)
	}

	rows, err := i.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var out []FacetValue
	for rows.Next() {
		var v FacetValue
		if err := rows.Scan(&v.Value, &v.Count); err != nil {
			return nil, err
		}
		v.Label = v.Value
		out = append(out, v)
	}
	return out, rows.Err()
}

// User returns one row, which the detail pane uses to locate the full record.
//
// It uses QueryRow rather than Query deliberately: the index runs on a single
// connection, so a result set left open while another query is issued would
// deadlock against itself.
func (i *Index) User(ctx context.Context, id string) (UserRow, error) {
	var r UserRow
	var actions string
	err := i.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, COALESCE(u.email,''), COALESCE(u.first_name,''), COALESCE(u.last_name,''),
		       u.enabled, u.email_verified, u.origin, u.has_password, COALESCE(u.password_algo,''),
		       COALESCE(u.password_iterations,0), u.otp_count, u.webauthn_count, u.recovery_codes,
		       u.second_factor, COALESCE(u.required_actions,''), COALESCE(u.service_account,''),
		       u.source_file, u.source_index
		FROM users u WHERE u.id = ?`, id).Scan(
		&r.ID, &r.Username, &r.Email, &r.FirstName, &r.LastName,
		&r.Enabled, &r.EmailVerified, &r.Origin, &r.HasPassword, &r.PasswordAlgorithm,
		&r.PasswordIterations, &r.OTPCount, &r.WebAuthnCount, &r.RecoveryCodes,
		&r.SecondFactor, &actions, &r.ServiceAccount, &r.SourceFile, &r.SourceIndex)
	if errors.Is(err, sql.ErrNoRows) {
		return UserRow{}, fmt.Errorf("this snapshot has no user with the id %q", id)
	}
	if err != nil {
		return UserRow{}, err
	}
	if actions != "" {
		r.RequiredActions = strings.Split(actions, ",")
	}

	groups, err := i.groupsFor(ctx, []string{r.ID})
	if err != nil {
		return UserRow{}, err
	}
	r.Groups = groups[r.ID]
	return r, nil
}

// RolesFor returns a user's realm and client role mappings.
func (i *Index) RolesFor(ctx context.Context, id string) (realmRoles []string, clientRoles map[string][]string, err error) {
	clientRoles = map[string][]string{}

	rows, err := i.db.QueryContext(ctx, "SELECT role FROM user_realm_roles WHERE user_id = ? ORDER BY role", id)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			_ = rows.Close()
			return nil, nil, err
		}
		realmRoles = append(realmRoles, r)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, nil, err
	}
	_ = rows.Close()

	crows, err := i.db.QueryContext(ctx, "SELECT client, role FROM user_client_roles WHERE user_id = ? ORDER BY client, role", id)
	if err != nil {
		return nil, nil, err
	}
	defer crows.Close() //nolint:errcheck
	for crows.Next() {
		var client, role string
		if err := crows.Scan(&client, &role); err != nil {
			return nil, nil, err
		}
		clientRoles[client] = append(clientRoles[client], role)
	}
	return realmRoles, clientRoles, crows.Err()
}

// FederatedIdentitiesFor returns a user's social account links.
func (i *Index) FederatedIdentitiesFor(ctx context.Context, id string) ([]FacetValue, error) {
	rows, err := i.db.QueryContext(ctx,
		"SELECT provider, COALESCE(username,'') FROM user_federated_identities WHERE user_id = ? ORDER BY provider", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var out []FacetValue
	for rows.Next() {
		var provider, username string
		if err := rows.Scan(&provider, &username); err != nil {
			return nil, err
		}
		out = append(out, FacetValue{Value: provider, Label: username})
	}
	return out, rows.Err()
}

// AllUserIDs streams every matching user id, for an export that must not hold
// the whole result set in memory.
func (i *Index) AllUserIDs(ctx context.Context, f UserFilter, fn func(UserRow) error) error {
	const batch = 500
	for offset := 0; ; offset += batch {
		page, err := i.Users(ctx, f, SortUsername, false, offset, batch)
		if err != nil {
			return err
		}
		for _, r := range page.Rows {
			if err := fn(r); err != nil {
				return err
			}
		}
		if len(page.Rows) < batch {
			return nil
		}
	}
}

// Columns lists the columns a table actually has, for the schema assertion.
func (i *Index) Columns(ctx context.Context, table string) ([]string, error) {
	rows, err := i.db.QueryContext(ctx, "SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out, rows.Err()
}

// Tables lists the index's tables, for the schema assertion.
func (i *Index) Tables(ctx context.Context) ([]string, error) {
	rows, err := i.db.QueryContext(ctx,
		"SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
