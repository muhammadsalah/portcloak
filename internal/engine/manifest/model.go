// Package manifest turns a realm export into the inventory that is PortCloak's
// central promise: exactly what this snapshot carries, down to the individual
// secret, so a restore is a faithful clone and nothing important vanishes
// silently.
//
// The governing rule is that secrets are carried in the realm JSON and
// referenced here by type and location, never by value. The manifest is safe to
// read, screenshot and export; the payload is not.
package manifest

import (
	"fmt"
	"sort"
	"strings"
)

// SchemaVersion of the manifest document.
const SchemaVersion = "1.0"

// Status is how a category came out.
type Status string

const (
	// Captured means carried, and counted.
	Captured Status = "captured"
	// Partial means something was carried but not entirely — a masked secret,
	// a category that hit an error partway.
	Partial Status = "partial"
	// Missing means something PortCloak intended to carry did not make it. This
	// is a real problem to investigate.
	Missing Status = "missing"
	// OutOfScope means PortCloak deliberately does not carry this.
	//
	// Keeping this distinct from Missing is not cosmetic. A tool that reports
	// "sessions: missing" on every single capture trains its operator to ignore
	// the report, and then the one real Missing goes unread.
	OutOfScope Status = "outOfScope"
	// NotChecked means a check did not run, so its silence means nothing.
	NotChecked Status = "notChecked"
)

// Category is one line of the completeness report.
type Category struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Count  int    `json:"count,omitempty"`
	// Reason explains anything that is not a plain Captured.
	Reason string `json:"reason,omitempty"`
}

// Completeness is the verdict on a whole snapshot.
type Completeness struct {
	Categories []Category `json:"categories"`
	Warnings   []string   `json:"warnings,omitempty"`
	// Verdict is Full, Partial or Gaps — the badge the library shows.
	Verdict string `json:"verdict"`
}

// Verdicts.
const (
	VerdictFull    = "Complete"
	VerdictPartial = "Partial"
	VerdictGaps    = "Gaps"
)

// Recompute derives the verdict from the categories.
func (c *Completeness) Recompute() {
	verdict := VerdictFull
	for _, cat := range c.Categories {
		switch cat.Status {
		case Missing:
			verdict = VerdictGaps
		case Partial:
			if verdict != VerdictGaps {
				verdict = VerdictPartial
			}
		}
	}
	c.Verdict = verdict
}

// By returns the categories with a given status.
func (c Completeness) By(status Status) []Category {
	var out []Category
	for _, cat := range c.Categories {
		if cat.Status == status {
			out = append(out, cat)
		}
	}
	return out
}

// SecretKind names what sort of secret a ledger entry points at.
type SecretKind string

const (
	SecretClient       SecretKind = "client-secret"
	SecretLDAPBind     SecretKind = "ldap-bind"
	SecretIdP          SecretKind = "idp-secret"
	SecretKeyPrivate   SecretKind = "key-private"
	SecretSMTP         SecretKind = "smtp"
	SecretAuthConfig   SecretKind = "authcfg"
	SecretRegistration SecretKind = "registration-token"
	SecretAttribute    SecretKind = "realm-attribute"
)

// KindLabel renders a secret kind for the ledger table.
func KindLabel(k SecretKind) string {
	switch k {
	case SecretClient:
		return "Client secret"
	case SecretLDAPBind:
		return "LDAP bind credential"
	case SecretIdP:
		return "IdP client secret"
	case SecretKeyPrivate:
		return "Private key material"
	case SecretSMTP:
		return "SMTP password"
	case SecretAuthConfig:
		return "Authenticator secret"
	case SecretRegistration:
		return "Registration access token"
	case SecretAttribute:
		return "Realm attribute"
	default:
		return string(k)
	}
}

// Secret is one ledger entry.
//
// There is no Value field, and there never will be. The ledger is safe to read,
// screenshot and export precisely because the type cannot hold one.
type Secret struct {
	Kind SecretKind `json:"kind"`
	// Location is a path into the realm JSON, e.g. clients[app-web].secret.
	Location string `json:"location"`
	Carried  bool   `json:"carried"`
	// Masked records that the source exported a placeholder rather than a real
	// value. Shipping it as if it were real is the failure this catches.
	Masked bool `json:"masked"`
	// Note explains a Carried:false or Masked:true entry.
	Note string `json:"note,omitempty"`
	// Algorithm is set for key material, so the ledger says what kind of key.
	Algorithm string `json:"algorithm,omitempty"`
}

