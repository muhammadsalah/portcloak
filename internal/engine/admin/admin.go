// Package admin is the optional Admin REST pass.
//
// Everything here is strictly secondary. Offline kc.sh export is the
// authoritative capture mechanism and reads the realm straight from the
// database; the Admin API exists only to confirm that what the export produced
// is real, and to notice the assets a realm depends on that live outside it.
//
// An unreachable Admin API is a normal, expected outcome — an offline capture
// from a stopped Keycloak has no Admin API by definition — so nothing here ever
// fails a capture.
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/manifest"
	"portcloak/internal/engine/realm"
	"portcloak/internal/engine/resil"
)

// Client is the Admin REST client.
type Client struct {
	base   string
	realm  string
	client *http.Client

	authRealm string
	clientID  string
	username  string
	password  string

	mu      sync.Mutex
	token   string
	expires time.Time
}

// New builds a client from an environment, or reports that this environment has
// no Admin API configured — which is a supported configuration, not an error.
func New(env config.Environment, creds config.CredentialStore) (*Client, error) {
	if env.AdminBaseURL == "" {
		return nil, nil
	}
	secret, err := config.Resolve(creds, env.AdminCredentialRef, env.Name+" (Admin API)")
	if err != nil {
		return nil, err
	}

	authRealm := env.AdminRealm
	if authRealm == "" {
		authRealm = "master"
	}
	clientID := env.AdminClientID
	if clientID == "" {
		clientID = "admin-cli"
	}

	transport := http.DefaultTransport
	if env.AdminInsecureTLS {
		transport = insecureTransport()
	}

	return &Client{
		base:      strings.TrimRight(env.AdminBaseURL, "/"),
		client:    &http.Client{Timeout: 30 * time.Second, Transport: transport},
		authRealm: authRealm,
		clientID:  clientID,
		username:  env.AdminUser,
		password:  secret,
	}, nil
}

// ErrNotConfigured is what Check reports for an environment that has no Admin
// API. It is a supported configuration, so callers treat it as a note.
var ErrNotConfigured = errors.New("no Admin API is configured on this environment")

// Reachable reports whether the Admin API answered.
//
// It is a question, not a transfer, so it is asked once and the answer is the
// first one — a slow retry loop here would delay a capture that does not need
// the Admin API at all.
func (c *Client) Reachable(ctx context.Context) bool { return c.Check(ctx) == nil }

// Check is Reachable with the reason kept.
//
// The bool is what a capture needs — the Admin API is optional and its absence
// is a note. The reason is what the environment editor needs: "not reachable"
// over a URL the operator can open in a browser is not a diagnosis, and an
// untrusted certificate is the case where the difference decides whether they
// find the setting that fixes it.
func (c *Client) Check(ctx context.Context) error {
	// A nil client is an environment with no Admin API — a supported
	// configuration, and not a reachable one. Returning nil here would have
	// made Reachable answer true for a server that does not exist.
	if c == nil {
		return ErrNotConfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := c.token_(ctx)
	return err
}

// Realms lists the realms the credentials can see, for the capture wizard.
func (c *Client) Realms(ctx context.Context) ([]string, error) {
	if c == nil {
		return nil, nil
	}
	var out []struct {
		Realm string `json:"realm"`
	}
	if err := c.get(ctx, "/admin/realms", &out); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(out))
	for _, r := range out {
		names = append(names, r.Realm)
	}
	sort.Strings(names)
	return names, nil
}

