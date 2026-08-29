// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	mathrand "math/rand"
	"os"
	"strings"
)

// buildRealm produces a realm representation in the shape `kc.sh import` and the
// admin API both read.
//
// Every category PortCloak's manifest enumerates is represented, because a
// fixture that omits one is a fixture that cannot fail the check for it: clients
// with secrets, client scopes, protocol mappers, realm and client roles,
// composites, nested groups with attributes and role mappings, an identity
// provider, a user federation provider, required actions, and users carrying
// every credential type.
func buildRealm(s shape, ldapHost string, ldapPort int) map[string]any {
	r := mathrand.New(mathrand.NewSource(s.seed))

	groups, groupPaths := buildGroups(r, s.groups)
	roles, roleNames := buildRealmRoles(r, s.roles)
	clients, clientScopes, clientRoles := buildClients(r, s.clients)

	realm := map[string]any{
		"realm":                                  s.realm,
		"displayName":                            strings.ToUpper(s.realm[:1]) + s.realm[1:] + " (playground)",
		"enabled":                                true,
		"sslRequired":                            "none",
		"registrationAllowed":                    true,
		"loginWithEmailAllowed":                  true,
		"duplicateEmailsAllowed":                 false,
		"resetPasswordAllowed":                   true,
		"editUsernameAllowed":                    false,
		"bruteForceProtected":                    true,
		"permanentLockout":                       false,
		"maxFailureWaitSeconds":                  900,
		"failureFactor":                          30,
		"otpPolicyType":                          "totp",
		"otpPolicyAlgorithm":                     "HmacSHA1",
		"otpPolicyDigits":                        6,
		"otpPolicyPeriod":                        30,
		"webAuthnPolicyPasswordlessRpEntityName": "PortCloak playground",
		"webAuthnPolicyPasswordlessSignatureAlgorithms":         []string{"ES256", "RS256"},
		"webAuthnPolicyPasswordlessRequireResidentKey":          "Yes",
		"webAuthnPolicyPasswordlessUserVerificationRequirement": "required",

		// Token lifetimes are off the defaults so a restored realm's settings
		// can be compared against the source for something other than zero.
		"accessTokenLifespan":       360,
		"ssoSessionIdleTimeout":     2100,
		"ssoSessionMaxLifespan":     39600,
		"offlineSessionIdleTimeout": 2592000,
		"passwordPolicy":            "length(10) and notUsername(undefined) and passwordHistory(3)",

		"attributes": map[string]string{
			"frontendUrl":              "",
			"playground.generated":     "true",
			"playground.seed":          fmt.Sprintf("%d", s.seed),
			"playground.awkward.value": "a value with a \"quote\", a comma, and a ünïcode character",
		},

		"requiredActions": requiredActions(),
		"groups":          groups,
		"roles": map[string]any{
			"realm":  roles,
			"client": clientRoles,
		},
		"clientScopes": clientScopes,
		"clients":      clients,
		"identityProviders": []any{
			identityProvider(),
		},
		"components": map[string]any{
			"org.keycloak.storage.UserStorageProvider": []any{
				ldapProvider(s, ldapHost, ldapPort),
			},
		},
	}

	realm["users"] = buildUsers(r, s, groupPaths, roleNames, clients)
	return realm
}

// buildGroups makes a forest rather than a list: nesting is where group path
// handling goes wrong, and a flat set of eight would never show it.
func buildGroups(r *mathrand.Rand, n int) ([]any, []string) {
	var groups []any
	var paths []string

	for i := range n {
		name := fmt.Sprintf("%s-%d", strings.ToLower(departments[i%len(departments)]), i)
		path := "/" + name
		paths = append(paths, path)

		var children []any
		for j := range 1 + r.Intn(3) {
			childName := fmt.Sprintf("%s-team-%d", name, j)
			childPath := path + "/" + childName
			paths = append(paths, childPath)

			var grandchildren []any
			if r.Intn(3) == 0 {
				gcName := childName + "-oncall"
				paths = append(paths, childPath+"/"+gcName)
				grandchildren = append(grandchildren, map[string]any{
					"name":       gcName,
					"path":       childPath + "/" + gcName,
					"attributes": map[string][]string{"rota": {"primary", "secondary"}},
					"subGroups":  []any{},
				})
			}

			children = append(children, map[string]any{
				"name": childName,
				"path": childPath,
				"attributes": map[string][]string{
					"cost-centre": {fmt.Sprintf("CC-%03d", r.Intn(999))},
				},
				"subGroups": grandchildren,
			})
		}

		groups = append(groups, map[string]any{
			"name": name,
			"path": path,
			"attributes": map[string][]string{
				"department": {departments[i%len(departments)]},
				"region":     {locations[i%len(locations)]},
			},
			"subGroups": children,
		})
	}
	return groups, paths
}