// Key is one key provider carried in the snapshot.
type Key struct {
	KID       string `json:"kid,omitempty"`
	Provider  string `json:"provider"`
	Name      string `json:"name,omitempty"`
	Type      string `json:"type,omitempty"`
	Algorithm string `json:"algorithm,omitempty"`
	Use       string `json:"use,omitempty"`
	Active    bool   `json:"active"`
	Priority  int    `json:"priority,omitempty"`
	// PrivateCarried is the token-continuity signal: tokens signed before the
	// move stay verifiable only if the private material travelled.
	PrivateCarried bool `json:"privateCarried"`
	// KeystoreFile is set for java-keystore providers, whose material may live
	// in a file on disk rather than in the component.
	KeystoreFile string `json:"keystoreFile,omitempty"`
}

// ClientSummary is one client, as the inspector's table shows it.
type ClientSummary struct {
	ClientID       string   `json:"clientId"`
	Name           string   `json:"name,omitempty"`
	Enabled        bool     `json:"enabled"`
	Protocol       string   `json:"protocol"`
	Confidential   bool     `json:"confidential"`
	SecretPresent  bool     `json:"secretPresent"`
	SecretMasked   bool     `json:"secretMasked"`
	RedirectURIs   []string `json:"redirectUris,omitempty"`
	Mappers        int      `json:"mappers"`
	Authorization  bool     `json:"authorization"`
	ServiceAccount bool     `json:"serviceAccount"`
	DefaultScopes  []string `json:"defaultScopes,omitempty"`
	OptionalScopes []string `json:"optionalScopes,omitempty"`
}

// ClientScopeSummary is one realm-level client scope.
type ClientScopeSummary struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Mappers  int    `json:"mappers"`
}

// RoleSummary is one realm or client role.
type RoleSummary struct {
	Name      string `json:"name"`
	Client    string `json:"client,omitempty"`
	Composite bool   `json:"composite"`
}

// GroupSummary is one group in the flattened hierarchy.
type GroupSummary struct {
	Path        string `json:"path"`
	RealmRoles  int    `json:"realmRoles"`
	ClientRoles int    `json:"clientRoles"`
	Attributes  int    `json:"attributes"`
}

// IdentityProviderSummary is one IdP federation.
type IdentityProviderSummary struct {
	Alias         string `json:"alias"`
	Protocol      string `json:"protocol"`
	Enabled       bool   `json:"enabled"`
	SecretCarried bool   `json:"secretCarried"`
	Mappers       int    `json:"mappers"`
}

// FederationSummary is one user federation provider.
type FederationSummary struct {
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	Enabled       bool   `json:"enabled"`
	ConnectionURL string `json:"connectionUrl,omitempty"`
	UsersDN       string `json:"usersDn,omitempty"`
	BindDN        string `json:"bindDn,omitempty"`
	// BindCarried says whether the bind credential travelled — without it, the
	// federation reconnects at the destination only if it is re-entered there.
	BindCarried bool   `json:"bindCarried"`
	Mappers     int    `json:"mappers"`
	SyncPeriod  string `json:"syncPeriod,omitempty"`
}

// FlowSummary is one authentication flow.
type FlowSummary struct {
	Alias        string `json:"alias"`
	Description  string `json:"description,omitempty"`
	TopLevel     bool   `json:"topLevel"`
	BuiltIn      bool   `json:"builtIn"`
	Executions   int    `json:"executions"`
	BoundAs      string `json:"boundAs,omitempty"`
	ConfigSecret bool   `json:"configSecret"`
}

// DependencyType is what sort of external asset a realm needs.
type DependencyType string

const (
	DependencyTheme    DependencyType = "theme"
	DependencyProvider DependencyType = "provider-jar"
	DependencyKeystore DependencyType = "keystore"
)

// Dependency is an asset the realm references that lives outside the realm
// representation.
//
// PortCloak detects and reports these; it never migrates them. The consequence
// is the reason they are surfaced so prominently: a realm referencing a missing
// theme or authenticator SPI imports cleanly and then fails at login.
type Dependency struct {
	Type       DependencyType `json:"type"`
	Name       string         `json:"name"`
	DetectedAt string         `json:"detectedAt,omitempty"`
	// ReferencedBy says what in the realm needs it, so the list is about this
	// realm rather than about everything deployed on the source.
	ReferencedBy string `json:"referencedBy,omitempty"`
	Action       string `json:"action"`
	Consequence  string `json:"consequence"`
}

// ProvisionAction is the standard action text.
const ProvisionAction = "provision manually at destination before import"