// VerifySecrets confirms exported values are real rather than masked.
//
// The check is on shape and provenance, never on comparing values. The verifier
// must not become a second path by which a secret is fetched and handled.
func (c *Client) VerifySecrets(ctx context.Context, realmName string, secrets []manifest.Secret) (map[string]string, error) {
	if c == nil {
		return nil, nil
	}
	masked := map[string]string{}

	// Client secrets are the case that matters most: a client whose secret
	// exported as a placeholder imports perfectly and then fails silently at
	// the first authentication.
	var clients []struct {
		ID           string `json:"id"`
		ClientID     string `json:"clientId"`
		PublicClient bool   `json:"publicClient"`
		BearerOnly   bool   `json:"bearerOnly"`
		Secret       string `json:"secret"`
	}
	if err := c.get(ctx, "/admin/realms/"+url.PathEscape(realmName)+"/clients", &clients); err != nil {
		return nil, err
	}

	live := map[string]string{}
	for _, cl := range clients {
		if cl.PublicClient || cl.BearerOnly {
			continue
		}
		live[cl.ClientID] = cl.Secret
	}

	for _, s := range secrets {
		if s.Kind != manifest.SecretClient {
			continue
		}
		name, _, ok := bracketed(s.Location, "clients[")
		if !ok {
			continue
		}
		switch {
		case s.Masked:
			masked[s.Location] = fmt.Sprintf("The export produced a placeholder rather than a real secret for %s.", name)
		case !s.Carried:
			masked[s.Location] = fmt.Sprintf("No secret was carried for %s.", name)
		default:
			value, present := live[name]
			if !present {
				// The client exists in the export but not on the running
				// server. Reporting "could not verify" is the honest answer;
				// assuming good is how a dud secret ships.
				masked[s.Location] = fmt.Sprintf("PortCloak could not confirm the secret for %s, because that client is not on the running server.", name)
				continue
			}
			if manifest.IsMasked(value) {
				// The Admin API itself masks on some versions, which says
				// nothing about the export. Not a finding.
				continue
			}
		}
	}

	// Key providers are the other case with a real consequence: a masked
	// private key means tokens signed before the move stop verifying after it.
	for _, s := range secrets {
		if s.Kind != manifest.SecretKeyPrivate {
			continue
		}
		if s.Masked {
			masked[s.Location] = "The export produced a placeholder rather than the private key material, so token continuity would be lost."
		}
	}

	if len(masked) == 0 {
		return map[string]string{}, nil
	}
	return masked, nil
}

