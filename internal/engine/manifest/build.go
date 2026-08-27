// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"portcloak/internal/engine/realm"
)

// MaskPattern is what Keycloak substitutes for a secret it will not disclose.
// Several versions use it, and a client whose secret exports as this imports
// perfectly and then fails silently at the first authentication.
const MaskPattern = "**********"

// IsMasked reports whether an exported value is a placeholder rather than a
// real secret.
//
// An unrecognised shape is reported as "could not verify" by the caller rather
// than assumed good. Assuming good is how a dud secret ships.
func IsMasked(v string) bool {
	t := strings.TrimSpace(v)
	if t == "" {
		return false
	}
	if t == MaskPattern {
		return true
	}
	// Some builds emit a run of asterisks of another length, or the literal
	// placeholder text.
	if strings.Trim(t, "*") == "" && len(t) >= 6 {
		return true
	}
	switch strings.ToLower(t) {
	case "**********", "*****", "masked", "<masked>", "hidden":
		return true
	}
	return false
}

// BuildOptions carries what the builder cannot learn from the export itself.
type BuildOptions struct {
	Source Source
	// UserFiles are the streamed user files, in order.
	UserFiles []string
	// Dependencies come from the optional Admin API scan. Nil means the scan
	// did not run, which is reported as notChecked rather than as "none".
	Dependencies []Dependency
	// DependencyScanRan distinguishes "scanned and found nothing" from "did not
	// scan", which are very different answers.
	DependencyScanRan bool
	// VerificationRan and MaskedLocations come from the optional secret
	// verification pass.
	VerificationRan bool
	MaskedLocations map[string]string
	// Progress reports user-file streaming, which on a large realm takes real
	// time.
	Progress func(file string, users int)
}

// Build walks a realm representation and its user files into the inventory.
func Build(ctx context.Context, rep *realm.Representation, opts BuildOptions) (*Manifest, error) {
	if rep == nil {
		return nil, fmt.Errorf("there is no realm representation to build a manifest from")
	}
	m := &Manifest{
		SchemaVersion: SchemaVersion,
		Realm:         rep.Realm,
		Source:        opts.Source,
	}
	if opts.MaskedLocations == nil {
		opts.MaskedLocations = map[string]string{}
	}

	m.Settings = buildSettings(rep)
	m.Keys = buildKeys(rep)
	m.Clients = buildClients(rep, opts)
	m.ClientScopes = buildClientScopes(rep)
	m.RealmRoles, m.ClientRoles = buildRoles(rep)
	m.Groups = buildGroups(rep)
	m.IdentityProviders = buildIdentityProviders(rep)
	m.Federations = buildFederations(rep)
	m.Flows = buildFlows(rep)
	m.Secrets = buildSecretLedger(rep, opts)

	counts, credentials, err := countPopulation(ctx, rep, opts)
	if err != nil {
		return nil, err
	}
	m.Credentials = credentials

	m.Counts = Counts{
		Users:             counts,
		Clients:           len(m.Clients),
		ClientScopes:      len(m.ClientScopes),
		RealmRoles:        len(m.RealmRoles),
		ClientRoles:       len(m.ClientRoles),
		Groups:            len(m.Groups),
		IdentityProviders: len(m.IdentityProviders),
		Federations:       len(m.Federations),
		KeyProviders:      len(m.Keys),
		AuthFlows:         len(m.Flows),
		RequiredActions:   len(rep.RequiredActions),
	}

	m.ExternalDependencies = buildDependencies(rep, opts)
	m.Completeness = buildCompleteness(m, opts)
	return m, nil
}