// buildRealmRoles makes some plain roles and some composites, because a
// composite is a role that points at other roles and the pointing is the part
// that can be lost in a move.
func buildRealmRoles(r *mathrand.Rand, n int) ([]any, []string) {
	base := []string{
		"realm-reader", "realm-writer", "billing-viewer", "billing-admin",
		"support-agent", "support-lead", "auditor", "release-manager",
		"data-scientist", "on-call", "contractor", "intern", "vendor",
		"security-reviewer", "platform-admin",
	}
	if n > len(base) {
		for i := len(base); i < n; i++ {
			base = append(base, fmt.Sprintf("generated-role-%d", i))
		}
	}
	base = base[:n]

	var roles []any
	for i, name := range base {
		role := map[string]any{
			"name":        name,
			"description": "Playground role " + name,
			"composite":   false,
			"attributes":  map[string][]string{"tier": {fmt.Sprintf("%d", 1+i%3)}},
		}
		// Every fourth role is composite, over roles that already exist.
		if i > 0 && i%4 == 0 {
			members := pickSome(r, base[:i], 1, 3)
			role["composite"] = true
			role["composites"] = map[string]any{"realm": members}
		}
		roles = append(roles, role)
	}
	return roles, base
}

// buildClients covers the four client shapes that behave differently on a
// restore: a confidential client with a secret, a public SPA, a bearer-only
// service, and a service-account client with its own roles.
// The client roles come back separately rather than riding inside each client.
// ClientRepresentation has no roles field: Keycloak carries client roles at the
// realm level, under roles.client keyed by clientId. A roles array inside a
// client is not ignored, it fails the whole import — the realm comes back as
// 400 "unable to read contents from stream", which names the stream rather than
// the field and sends you looking at the request instead of at what is in it.
func buildClients(r *mathrand.Rand, n int) ([]any, []any, map[string]any) {
	scopes := []any{
		map[string]any{
			"name":        "playground-profile",
			"description": "Extra claims the playground's mappers add",
			"protocol":    "openid-connect",
			"attributes": map[string]string{
				"include.in.token.scope":    "true",
				"display.on.consent.screen": "true",
			},
			"protocolMappers": []any{
				mapper("department", "department", "String"),
				mapper("employee-number", "employeeNumber", "String"),
				mapper("location", "location", "String"),
			},
		},
	}

	var clients []any
	clientRoles := map[string]any{}
	for i := range n {
		id := fmt.Sprintf("playground-client-%02d", i)
		kind := i % 4

		c := map[string]any{
			"clientId":             id,
			"name":                 fmt.Sprintf("Playground client %02d", i),
			"description":          "Generated by playground/seed",
			"enabled":              true,
			"protocol":             "openid-connect",
			"defaultClientScopes":  []string{"web-origins", "profile", "roles", "email", "playground-profile"},
			"optionalClientScopes": []string{"address", "phone", "offline_access"},
			"attributes": map[string]string{
				"post.logout.redirect.uris": "+",
				"playground.kind":           fmt.Sprintf("%d", kind),
			},
			"protocolMappers": []any{
				mapper(fmt.Sprintf("%s-tenant", id), "tenant", "String"),
			},
		}

		switch kind {
		case 0: // confidential, with a secret worth carrying
			c["publicClient"] = false
			c["secret"] = fmt.Sprintf("playground-secret-%02d-%d", i, r.Intn(1_000_000))
			c["standardFlowEnabled"] = true
			c["redirectUris"] = []string{fmt.Sprintf("https://app-%02d.example.com/callback", i)}
			c["webOrigins"] = []string{fmt.Sprintf("https://app-%02d.example.com", i)}
		case 1: // public SPA, no secret at all
			c["publicClient"] = true
			c["standardFlowEnabled"] = true
			c["redirectUris"] = []string{fmt.Sprintf("https://spa-%02d.example.com/*", i)}
			c["webOrigins"] = []string{"+"}
			c["attributes"].(map[string]string)["pkce.code.challenge.method"] = "S256"
		case 2: // bearer-only: it validates tokens and issues none
			c["publicClient"] = false
			c["bearerOnly"] = true
			c["secret"] = fmt.Sprintf("playground-bearer-%02d", i)
			c["standardFlowEnabled"] = false
		case 3: // service account, with client roles of its own
			c["publicClient"] = false
			c["secret"] = fmt.Sprintf("playground-service-%02d", i)
			c["serviceAccountsEnabled"] = true
			c["standardFlowEnabled"] = false
			c["authorizationServicesEnabled"] = false
			c["defaultRoles"] = []string{}
		}

		// Client roles, so role mappings have somewhere client-scoped to point.
		clientRoles[id] = []any{
			map[string]any{"name": "viewer", "description": "read " + id},
			map[string]any{"name": "editor", "description": "write " + id},
		}
		clients = append(clients, c)
	}
	return clients, scopes, clientRoles
}