// DetectDependencies enumerates the assets a realm references that live outside
// the realm representation.
//
// The cross-reference is the point: every deployed theme reported as a
// dependency would make the list worthless, so only what this realm actually
// references is reported.
func (c *Client) DetectDependencies(ctx context.Context, realmName string, rep *realm.Representation) ([]manifest.Dependency, error) {
	if c == nil {
		return nil, nil
	}

	deployed, err := c.deployedThemes(ctx)
	if err != nil {
		return nil, err
	}
	providers, err := c.deployedProviders(ctx)
	if err != nil {
		// Provider information is not exposed by every version, and its absence
		// is not a reason to abandon theme detection.
		providers = nil
	}

	var out []manifest.Dependency
	seen := map[string]bool{}

	// Themes the realm selects.
	for slot, name := range map[string]string{
		"login": rep.LoginTheme, "account": rep.AccountTheme,
		"admin": rep.AdminTheme, "email": rep.EmailTheme,
	} {
		if name == "" || builtInTheme(name) {
			continue
		}
		key := "theme\x00" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		d := manifest.Dependency{
			Type:         manifest.DependencyTheme,
			Name:         name,
			ReferencedBy: "the realm's " + slot + " theme",
			Action:       manifest.ProvisionAction,
			Consequence:  "Without this theme the realm imports cleanly and then fails at login.",
		}
		if at, ok := deployed[name]; ok {
			d.DetectedAt = at
		}
		out = append(out, d)
	}

	// Provider JARs the realm's authenticators and mappers depend on.
	referenced := referencedProviders(rep)
	for _, spi := range referenced {
		jar, ok := providers[spi]
		if !ok {
			continue
		}
		// The name is what an operator would recognise — the JAR's own file
		// name — while the path is where it was found.
		name := path.Base(jar)
		if name == "." || name == "/" {
			name = spi
		}
		key := "jar\x00" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, manifest.Dependency{
			Type:         manifest.DependencyProvider,
			Name:         name,
			DetectedAt:   jar,
			ReferencedBy: "the authenticator " + spi,
			Action:       manifest.ProvisionAction,
			Consequence:  "Without this provider the realm imports cleanly and then fails at login, because the authenticator it names does not exist.",
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// builtInTheme recognises the themes Keycloak ships, so a realm using only
// those reports no dependencies rather than a false positive.
func builtInTheme(name string) bool {
	switch strings.ToLower(name) {
	case "keycloak", "keycloak.v2", "base", "rh-sso", "keycloak.v3":
		return true
	}
	return false
}

// referencedProviders lists the authenticator and mapper provider ids a realm
// actually uses, which is what turns "everything deployed" into "what this
// realm needs".
func referencedProviders(rep *realm.Representation) []string {
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		if id == "" || builtInProvider(id) || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}

	for _, f := range rep.AuthenticationFlows {
		for _, e := range f.Executions {
			add(e.Authenticator)
		}
	}
	for _, ra := range rep.RequiredActions {
		add(ra.ProviderID)
	}
	for _, listener := range rep.EventsListeners {
		add(listener)
	}
	for _, c := range rep.Clients {
		for _, m := range c.ProtocolMappers {
			add(m.ProtocolMapper)
		}
	}
	for _, cs := range rep.ClientScopes {
		for _, m := range cs.ProtocolMappers {
			add(m.ProtocolMapper)
		}
	}
	sort.Strings(out)
	return out
}

// builtInProvider recognises the provider ids Keycloak ships. The prefixes are
// the ones the distribution uses, so anything outside them is a custom
// deployment worth reporting.
func builtInProvider(id string) bool {
	for _, prefix := range []string{
		"auth-", "direct-grant-", "reset-", "registration-", "conditional-",
		"idp-", "oidc-", "saml-", "docker-", "client-",
		"CONFIGURE_", "UPDATE_", "VERIFY_", "TERMS_", "delete_account",
		"webauthn-", "identity-provider-",
	} {
		if strings.HasPrefix(id, prefix) {
			return true
		}
	}
	switch id {
	case "jboss-logging", "email", "basic-auth", "basic-auth-otp", "no-cookie-redirect":
		return true
	}
	return false
}

// deployedThemes reads what is actually deployed on the server.
func (c *Client) deployedThemes(ctx context.Context) (map[string]string, error) {
	var info struct {
		Themes map[string][]struct {
			Name string `json:"name"`
		} `json:"themes"`
	}
	if err := c.get(ctx, "/admin/serverinfo", &info); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, group := range info.Themes {
		for _, t := range group {
			out[t.Name] = path.Join("/opt/keycloak/themes", t.Name)
		}
	}
	return out, nil
}

// deployedProviders maps a provider id to the JAR it came from, where the
// server exposes that.
func (c *Client) deployedProviders(ctx context.Context) (map[string]string, error) {
	var info struct {
		Providers map[string]struct {
			Providers map[string]struct {
				Order int `json:"order"`
			} `json:"providers"`
		} `json:"providers"`
		Deployments []struct {
			Name      string   `json:"name"`
			Providers []string `json:"providers"`
		} `json:"deployments"`
	}
	if err := c.get(ctx, "/admin/serverinfo", &info); err != nil {
		return nil, err
	}

	out := map[string]string{}
	for _, d := range info.Deployments {
		for _, p := range d.Providers {
			out[p] = path.Join("/opt/keycloak/providers", d.Name)
		}
	}
	// Where deployments are not reported, a provider id that is not built in is
	// still worth naming, with no path.
	for spi, group := range info.Providers {
		_ = spi
		for id := range group.Providers {
			if _, known := out[id]; !known && !builtInProvider(id) {
				out[id] = ""
			}
		}
	}
	return out, nil
}

// RealmCounts reads a live realm's entity counts, for post-restore validation.
type RealmCounts struct {
	Exists            bool     `json:"exists"`
	Users             int      `json:"users"`
	Clients           int      `json:"clients"`
	ClientScopes      int      `json:"clientScopes"`
	RealmRoles        int      `json:"realmRoles"`
	Groups            int      `json:"groups"`
	IdentityProviders int      `json:"identityProviders"`
	Federations       int      `json:"federations"`
	KeyIDs            []string `json:"keyIds"`
	KeycloakVersion   string   `json:"keycloakVersion,omitempty"`
}

// ReadRealm reads a target realm's shape, for the dry run and for validation.
func (c *Client) ReadRealm(ctx context.Context, realmName string) (RealmCounts, error) {
	if c == nil {
		return RealmCounts{}, resil.Fatal("read the destination realm",
			"This environment has no Admin API configured, so the destination cannot be read.", nil)
	}
	out := RealmCounts{}

	var top map[string]any
	if err := c.get(ctx, "/admin/realms/"+url.PathEscape(realmName), &top); err != nil {
		if isNotFound(err) {
			return out, nil
		}
		return out, err
	}
	out.Exists = true

	base := "/admin/realms/" + url.PathEscape(realmName)
	var userCount struct {
		Count int `json:"count"`
	}
	if err := c.get(ctx, base+"/users/count", &userCount.Count); err == nil {
		out.Users = userCount.Count
	}

	out.Clients = c.countList(ctx, base+"/clients")
	out.ClientScopes = c.countList(ctx, base+"/client-scopes")
	out.RealmRoles = c.countList(ctx, base+"/roles")
	out.Groups = c.countList(ctx, base+"/groups")
	out.IdentityProviders = c.countList(ctx, base+"/identity-provider/instances")
	out.Federations = c.countList(ctx, base+"/components?type=org.keycloak.storage.UserStorageProvider")

	var keys struct {
		Keys []struct {
			KID       string `json:"kid"`
			Status    string `json:"status"`
			Algorithm string `json:"algorithm"`
			Use       string `json:"use"`
		} `json:"keys"`
	}
	if err := c.get(ctx, base+"/keys", &keys); err == nil {
		for _, k := range keys.Keys {
			if k.KID != "" {
				out.KeyIDs = append(out.KeyIDs, k.KID)
			}
		}
		sort.Strings(out.KeyIDs)
	}
	return out, nil
}

func (c *Client) countList(ctx context.Context, p string) int {
	var items []json.RawMessage
	if err := c.get(ctx, p, &items); err != nil {
		return 0
	}
	return len(items)
}

// PartialImport applies a realm representation to a running server, which is
// how the merge strategy is delivered.
func (c *Client) PartialImport(ctx context.Context, realmName string, body []byte, policy string) (PartialImportResult, error) {
	if c == nil {
		return PartialImportResult{}, resil.Fatal("import into the destination",
			"This environment has no Admin API configured, so a merge cannot be applied.", nil).
			WithAdvice("Configure the Admin API on the destination environment, or choose overwrite or skip, which use kc.sh import.")
	}

	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return PartialImportResult{}, resil.Fatal("import into the destination",
			"The realm representation in this snapshot could not be read.", err)
	}
	wrapped["ifResourceExists"] = json.RawMessage(`"` + policy + `"`)
	payload, err := json.Marshal(wrapped)
	if err != nil {
		return PartialImportResult{}, err
	}

	var res PartialImportResult
	if err := c.post(ctx, "/admin/realms/"+url.PathEscape(realmName)+"/partialImport", payload, &res); err != nil {
		return PartialImportResult{}, err
	}
	return res, nil
}

// PartialImportResult is what Keycloak reported about a merge.
type PartialImportResult struct {
	Overwritten int `json:"overwritten"`
	Added       int `json:"added"`
	Skipped     int `json:"skipped"`
	Results     []struct {
		Action       string `json:"action"`
		ResourceType string `json:"resourceType"`
		ResourceName string `json:"resourceName"`
		ID           string `json:"id"`
	} `json:"results"`
}

func bracketed(location, prefix string) (name, field string, ok bool) {
	rest := strings.TrimPrefix(location, prefix)
	name, after, ok := strings.Cut(rest, "]")
	if !ok {
		return "", "", false
	}
	return name, strings.TrimPrefix(after, "."), true
}