func buildSettings(rep *realm.Representation) Settings {
	s := Settings{
		Enabled:             rep.Enabled,
		DisplayName:         rep.DisplayName,
		SSLRequired:         rep.SSLRequired,
		PasswordPolicy:      rep.PasswordPolicy,
		BruteForce:          rep.BruteForceProtected,
		AccessTokenLifespan: rep.AccessTokenLifespan,
		SSOSessionIdle:      rep.SSOSessionIdleTimeout,
		SSOSessionMax:       rep.SSOSessionMaxLifespan,
		Locales:             rep.SupportedLocales,
		DefaultLocale:       rep.DefaultLocale,
		SMTPConfigured:      len(rep.SMTPServer) > 0,
		EventsEnabled:       rep.EventsEnabled,
		DefaultGroups:       rep.DefaultGroups,
	}
	if rep.OTPPolicyType != "" {
		s.OTPPolicy = fmt.Sprintf("%s · %s · %d digits · %ds",
			rep.OTPPolicyType, rep.OTPPolicyAlgorithm, rep.OTPPolicyDigits, rep.OTPPolicyPeriod)
	}
	if rep.WebAuthnPolicyRpEntityName != "" || len(rep.WebAuthnPolicySignatureAlgorithms) > 0 {
		s.WebAuthnPolicy = strings.TrimSpace(rep.WebAuthnPolicyRpEntityName + " " +
			strings.Join(rep.WebAuthnPolicySignatureAlgorithms, ", "))
	}
	themes := map[string]string{}
	for name, value := range map[string]string{
		"login": rep.LoginTheme, "account": rep.AccountTheme,
		"admin": rep.AdminTheme, "email": rep.EmailTheme,
	} {
		if value != "" {
			themes[name] = value
		}
	}
	if len(themes) > 0 {
		s.Themes = themes
	}
	return s
}