// mapper is a user-attribute-to-claim protocol mapper, which is the kind that
// carries a realm's own vocabulary into its tokens.
func mapper(name, attribute, claimType string) map[string]any {
	return map[string]any{
		"name":            name,
		"protocol":        "openid-connect",
		"protocolMapper":  "oidc-usermodel-attribute-mapper",
		"consentRequired": false,
		"config": map[string]string{
			"user.attribute":       attribute,
			"claim.name":           attribute,
			"jsonType.label":       claimType,
			"id.token.claim":       "true",
			"access.token.claim":   "true",
			"userinfo.token.claim": "true",
		},
	}
}

// identityProvider is here because an IdP's client secret is one of the values
// an export can quietly return masked, and a realm without one cannot prove it
// was carried.
func identityProvider() map[string]any {
	return map[string]any{
		"alias":                     "playground-oidc",
		"displayName":               "Playground OIDC",
		"providerId":                "oidc",
		"enabled":                   true,
		"trustEmail":                false,
		"storeToken":                false,
		"firstBrokerLoginFlowAlias": "first broker login",
		"config": map[string]string{
			"clientId":         "playground-broker",
			"clientSecret":     "playground-broker-secret",
			"tokenUrl":         "https://idp.example.com/token",
			"authorizationUrl": "https://idp.example.com/auth",
			"defaultScope":     "openid profile email",
			"syncMode":         "IMPORT",
		},
	}
}

// ldapProvider is the component that makes this realm federated, which is what
// makes the playground representative: a federated user is re-read through the
// directory during an export, inside the transaction that writes the page.
func ldapProvider(s shape, host string, port int) map[string]any {
	return map[string]any{
		"id":         randomID(),
		"name":       "ldap-" + s.realm,
		"providerId": "ldap",
		// No providerType here. In a realm export `components` is a map keyed by
		// provider type, and the component under it is a ComponentExportRepresentation,
		// which has no such field — repeating it fails the whole import with the
		// same "unable to read contents from stream" a client-level roles array
		// gives, and for the same reason.
		"config": map[string][]string{
			"enabled":               {"true"},
			"priority":              {"0"},
			"importEnabled":         {"true"},
			"editMode":              {"READ_ONLY"},
			"syncRegistrations":     {"false"},
			"vendor":                {"other"},
			"usernameLDAPAttribute": {"uid"},
			"rdnLDAPAttribute":      {"uid"},
			"uuidLDAPAttribute":     {"entryUUID"},
			"userObjectClasses":     {"inetOrgPerson, organizationalPerson"},
			"connectionUrl":         {fmt.Sprintf("ldap://%s:%d", host, port)},
			"usersDn":               {"ou=people," + s.baseDN},
			"authType":              {"simple"},
			"bindDn":                {"cn=admin," + s.baseDN},
			"bindCredential":        {"adminpassword"},
			"searchScope":           {"2"},
			"pagination":            {"true"},
			"batchSizeForSync":      {"1000"},
			"fullSyncPeriod":        {"-1"},
			"changedSyncPeriod":     {"-1"},
			"cachePolicy":           {"DEFAULT"},
			"trustEmail":            {"true"},
			"connectionPooling":     {"true"},
			// The two timeouts a directory needs and a default install does not
			// set. Without them a stalled connection blocks until the server's
			// transaction reaper gives up, which is the failure that taught
			// PortCloak to classify a transaction timeout at all.
			"connectionTimeout": {"5000"},
			"readTimeout":       {"10000"},
		},
	}
}

