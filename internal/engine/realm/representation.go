// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Package realm models the parts of Keycloak's realm representation PortCloak
// needs to reason about, and parses them without ever loading a whole export
// into memory.
//
// It is deliberately partial. The realm JSON is carried through to the
// destination byte for byte — re-serialising it would put PortCloak in the path
// of the very data it promises to carry faithfully — so this model exists only
// to answer questions about a realm, never to rewrite one.
package realm

import (
	"encoding/json"
)

// Representation is the subset of RealmRepresentation the manifest, the
// inspector and the dependency scanner read.
//
// Unknown fields are simply not decoded. That is safe because nothing here
// writes a realm file: the authoritative artifact stays exactly as kc.sh
// produced it.
type Representation struct {
	Roles       Roles             `json:"roles"`
	Attributes  map[string]string `json:"attributes"`
	DefaultRole *Role             `json:"defaultRole"`
	SMTPServer  map[string]string `json:"smtpServer"`
	// Components hold key providers, LDAP federation and their mappers. They
	// are the reason private key material travels in an ordinary export.
	Components  map[string][]Component `json:"components"`
	Realm       string                 `json:"realm"`
	ID          string                 `json:"id"`
	DisplayName string                 `json:"displayName"`
	SSLRequired string                 `json:"sslRequired"`
	// Password policy has to match at the destination, or imported hashes stop
	// verifying.
	PasswordPolicy string `json:"passwordPolicy"`
	// OTP policy has to match, or existing OTP secrets stop verifying.
	OTPPolicyType      string `json:"otpPolicyType"`
	OTPPolicyAlgorithm string `json:"otpPolicyAlgorithm"`
	// WebAuthn policy has to match, or passkeys stop working.
	WebAuthnPolicyRpEntityName             string `json:"webAuthnPolicyRpEntityName"`
	WebAuthnPolicyRpID                     string `json:"webAuthnPolicyRpId"`
	WebAuthnPolicyPasswordlessRpEntityName string `json:"webAuthnPolicyPasswordlessRpEntityName"`
	WebAuthnPolicyPasswordlessRpID         string `json:"webAuthnPolicyPasswordlessRpId"`
	DefaultLocale                          string `json:"defaultLocale"`
	// Theme selections travel in the realm JSON; the theme files do not, which
	// is why they are detected and reported instead.
	LoginTheme                        string                   `json:"loginTheme"`
	AccountTheme                      string                   `json:"accountTheme"`
	AdminTheme                        string                   `json:"adminTheme"`
	EmailTheme                        string                   `json:"emailTheme"`
	BrowserFlow                       string                   `json:"browserFlow"`
	DirectGrantFlow                   string                   `json:"directGrantFlow"`
	RegistrationFlow                  string                   `json:"registrationFlow"`
	ResetCredentialsFlow              string                   `json:"resetCredentialsFlow"`
	ClientAuthenticationFlow          string                   `json:"clientAuthenticationFlow"`
	DockerAuthenticationFlow          string                   `json:"dockerAuthenticationFlow"`
	WebAuthnPolicySignatureAlgorithms []string                 `json:"webAuthnPolicySignatureAlgorithms"`
	SupportedLocales                  []string                 `json:"supportedLocales"`
	DefaultRoles                      []string                 `json:"defaultRoles"`
	DefaultGroups                     []string                 `json:"defaultGroups"`
	EventsListeners                   []string                 `json:"eventsListeners"`
	Clients                           []Client                 `json:"clients"`
	ClientScopes                      []ClientScope            `json:"clientScopes"`
	Groups                            []Group                  `json:"groups"`
	IdentityProviders                 []IdentityProvider       `json:"identityProviders"`
	IdentityProviderMappers           []IdentityProviderMapper `json:"identityProviderMappers"`
	AuthenticationFlows               []AuthenticationFlow     `json:"authenticationFlows"`
	AuthenticatorConfig               []AuthenticatorConfig    `json:"authenticatorConfig"`
	RequiredActions                   []RequiredAction         `json:"requiredActions"`
	// Users appear here only in the realm_file export mode. In the default
	// different_files mode they live in sibling files and are streamed.
	Users []User `json:"users"`
	// Token and session lifespans. Token continuity relies on these together
	// with the carried signing keys.
	AccessTokenLifespan                int   `json:"accessTokenLifespan"`
	AccessTokenLifespanForImplicitFlow int   `json:"accessTokenLifespanForImplicitFlow"`
	SSOSessionIdleTimeout              int   `json:"ssoSessionIdleTimeout"`
	SSOSessionMaxLifespan              int   `json:"ssoSessionMaxLifespan"`
	OfflineSessionIdleTimeout          int   `json:"offlineSessionIdleTimeout"`
	OfflineSessionMaxLifespan          int   `json:"offlineSessionMaxLifespan"`
	RefreshTokenMaxReuse               int   `json:"refreshTokenMaxReuse"`
	MaxFailureWaitSeconds              int   `json:"maxFailureWaitSeconds"`
	FailureFactor                      int   `json:"failureFactor"`
	OTPPolicyDigits                    int   `json:"otpPolicyDigits"`
	OTPPolicyPeriod                    int   `json:"otpPolicyPeriod"`
	OTPPolicyLookAheadWindow           int   `json:"otpPolicyLookAheadWindow"`
	EventsExpiration                   int64 `json:"eventsExpiration"`
	Enabled                            bool  `json:"enabled"`
	BruteForceProtected                bool  `json:"bruteForceProtected"`
	PermanentLockout                   bool  `json:"permanentLockout"`
	InternationalizationEnabled        bool  `json:"internationalizationEnabled"`
	EventsEnabled                      bool  `json:"eventsEnabled"`
	AdminEventsEnabled                 bool  `json:"adminEventsEnabled"`
	AdminEventsDetails                 bool  `json:"adminEventsDetailsEnabled"`
}