// Counts is the headline inventory.
type Counts struct {
	Users             int `json:"users"`
	Clients           int `json:"clients"`
	ClientScopes      int `json:"clientScopes"`
	RealmRoles        int `json:"realmRoles"`
	ClientRoles       int `json:"clientRoles"`
	Groups            int `json:"groups"`
	IdentityProviders int `json:"identityProviders"`
	Federations       int `json:"federations"`
	KeyProviders      int `json:"keyProviders"`
	AuthFlows         int `json:"authFlows"`
	RequiredActions   int `json:"requiredActions"`
}

// CredentialCounts is the population's credential picture, as presence only.
type CredentialCounts struct {
	PasswordHashes int `json:"passwordHashes"`
	OTP            int `json:"otp"`
	WebAuthn       int `json:"webauthn"`
	RecoveryCodes  int `json:"recoveryCodes"`
	// Algorithms counts users per password hashing algorithm, which is what
	// answers "will these hashes still verify at the destination".
	Algorithms map[string]int `json:"algorithms,omitempty"`
}

// Settings is the realm-level configuration worth showing.
type Settings struct {
	Enabled             bool              `json:"enabled"`
	DisplayName         string            `json:"displayName,omitempty"`
	SSLRequired         string            `json:"sslRequired,omitempty"`
	PasswordPolicy      string            `json:"passwordPolicy,omitempty"`
	BruteForce          bool              `json:"bruteForceProtected"`
	OTPPolicy           string            `json:"otpPolicy,omitempty"`
	WebAuthnPolicy      string            `json:"webAuthnPolicy,omitempty"`
	AccessTokenLifespan int               `json:"accessTokenLifespan,omitempty"`
	SSOSessionIdle      int               `json:"ssoSessionIdleTimeout,omitempty"`
	SSOSessionMax       int               `json:"ssoSessionMaxLifespan,omitempty"`
	Themes              map[string]string `json:"themes,omitempty"`
	Locales             []string          `json:"supportedLocales,omitempty"`
	DefaultLocale       string            `json:"defaultLocale,omitempty"`
	SMTPConfigured      bool              `json:"smtpConfigured"`
	EventsEnabled       bool              `json:"eventsEnabled"`
	DefaultGroups       []string          `json:"defaultGroups,omitempty"`
}

// Source is where and how a snapshot was taken.
type Source struct {
	EnvironmentName    string `json:"environmentName,omitempty"`
	Kind               string `json:"kind"`
	KeycloakVersion    string `json:"keycloakVersion,omitempty"`
	CaptureMode        string `json:"captureMode"`
	ExecutionMode      string `json:"executionMode"`
	CloneRef           string `json:"cloneRef,omitempty"`
	SecretVerification string `json:"secretVerification"`
	DependencyScan     string `json:"dependencyScan"`
	UsersMode          string `json:"usersMode,omitempty"`
}

// Manifest is the whole inventory.
type Manifest struct {
	SchemaVersion string `json:"schemaVersion"`
	Realm         string `json:"realm"`
	Source        Source `json:"source"`

	Counts      Counts           `json:"counts"`
	Credentials CredentialCounts `json:"credentials"`
	Settings    Settings         `json:"settings"`

	Keys              []Key                     `json:"keys"`
	Clients           []ClientSummary           `json:"clients"`
	ClientScopes      []ClientScopeSummary      `json:"clientScopes"`
	RealmRoles        []RoleSummary             `json:"realmRoles"`
	ClientRoles       []RoleSummary             `json:"clientRoles"`
	Groups            []GroupSummary            `json:"groups"`
	IdentityProviders []IdentityProviderSummary `json:"identityProviders"`
	Federations       []FederationSummary       `json:"federations"`
	Flows             []FlowSummary             `json:"flows"`

	Secrets              []Secret     `json:"secrets"`
	ExternalDependencies []Dependency `json:"externalDependencies"`

	Completeness Completeness `json:"completeness"`
}

// ActiveSigningKey returns the key an operator cares about most: the one whose
// presence decides whether tokens minted before the move still verify after it.
//
// Asymmetric keys outrank HMAC regardless of priority. The continuity claim is
// specifically that a token issued by the source verifies against the
// destination's JWKS, and only an asymmetric key is published there — reporting
// an HMAC key as the continuity signal would answer a question nobody asked.
func (m Manifest) ActiveSigningKey() (Key, bool) {
	var best Key
	found := false
	for _, k := range m.Keys {
		if !k.Active || strings.EqualFold(k.Use, "enc") {
			continue
		}
		if !found || signingRank(k) > signingRank(best) ||
			(signingRank(k) == signingRank(best) && k.Priority > best.Priority) {
			best, found = k, true
		}
	}
	return best, found
}