// buildKeys reads key providers out of the components map. They live there
// because Keycloak stores them as components, which is exactly why private key
// material travels in an ordinary export.
func buildKeys(rep *realm.Representation) []Key {
	var out []Key
	for _, c := range rep.Components[realm.ComponentKeyProvider] {
		k := Key{
			Provider: c.ProviderID,
			Name:     c.Name,
			Active:   boolConfig(c, "active", true),
			Priority: intConfig(c, "priority"),
			Use:      keyUse(c),
			Type:     keyType(c.ProviderID),
		}
		if a := c.ConfigValue("algorithm"); a != "" {
			k.Algorithm = a
		}
		if kid := c.ConfigValue("kid"); kid != "" {
			k.KID = kid
		}
		if f := c.ConfigValue("keystore"); f != "" {
			k.KeystoreFile = f
		}
		k.PrivateCarried = privateMaterialPresent(c)
		out = append(out, k)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func keyType(providerID string) string {
	switch {
	case strings.HasPrefix(providerID, "rsa"):
		return "RSA"
	case strings.HasPrefix(providerID, "ecdsa"):
		return "EC"
	case strings.HasPrefix(providerID, "hmac"):
		return "HMAC"
	case strings.HasPrefix(providerID, "aes"):
		return "AES"
	case strings.Contains(providerID, "keystore"):
		return "keystore"
	default:
		return providerID
	}
}

func keyUse(c realm.Component) string {
	if u := c.ConfigValue("keyUse"); u != "" {
		return strings.ToLower(u)
	}
	if strings.Contains(c.ProviderID, "enc") {
		return "enc"
	}
	if strings.HasPrefix(c.ProviderID, "aes") {
		return "enc"
	}
	return "sig"
}

// privateMaterialPresent is the token-continuity check, done by looking for the
// config keys that hold private material rather than by reading them.
func privateMaterialPresent(c realm.Component) bool {
	for _, key := range []string{"privateKey", "secret", "secretSize", "keystorePassword"} {
		if c.HasConfig(key) {
			// A generated key provider reports secretSize rather than a value,
			// and that is genuine: the material is regenerated deterministically
			// from the component on import.
			if key == "secretSize" && !c.HasConfig("secret") {
				continue
			}
			if v := c.ConfigValue(key); IsMasked(v) {
				return false
			}
			return true
		}
	}
	// hmac-generated and aes-generated store their material under "secret";
	// a keystore provider points at a file instead.
	return false
}

func buildClients(rep *realm.Representation, opts BuildOptions) []ClientSummary {
	out := make([]ClientSummary, 0, len(rep.Clients))
	for _, c := range rep.Clients {
		loc := fmt.Sprintf("clients[%s].secret", c.ClientID)
		s := ClientSummary{
			ClientID:       c.ClientID,
			Name:           c.Name,
			Enabled:        c.Enabled,
			Protocol:       protocolOf(c),
			Confidential:   c.Confidential(),
			SecretPresent:  c.Secret != "" && !IsMasked(c.Secret),
			SecretMasked:   IsMasked(c.Secret) || opts.MaskedLocations[loc] != "",
			RedirectURIs:   c.RedirectURIs,
			Mappers:        len(c.ProtocolMappers),
			Authorization:  c.AuthorizationServicesEnabled,
			ServiceAccount: c.ServiceAccountsEnabled,
			DefaultScopes:  c.DefaultClientScopes,
			OptionalScopes: c.OptionalClientScopes,
		}
		if s.SecretMasked {
			s.SecretPresent = false
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ClientID < out[j].ClientID })
	return out
}

func protocolOf(c realm.Client) string {
	if c.Protocol != "" {
		return c.Protocol
	}
	return "openid-connect"
}

func buildClientScopes(rep *realm.Representation) []ClientScopeSummary {
	out := make([]ClientScopeSummary, 0, len(rep.ClientScopes))
	for _, s := range rep.ClientScopes {
		out = append(out, ClientScopeSummary{Name: s.Name, Protocol: s.Protocol, Mappers: len(s.ProtocolMappers)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func buildRoles(rep *realm.Representation) (realmRoles, clientRoles []RoleSummary) {
	for _, r := range rep.Roles.Realm {
		realmRoles = append(realmRoles, RoleSummary{Name: r.Name, Composite: r.Composite})
	}
	clients := make([]string, 0, len(rep.Roles.Client))
	for client := range rep.Roles.Client {
		clients = append(clients, client)
	}
	sort.Strings(clients)
	for _, client := range clients {
		for _, r := range rep.Roles.Client[client] {
			clientRoles = append(clientRoles, RoleSummary{Name: r.Name, Client: client, Composite: r.Composite})
		}
	}
	sort.SliceStable(realmRoles, func(i, j int) bool { return realmRoles[i].Name < realmRoles[j].Name })
	return realmRoles, clientRoles
}

func buildGroups(rep *realm.Representation) []GroupSummary {
	flat := realm.FlattenGroups(rep.Groups)
	out := make([]GroupSummary, 0, len(flat))
	for _, g := range flat {
		clientRoles := 0
		for _, rs := range g.ClientRoles {
			clientRoles += len(rs)
		}
		out = append(out, GroupSummary{
			Path:        g.Path,
			RealmRoles:  len(g.RealmRoles),
			ClientRoles: clientRoles,
			Attributes:  len(g.Attributes),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func buildIdentityProviders(rep *realm.Representation) []IdentityProviderSummary {
	mappers := map[string]int{}
	for _, m := range rep.IdentityProviderMappers {
		mappers[m.IdentityProviderAlias]++
	}
	out := make([]IdentityProviderSummary, 0, len(rep.IdentityProviders))
	for _, idp := range rep.IdentityProviders {
		secret := idp.Config["clientSecret"]
		out = append(out, IdentityProviderSummary{
			Alias:         idp.Alias,
			Protocol:      idp.ProviderID,
			Enabled:       idp.Enabled,
			SecretCarried: secret != "" && !IsMasked(secret),
			Mappers:       mappers[idp.Alias],
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	return out
}

func buildFederations(rep *realm.Representation) []FederationSummary {
	var out []FederationSummary
	for _, c := range rep.Components[realm.ComponentUserStorageProvider] {
		bind := c.ConfigValue("bindCredential")
		f := FederationSummary{
			Name:          c.Name,
			Provider:      c.ProviderID,
			Enabled:       boolConfig(c, "enabled", true),
			ConnectionURL: c.ConfigValue("connectionUrl"),
			UsersDN:       c.ConfigValue("usersDn"),
			BindDN:        c.ConfigValue("bindDn"),
			BindCarried:   bind != "" && !IsMasked(bind),
			Mappers:       len(c.SubComponents[realm.ComponentLDAPMapper]),
			SyncPeriod:    c.ConfigValue("fullSyncPeriod"),
		}
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func buildFlows(rep *realm.Representation) []FlowSummary {
	configSecret := map[string]bool{}
	for _, cfg := range rep.AuthenticatorConfig {
		for k, v := range cfg.Config {
			if looksSecretKey(k) && v != "" {
				configSecret[cfg.Alias] = true
			}
		}
	}
	bindings := map[string]string{
		rep.BrowserFlow:              "browser",
		rep.DirectGrantFlow:          "direct grant",
		rep.RegistrationFlow:         "registration",
		rep.ResetCredentialsFlow:     "reset credentials",
		rep.ClientAuthenticationFlow: "client authentication",
		rep.DockerAuthenticationFlow: "docker authentication",
	}

	out := make([]FlowSummary, 0, len(rep.AuthenticationFlows))
	for _, f := range rep.AuthenticationFlows {
		s := FlowSummary{
			Alias:       f.Alias,
			Description: f.Description,
			TopLevel:    f.TopLevel,
			BuiltIn:     f.BuiltIn,
			Executions:  len(f.Executions),
			BoundAs:     bindings[f.Alias],
		}
		for _, e := range f.Executions {
			if e.AuthenticatorConfig != "" && configSecret[e.AuthenticatorConfig] {
				s.ConfigSecret = true
			}
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	return out
}

// buildSecretLedger enumerates every secret the snapshot carries, by location
// and kind. This is what lets an operator audit the blast radius of a bundle
// without the bundle's contents being disclosed to do it.
func buildSecretLedger(rep *realm.Representation, opts BuildOptions) []Secret {
	var out []Secret
	add := func(kind SecretKind, location, value, algorithm string) {
		s := Secret{Kind: kind, Location: location, Algorithm: algorithm}
		switch {
		case value == "":
			s.Carried = false
			s.Note = "not carried — set it by hand after import"
		case IsMasked(value):
			s.Carried = false
			s.Masked = true
			s.Note = "the source exported a placeholder rather than the real value"
		default:
			s.Carried = true
		}
		if reason, masked := opts.MaskedLocations[location]; masked {
			s.Carried, s.Masked, s.Note = false, true, reason
		}
		out = append(out, s)
	}

	for _, c := range rep.Clients {
		if !c.Confidential() {
			continue
		}
		add(SecretClient, fmt.Sprintf("clients[%s].secret", c.ClientID), c.Secret, "")
		if c.RegistrationAccessToken != "" {
			add(SecretRegistration, fmt.Sprintf("clients[%s].registrationAccessToken", c.ClientID), c.RegistrationAccessToken, "")
		}
	}

	for _, c := range rep.Components[realm.ComponentKeyProvider] {
		for _, key := range []string{"privateKey", "secret", "keystorePassword"} {
			if !c.HasConfig(key) {
				continue
			}
			add(SecretKeyPrivate,
				fmt.Sprintf("components[keys/%s].config.%s", c.Name, key),
				c.ConfigValue(key), c.ConfigValue("algorithm"))
		}
	}

	for _, c := range rep.Components[realm.ComponentUserStorageProvider] {
		if c.HasConfig("bindCredential") {
			add(SecretLDAPBind, fmt.Sprintf("components[%s/%s].config.bindCredential", c.ProviderID, c.Name), c.ConfigValue("bindCredential"), "")
		}
		if c.HasConfig("keyTab") {
			add(SecretLDAPBind, fmt.Sprintf("components[%s/%s].config.keyTab", c.ProviderID, c.Name), c.ConfigValue("keyTab"), "")
		}
	}

	for _, idp := range rep.IdentityProviders {
		for _, key := range []string{"clientSecret", "signingCertificate", "encryptionPrivateKey"} {
			if v, ok := idp.Config[key]; ok && v != "" {
				add(SecretIdP, fmt.Sprintf("identityProviders[%s].config.%s", idp.Alias, key), v, "")
			}
		}
	}

	if pw, ok := rep.SMTPServer["password"]; ok && pw != "" {
		add(SecretSMTP, "smtpServer.password", pw, "")
	}

	for _, cfg := range rep.AuthenticatorConfig {
		keys := make([]string, 0, len(cfg.Config))
		for k := range cfg.Config {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if looksSecretKey(k) && cfg.Config[k] != "" {
				add(SecretAuthConfig, fmt.Sprintf("authenticatorConfig[%s].config.%s", cfg.Alias, k), cfg.Config[k], "")
			}
		}
	}

	// Realm attributes can hold configuration secrets, so they are scanned
	// rather than assumed harmless.
	attrKeys := make([]string, 0, len(rep.Attributes))
	for k := range rep.Attributes {
		attrKeys = append(attrKeys, k)
	}
	sort.Strings(attrKeys)
	for _, k := range attrKeys {
		if looksSecretKey(k) && rep.Attributes[k] != "" {
			add(SecretAttribute, fmt.Sprintf("attributes.%s", k), rep.Attributes[k], "")
		}
	}

	sortSecrets(out)
	return out
}

func looksSecretKey(k string) bool {
	l := strings.ToLower(strings.NewReplacer("_", "", "-", "", ".", "").Replace(k))
	for _, s := range []string{"secret", "password", "passwd", "credential", "privatekey", "token", "apikey", "keytab"} {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

func countPopulation(ctx context.Context, rep *realm.Representation, opts BuildOptions) (int, CredentialCounts, error) {
	credentials := CredentialCounts{Algorithms: map[string]int{}}
	total := 0

	tally := func(u realm.User) error {
		total++
		s := realm.Summarise(u)
		if s.HasPassword {
			credentials.PasswordHashes++
			algo := s.PasswordAlgorithm
			if algo == "" {
				algo = "unspecified"
			}
			credentials.Algorithms[algo]++
		}
		if s.OTPCount > 0 {
			credentials.OTP += s.OTPCount
		}
		if s.WebAuthnCount > 0 {
			credentials.WebAuthn += s.WebAuthnCount
		}
		if s.RecoveryCodes {
			credentials.RecoveryCodes++
		}
		return nil
	}

	// The realm_file export mode keeps users inline; different_files puts them
	// in siblings. Both are supported, and neither is read whole.
	for _, u := range rep.Users {
		if err := tally(u); err != nil {
			return 0, credentials, err
		}
	}
	for _, path := range opts.UserFiles {
		n, err := realm.StreamUsersFile(ctx, path, tally)
		if err != nil {
			return total, credentials, fmt.Errorf("reading %s: %w", path, err)
		}
		if opts.Progress != nil {
			opts.Progress(path, n)
		}
	}
	if len(credentials.Algorithms) == 0 {
		credentials.Algorithms = nil
	}
	return total, credentials, nil
}

// buildDependencies combines the scan's findings with what the realm itself
// references, so the list is about this realm rather than about everything
// deployed on the source.
func buildDependencies(rep *realm.Representation, opts BuildOptions) []Dependency {
	if !opts.DependencyScanRan {
		return opts.Dependencies
	}
	out := append([]Dependency(nil), opts.Dependencies...)

	// A java-keystore key provider points at a file the realm needs and the
	// export does not carry. That is detectable from the realm alone, without
	// any Admin API at all.
	for _, c := range rep.Components[realm.ComponentKeyProvider] {
		f := c.ConfigValue("keystore")
		if f == "" {
			continue
		}
		out = append(out, Dependency{
			Type:         DependencyKeystore,
			Name:         f,
			DetectedAt:   f,
			ReferencedBy: "key provider " + c.Name,
			Action:       ProvisionAction,
			Consequence:  "Without this keystore the realm's signing keys cannot be loaded, so tokens will not be issued.",
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Name < out[j].Name
	})
	return dedupeDependencies(out)
}

func dedupeDependencies(in []Dependency) []Dependency {
	seen := map[string]bool{}
	out := in[:0]
	for _, d := range in {
		k := string(d.Type) + "\x00" + d.Name
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, d)
	}
	return out
}

func buildCompleteness(m *Manifest, opts BuildOptions) Completeness {
	c := Completeness{}
	captured := func(name string, count int) {
		c.Categories = append(c.Categories, Category{Name: name, Status: Captured, Count: count})
	}

	captured("Realm settings", 1)
	captured("Key providers", m.Counts.KeyProviders)
	captured("Clients", m.Counts.Clients)
	captured("Client scopes", m.Counts.ClientScopes)
	captured("Roles", m.Counts.RealmRoles+m.Counts.ClientRoles)
	captured("Groups", m.Counts.Groups)
	captured("Users", m.Counts.Users)
	captured("Credentials", m.Credentials.PasswordHashes+m.Credentials.OTP+m.Credentials.WebAuthn)
	captured("User federation", m.Counts.Federations)
	captured("Identity providers", m.Counts.IdentityProviders)
	captured("Authentication flows", m.Counts.AuthFlows)
	if m.Settings.SMTPConfigured {
		captured("SMTP", 1)
	}

	// Anything the ledger says was masked degrades its own category, naming the
	// client rather than the category alone.
	maskedByCategory := map[string][]string{}
	for _, s := range m.Secrets {
		if !s.Masked {
			continue
		}
		switch s.Kind {
		case SecretClient:
			maskedByCategory["Clients"] = append(maskedByCategory["Clients"], s.Location)
		case SecretKeyPrivate:
			maskedByCategory["Key providers"] = append(maskedByCategory["Key providers"], s.Location)
		case SecretLDAPBind:
			maskedByCategory["User federation"] = append(maskedByCategory["User federation"], s.Location)
		case SecretIdP:
			maskedByCategory["Identity providers"] = append(maskedByCategory["Identity providers"], s.Location)
		case SecretSMTP:
			maskedByCategory["SMTP"] = append(maskedByCategory["SMTP"], s.Location)
		default:
			maskedByCategory["Authentication flows"] = append(maskedByCategory["Authentication flows"], s.Location)
		}
	}
	for i := range c.Categories {
		locations, degraded := maskedByCategory[c.Categories[i].Name]
		if !degraded {
			continue
		}
		c.Categories[i].Status = Partial
		c.Categories[i].Reason = fmt.Sprintf("%d secret(s) were exported masked rather than as real values: %s.",
			len(locations), strings.Join(locations, ", "))
	}

	// Out of scope by design. These are listed every time, so a healthy
	// snapshot never reads as broken and the distinction stays visible.
	c.Categories = append(c.Categories,
		Category{Name: "Online sessions", Status: OutOfScope,
			Reason: "Sessions are not carried by design. Users re-authenticate after a restore."},
		Category{Name: "Offline sessions", Status: OutOfScope,
			Reason: "Offline sessions and offline tokens are not carried by design."},
		Category{Name: "Custom theme files", Status: OutOfScope,
			Reason: "Theme files are deployment assets, not realm data. They are detected and reported, never migrated."},
		Category{Name: "Provider and SPI JARs", Status: OutOfScope,
			Reason: "Provider JARs are deployment assets, not realm data. They are detected and reported, never migrated."},
	)

	switch {
	case !opts.VerificationRan:
		c.Categories = append(c.Categories, Category{
			Name: "Secret verification", Status: NotChecked,
			Reason: "The Admin API was not reachable or verification was declined, so PortCloak could not confirm that the exported secrets are real values rather than placeholders.",
		})
	default:
		masked := 0
		for _, s := range m.Secrets {
			if s.Masked {
				masked++
			}
		}
		if masked > 0 {
			c.Categories = append(c.Categories, Category{
				Name: "Secret verification", Status: Partial, Count: masked,
				Reason: fmt.Sprintf("%d secret(s) were exported masked. Set them by hand at the destination, or they will fail at the first authentication.", masked),
			})
		} else {
			c.Categories = append(c.Categories, Category{
				Name: "Secret verification", Status: Captured, Count: len(m.Secrets),
				Reason: "Every carried secret was confirmed to be a real value.",
			})
		}
	}

	if !opts.DependencyScanRan {
		c.Categories = append(c.Categories, Category{
			Name: "External dependency detection", Status: NotChecked,
			Reason: "Dependency detection did not run, so the absence of a list here does not mean the realm has no external dependencies.",
		})
	}

	// Warnings are the sentences the result screen shows, in the operator's
	// terms rather than as category names.
	continuity, sentence := m.TokenContinuity()
	c.Warnings = append(c.Warnings, "Sessions are out of scope by design; users will re-authenticate after restore. "+sentence)
	if !continuity {
		c.Categories = append(c.Categories, Category{
			Name: "Token continuity", Status: Partial,
			Reason: sentence,
		})
	}
	if n := len(m.ExternalDependencies); n > 0 {
		themes, jars, keystores := 0, 0, 0
		for _, d := range m.ExternalDependencies {
			switch d.Type {
			case DependencyTheme:
				themes++
			case DependencyProvider:
				jars++
			case DependencyKeystore:
				keystores++
			}
		}
		c.Warnings = append(c.Warnings, fmt.Sprintf(
			"This realm depends on %s that live outside the realm representation. Deploy them to the destination before importing, or logins will fail after a successful-looking import.",
			plural(themes, jars, keystores)))
	}
	if m.Counts.Users == 0 {
		c.Warnings = append(c.Warnings, "This snapshot carries no users. If that is unexpected, check the users export mode on the capture.")
	}

	c.Recompute()
	return c
}

func plural(themes, jars, keystores int) string {
	var parts []string
	if themes > 0 {
		parts = append(parts, fmt.Sprintf("%d custom theme%s", themes, s(themes)))
	}
	if jars > 0 {
		parts = append(parts, fmt.Sprintf("%d provider JAR%s", jars, s(jars)))
	}
	if keystores > 0 {
		parts = append(parts, fmt.Sprintf("%d keystore file%s", keystores, s(keystores)))
	}
	switch len(parts) {
	case 0:
		return "assets"
	case 1:
		return parts[0]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
}

func s(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func boolConfig(c realm.Component, key string, fallback bool) bool {
	v := c.ConfigValue(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func intConfig(c realm.Component, key string) int {
	n, _ := strconv.Atoi(c.ConfigValue(key))
	return n
}