// Component is Keycloak's plug-in config record. Key providers, LDAP
// federation and LDAP mappers are all components, which is why they travel in
// an ordinary export.
type Component struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	ProviderID    string                 `json:"providerId"`
	SubType       string                 `json:"subType"`
	SubComponents map[string][]Component `json:"subComponents"`
	Config        map[string][]string    `json:"config"`
}

// ConfigValue returns the first value of a component config key.
func (c Component) ConfigValue(key string) string {
	if v, ok := c.Config[key]; ok && len(v) > 0 {
		return v[0]
	}
	return ""
}

// HasConfig reports whether a config key carries a non-empty value.
func (c Component) HasConfig(key string) bool { return c.ConfigValue(key) != "" }

// Client is a client definition.
type Client struct {
	Attributes                   map[string]string `json:"attributes"`
	ID                           string            `json:"id"`
	ClientID                     string            `json:"clientId"`
	Name                         string            `json:"name"`
	Description                  string            `json:"description"`
	Protocol                     string            `json:"protocol"`
	Secret                       string            `json:"secret"`
	ClientAuthenticatorType      string            `json:"clientAuthenticatorType"`
	BaseURL                      string            `json:"baseUrl"`
	AdminURL                     string            `json:"adminUrl"`
	RootURL                      string            `json:"rootUrl"`
	RegistrationAccessToken      string            `json:"registrationAccessToken"`
	RedirectURIs                 []string          `json:"redirectUris"`
	WebOrigins                   []string          `json:"webOrigins"`
	AuthorizationSettings        json.RawMessage   `json:"authorizationSettings"`
	ProtocolMappers              []ProtocolMapper  `json:"protocolMappers"`
	DefaultClientScopes          []string          `json:"defaultClientScopes"`
	OptionalClientScopes         []string          `json:"optionalClientScopes"`
	Enabled                      bool              `json:"enabled"`
	PublicClient                 bool              `json:"publicClient"`
	BearerOnly                   bool              `json:"bearerOnly"`
	ServiceAccountsEnabled       bool              `json:"serviceAccountsEnabled"`
	AuthorizationServicesEnabled bool              `json:"authorizationServicesEnabled"`
}

// Confidential reports whether this client authenticates with a secret.
func (c Client) Confidential() bool { return !c.PublicClient && !c.BearerOnly }

// ClientScope is a realm-level client scope.
type ClientScope struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Protocol        string            `json:"protocol"`
	Attributes      map[string]string `json:"attributes"`
	ProtocolMappers []ProtocolMapper  `json:"protocolMappers"`
}

// ProtocolMapper maps claims onto tokens.
type ProtocolMapper struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Protocol       string            `json:"protocol"`
	ProtocolMapper string            `json:"protocolMapper"`
	Config         map[string]string `json:"config"`
}

// Roles is the realm's role model.
type Roles struct {
	Realm  []Role            `json:"realm"`
	Client map[string][]Role `json:"client"`
}

// Role is a realm or client role.
type Role struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Composite   bool                `json:"composite"`
	ClientRole  bool                `json:"clientRole"`
	ContainerID string              `json:"containerId"`
	Composites  *RoleComposites     `json:"composites"`
	Attributes  map[string][]string `json:"attributes"`
}

// RoleComposites is what a composite role includes.
type RoleComposites struct {
	Realm  []string            `json:"realm"`
	Client map[string][]string `json:"client"`
}

// Group is a group and its subtree.
type Group struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Path        string              `json:"path"`
	Attributes  map[string][]string `json:"attributes"`
	RealmRoles  []string            `json:"realmRoles"`
	ClientRoles map[string][]string `json:"clientRoles"`
	SubGroups   []Group             `json:"subGroups"`
}

// IdentityProvider is an IdP federation.
type IdentityProvider struct {
	Alias                     string            `json:"alias"`
	DisplayName               string            `json:"displayName"`
	ProviderID                string            `json:"providerId"`
	Enabled                   bool              `json:"enabled"`
	TrustEmail                bool              `json:"trustEmail"`
	StoreToken                bool              `json:"storeToken"`
	FirstBrokerLoginFlowAlias string            `json:"firstBrokerLoginFlowAlias"`
	Config                    map[string]string `json:"config"`
}