func signingRank(k Key) int {
	switch k.Type {
	case "RSA":
		return 3
	case "EC":
		return 2
	case "keystore":
		return 2
	default:
		return 1
	}
}

// TokenContinuity reports whether tokens signed before a move stay verifiable
// afterwards, and the sentence that explains why.
func (m Manifest) TokenContinuity() (bool, string) {
	k, ok := m.ActiveSigningKey()
	if !ok {
		return false, "No active signing key was found in this snapshot, so tokens issued before a restore will not verify against the destination."
	}
	if !k.PrivateCarried {
		return false, fmt.Sprintf("The active signing key (%s) travelled without its private material, so tokens issued before a restore will not verify against the destination.", describeKey(k))
	}
	return true, fmt.Sprintf("The active signing key (%s) travels with this snapshot, so tokens issued before the move remain verifiable afterwards.", describeKey(k))
}

func describeKey(k Key) string {
	parts := []string{}
	if k.KID != "" {
		parts = append(parts, "kid "+k.KID)
	}
	if k.Algorithm != "" {
		parts = append(parts, k.Algorithm)
	} else if k.Type != "" {
		parts = append(parts, k.Type)
	}
	if len(parts) == 0 {
		return k.Provider
	}
	return strings.Join(parts, ", ")
}

// SecretCounts summarises the ledger for the header line.
func (m Manifest) SecretCounts() (total, carried, masked int) {
	for _, s := range m.Secrets {
		total++
		if s.Carried {
			carried++
		}
		if s.Masked {
			masked++
		}
	}
	return
}

// Sidecar is the non-secret subset written next to a bundle.
//
// It is what makes the library usable without any key at all: an operator can
// survey every snapshot across every backend — counts, completeness, provenance
// — while holding nothing. That is only possible because this type has no field
// a secret could occupy.
type Sidecar struct {
	SchemaVersion    string           `json:"schemaVersion"`
	SnapshotID       string           `json:"snapshotId"`
	Realm            string           `json:"realm"`
	CreatedAt        string           `json:"createdAt"`
	PortCloakVersion string           `json:"portcloakVersion"`
	KeycloakVersion  string           `json:"keycloakVersion,omitempty"`
	Source           Source           `json:"source"`
	Counts           Counts           `json:"counts"`
	Credentials      CredentialCounts `json:"credentials"`
	Encrypted        bool             `json:"encrypted"`
	EncryptionMode   string           `json:"encryptionMode,omitempty"`
	Verdict          string           `json:"verdict"`
	Warnings         []string         `json:"warnings,omitempty"`
	// SecretCount says how many secrets the bundle carries, by count only. The
	// locations stay inside.
	SecretCount int `json:"secretCount"`
	// DependencyCount surfaces external dependencies in the library, so the
	// "2 external deps" badge does not require opening anything.
	DependencyCount int    `json:"dependencyCount"`
	BundleBytes     int64  `json:"bundleBytes"`
	IntegrityRoot   string `json:"integrityRoot"`
	TokenContinuity bool   `json:"tokenContinuity"`
}

// Sidecar derives the non-secret subset.
func (m Manifest) BuildSidecar(snapshotID, createdAt, portcloakVersion string, encrypted bool, mode string, bundleBytes int64, integrityRoot string) Sidecar {
	total, _, _ := m.SecretCounts()
	continuity, _ := m.TokenContinuity()
	return Sidecar{
		SchemaVersion:    SchemaVersion,
		SnapshotID:       snapshotID,
		Realm:            m.Realm,
		CreatedAt:        createdAt,
		PortCloakVersion: portcloakVersion,
		KeycloakVersion:  m.Source.KeycloakVersion,
		Source:           m.Source,
		Counts:           m.Counts,
		Credentials:      m.Credentials,
		Encrypted:        encrypted,
		EncryptionMode:   mode,
		Verdict:          m.Completeness.Verdict,
		Warnings:         m.Completeness.Warnings,
		SecretCount:      total,
		DependencyCount:  len(m.ExternalDependencies),
		BundleBytes:      bundleBytes,
		IntegrityRoot:    integrityRoot,
		TokenContinuity:  continuity,
	}
}

// sortSecrets orders the ledger so it reads the same way every time.
func sortSecrets(s []Secret) {
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].Kind != s[j].Kind {
			return s[i].Kind < s[j].Kind
		}
		return s[i].Location < s[j].Location
	})
}