func requiredActions() []any {
	actions := []struct {
		alias, name string
		enabled     bool
		def         bool
	}{
		{"CONFIGURE_TOTP", "Configure OTP", true, false},
		{"UPDATE_PASSWORD", "Update Password", true, false},
		{"UPDATE_PROFILE", "Update Profile", true, false},
		{"VERIFY_EMAIL", "Verify Email", true, false},
		{"webauthn-register-passwordless", "Webauthn Register Passwordless", true, false},
		{"TERMS_AND_CONDITIONS", "Terms and Conditions", true, false},
	}
	var out []any
	for i, a := range actions {
		out = append(out, map[string]any{
			"alias":         a.alias,
			"name":          a.name,
			"providerId":    a.alias,
			"enabled":       a.enabled,
			"defaultAction": a.def,
			"priority":      10 * (i + 1),
			"config":        map[string]string{},
		})
	}
	return out
}

// buildUsers writes the realm's own users — the ones with credentials. The
// federated ones live in the directory and are imported by Keycloak on demand;
// generating them here as well would mean two sources for the same person.
func buildUsers(r *mathrand.Rand, s shape, groupPaths, roleNames []string, clients []any) []any {
	crowd := people(r, s.users, groupPaths, roleNames)

	var out []any
	for _, p := range crowd {
		creds := []credential{passwordCredential(p.Username)}
		if p.OTP {
			creds = append(creds, otpCredential(r))
		}
		if p.Passkey {
			creds = append(creds, passkeyCredential(r))
		}

		u := map[string]any{
			"username":      p.Username,
			"enabled":       !p.Disabled,
			"emailVerified": !p.Unverified,
			"firstName":     p.First,
			"lastName":      p.Last,
			"email":         p.Email,
			"credentials":   creds,
			"groups":        p.Groups,
			"realmRoles":    p.Roles,
			"attributes": map[string][]string{
				"department":     {p.Dept},
				"location":       {p.Location},
				"employeeNumber": {p.Employee},
				// Multi-valued, because one value per attribute is the case
				// that always works.
				"tenant": {"tenant-" + strings.ToLower(p.Dept), "shared"},
			},
		}
		if p.Awkward {
			u["attributes"].(map[string][]string)["awkward"] = []string{
				"comma,separated", "quote\"inside", "ünïcode ✓", strings.Repeat("long", 60),
			}
		}
		// A fraction arrive owing an action, which is state a restore has to
		// carry as faithfully as a password.
		if r.Intn(15) == 0 {
			u["requiredActions"] = []string{"UPDATE_PASSWORD"}
		}
		// Client role mappings, on the clients that have roles.
		if len(clients) > 0 && r.Intn(3) == 0 {
			c := clients[r.Intn(len(clients))].(map[string]any)
			u["clientRoles"] = map[string][]string{
				c["clientId"].(string): {[]string{"viewer", "editor"}[r.Intn(2)]},
			}
		}
		out = append(out, u)
	}
	return out
}

// ── The subcommand ──────────────────────────────────────────────────────────

func realmCommand(args []string) error {
	fs := flag.NewFlagSet("realm", flag.ExitOnError)
	var s shape
	s.flags(fs)
	out := fs.String("out", "", "write the realm JSON here (default: stdout)")
	ldapHost := fs.String("ldap-host", "ldap-a", "host the realm's federation provider connects to")
	ldapPort := fs.Int("ldap-port", 1389, "port for the same")
	apply := fs.String("apply", "", "instead of writing, POST the realm to this Keycloak, e.g. http://127.0.0.1:8080")
	admin := fs.String("admin", "admin:admin", "admin credentials for --apply, as user:password")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s.resolve()

	realm := buildRealm(s, *ldapHost, *ldapPort)

	if *apply != "" {
		user, pass, _ := strings.Cut(*admin, ":")
		return applyRealm(*apply, user, pass, realm)
	}

	encoded, err := json.MarshalIndent(realm, "", "  ")
	if err != nil {
		return err
	}
	if *out == "" {
		_, err = os.Stdout.Write(append(encoded, '\n'))
		return err
	}
	if err := os.WriteFile(*out, append(encoded, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s — realm %q, %d users, %d clients, %d groups\n",
		*out, s.realm, s.users, s.clients, s.groups)
	return nil
}