// IdentityProviderMapper maps IdP claims onto local users.
type IdentityProviderMapper struct {
	Name                   string            `json:"name"`
	IdentityProviderAlias  string            `json:"identityProviderAlias"`
	IdentityProviderMapper string            `json:"identityProviderMapper"`
	Config                 map[string]string `json:"config"`
}

// AuthenticationFlow is a flow and its executions.
type AuthenticationFlow struct {
	ID          string          `json:"id"`
	Alias       string          `json:"alias"`
	Description string          `json:"description"`
	ProviderID  string          `json:"providerId"`
	TopLevel    bool            `json:"topLevel"`
	BuiltIn     bool            `json:"builtIn"`
	Executions  []FlowExecution `json:"authenticationExecutions"`
}

// FlowExecution is one step of a flow.
type FlowExecution struct {
	Authenticator       string `json:"authenticator"`
	AuthenticatorConfig string `json:"authenticatorConfig"`
	FlowAlias           string `json:"flowAlias"`
	Requirement         string `json:"requirement"`
	Priority            int    `json:"priority"`
	AuthenticatorFlow   bool   `json:"authenticatorFlow"`
	UserSetupAllowed    bool   `json:"userSetupAllowed"`
}

// AuthenticatorConfig is a named authenticator configuration, which may carry
// a secret of its own.
type AuthenticatorConfig struct {
	ID     string            `json:"id"`
	Alias  string            `json:"alias"`
	Config map[string]string `json:"config"`
}

// RequiredAction is a realm-level required action.
type RequiredAction struct {
	Alias         string `json:"alias"`
	Name          string `json:"name"`
	ProviderID    string `json:"providerId"`
	Enabled       bool   `json:"enabled"`
	DefaultAction bool   `json:"defaultAction"`
	Priority      int    `json:"priority"`
}

// User is one account, as it appears in a realm or user file.
type User struct {
	ID                  string              `json:"id"`
	Username            string              `json:"username"`
	Email               string              `json:"email"`
	FirstName           string              `json:"firstName"`
	LastName            string              `json:"lastName"`
	Enabled             bool                `json:"enabled"`
	EmailVerified       bool                `json:"emailVerified"`
	CreatedTimestamp    int64               `json:"createdTimestamp"`
	Attributes          map[string][]string `json:"attributes"`
	Credentials         []Credential        `json:"credentials"`
	RequiredActions     []string            `json:"requiredActions"`
	RealmRoles          []string            `json:"realmRoles"`
	ClientRoles         map[string][]string `json:"clientRoles"`
	Groups              []string            `json:"groups"`
	FederatedIdentities []FederatedIdentity `json:"federatedIdentities"`
	// FederationLink names the user storage provider a user comes from, which
	// is how local and LDAP-federated accounts are told apart.
	FederationLink             string   `json:"federationLink"`
	ServiceAccountID           string   `json:"serviceAccountClientId"`
	DisableableCredentialTypes []string `json:"disableableCredentialTypes"`
}

// Credential is one credential a user holds.
//
// PortCloak reads its type and metadata and never its value: the whole point of
// the credential boundary is that presence can be answered without the tool
// becoming a second copy of the crown jewels.
type Credential struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	UserLabel      string `json:"userLabel"`
	CreatedDate    int64  `json:"createdDate"`
	SecretData     string `json:"secretData"`
	CredentialData string `json:"credentialData"`
	// The pre-Keycloak-8 shape, still seen in older exports.
	Algorithm         string `json:"algorithm"`
	HashIterations    int    `json:"hashIterations"`
	HashedSaltedValue string `json:"hashedSaltedValue"`
	Salt              string `json:"salt"`
	Counter           int    `json:"counter"`
	Digits            int    `json:"digits"`
	Period            int    `json:"period"`
}

// FederatedIdentity links a user to an IdP account.
type FederatedIdentity struct {
	IdentityProvider string `json:"identityProvider"`
	UserID           string `json:"userId"`
	UserName         string `json:"userName"`
}

// Credential type names Keycloak uses.
const (
	CredentialPassword     = "password"
	CredentialOTP          = "otp"
	CredentialTOTP         = "totp"
	CredentialHOTP         = "hotp"
	CredentialWebAuthn     = "webauthn"
	CredentialWebAuthnPwl  = "webauthn-passwordless"
	CredentialRecoveryCode = "recovery-authn-codes"
)

// Component provider-type keys, which are how key providers and user
// federation are found inside the components map.
const (
	ComponentKeyProvider         = "org.keycloak.keys.KeyProvider"
	ComponentUserStorageProvider = "org.keycloak.storage.UserStorageProvider"
	ComponentLDAPMapper          = "org.keycloak.storage.ldap.mappers.LDAPStorageMapper"
	ComponentClientRegistration  = "org.keycloak.services.clientregistration.policy.ClientRegistrationPolicy"
)
