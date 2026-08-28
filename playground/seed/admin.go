// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// applyRealm creates the realm on a running Keycloak through the admin API.
//
// POST /admin/realms takes a whole realm representation — the same document
// `kc.sh import` reads — so the generator has one output and two ways to
// deliver it. Which matters for the playground: the file is what an operator
// diffs, and the POST is what a script uses.
func applyRealm(base, user, pass string, realm map[string]any) error {
	base = strings.TrimRight(base, "/")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	token, err := adminToken(ctx, base, user, pass)
	if err != nil {
		return err
	}

	name, _ := realm["realm"].(string)
	body, err := json.Marshal(realm)
	if err != nil {
		return err
	}

	// Deleting first makes the command repeatable, which is what a playground
	// needs: seeding twice should produce the realm the second run describes,
	// not a merge of two runs nobody can reason about.
	if err := deleteRealm(ctx, base, token, name); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/admin/realms", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("posting the realm to %s: %w", base, err)
	}
	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode/100 != 2 {
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("%s refused the realm: %s\n%s", base, res.Status, strings.TrimSpace(string(detail)))
	}

	users, _ := realm["users"].([]any)
	fmt.Fprintf(os.Stderr, "created realm %q on %s with %d users\n", name, base, len(users))
	return nil
}

func adminToken(ctx context.Context, base, user, pass string) (string, error) {
	form := url.Values{
		"grant_type": {"password"},
		"client_id":  {"admin-cli"},
		"username":   {user},
		"password":   {pass},
	}
	endpoint := base + "/realms/master/protocol/openid-connect/token"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("reaching %s: %w — is it started, and is the port right?", base, err)
	}
	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return "", fmt.Errorf("%s would not issue an admin token: %s %s",
			base, res.Status, strings.TrimSpace(string(detail)))
	}

	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("%s returned no access token", base)
	}
	return out.AccessToken, nil
}

// deleteRealm removes a realm if it is there, and says nothing if it is not.
func deleteRealm(ctx context.Context, base, token, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		base+"/admin/realms/"+url.PathEscape(name), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close() //nolint:errcheck

	switch {
	case res.StatusCode == http.StatusNotFound:
		return nil
	case res.StatusCode/100 == 2:
		fmt.Fprintf(os.Stderr, "replaced the existing realm %q\n", name)
		return nil
	default:
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return fmt.Errorf("could not replace the existing realm %q: %s %s",
			name, res.Status, strings.TrimSpace(string(detail)))
	}
}

// ── seed all ────────────────────────────────────────────────────────────────

// allCommand writes both halves into a directory, so the two files that have to
// agree about a realm are produced by one command with one seed.
func allCommand(args []string) error {
	fs := flag.NewFlagSet("all", flag.ExitOnError)
	var s shape
	s.flags(fs)
	dir := fs.String("dir", "playground/.seed", "directory to write into")
	ldapHost := fs.String("ldap-host", "ldap-a", "host the realm's federation provider connects to")
	ldapPort := fs.Int("ldap-port", 1389, "port for the same")
	apply := fs.String("apply", "", "also POST the realm to this Keycloak")
	admin := fs.String("admin", "admin:admin", "admin credentials for --apply, as user:password")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s.resolve()

	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return err
	}

	realmPath := fmt.Sprintf("%s/%s-realm.json", *dir, s.realm)
	ldifPath := fmt.Sprintf("%s/%s.ldif", *dir, s.realm)

	if err := realmCommand([]string{
		"-realm", s.realm, "-base-dn", s.baseDN,
		"-seed", fmt.Sprint(s.seed), "-users", fmt.Sprint(s.users),
		"-clients", fmt.Sprint(s.clients), "-groups", fmt.Sprint(s.groups),
		"-roles", fmt.Sprint(s.roles),
		"-ldap-host", *ldapHost, "-ldap-port", fmt.Sprint(*ldapPort),
		"-out", realmPath,
	}); err != nil {
		return err
	}

	if err := ldifCommand([]string{
		"-realm", s.realm, "-base-dn", s.baseDN,
		"-seed", fmt.Sprint(s.seed), "-ldap-users", fmt.Sprint(s.ldapUsers),
		"-out", ldifPath,
	}); err != nil {
		return err
	}

	if *apply != "" {
		user, pass, _ := strings.Cut(*admin, ":")
		realm := buildRealm(s, *ldapHost, *ldapPort)
		if err := applyRealm(*apply, user, pass, realm); err != nil {
			return err
		}
	}
	return nil
}
