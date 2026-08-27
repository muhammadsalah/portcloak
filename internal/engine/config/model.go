package config

import (
	"fmt"
	"strings"
	"time"
)

// EnvironmentKind is where a Keycloak server actually runs.
type EnvironmentKind string

const (
	EnvLocal      EnvironmentKind = "local"
	EnvSSH        EnvironmentKind = "ssh"
	EnvDocker     EnvironmentKind = "docker"
	EnvKubernetes EnvironmentKind = "kubernetes"
)

// EnvironmentKinds is the full set, in the order the UI offers them.
var EnvironmentKinds = []EnvironmentKind{EnvLocal, EnvSSH, EnvDocker, EnvKubernetes}

// StorageKind is where snapshot bundles live.
type StorageKind string

const (
	StoreDisk  StorageKind = "disk"
	StoreSSH   StorageKind = "ssh"
	StoreS3    StorageKind = "s3"
	StoreAzure StorageKind = "azure"
)

// StorageKinds is the full set, in the order the UI offers them.
var StorageKinds = []StorageKind{StoreDisk, StoreSSH, StoreS3, StoreAzure}

// SSHAuth is how PortCloak authenticates to an SSH host.
type SSHAuth string

const (
	SSHKey      SSHAuth = "key"
	SSHAgent    SSHAuth = "agent"
	SSHPassword SSHAuth = "password"
)

// Config is the whole of ~/.portcloak/config.yaml.
//
// Extra catches keys this build does not recognise and re-emits them on save,
// so a file written by a newer PortCloak and opened by an older one does not
// quietly lose entries (UC-O7 E2).
type Config struct {
	Version      int            `yaml:"version" json:"version"`
	Preferences  Preferences    `yaml:"preferences" json:"preferences"`
	Environments []Environment  `yaml:"environments" json:"environments"`
	Storage      []Storage      `yaml:"storage" json:"storage"`
	Extra        map[string]any `yaml:",inline" json:"-"`
}

// Preferences are the tool-wide settings.
type Preferences struct {
	// UsersMode is the kc.sh export users mode; different_files is the default
	// because it is what makes a very large realm survivable.
	UsersMode string `yaml:"usersMode,omitempty" json:"usersMode,omitempty"`
	// UsersPerFile bounds each user file.
	UsersPerFile int `yaml:"usersPerFile,omitempty" json:"usersPerFile,omitempty"`
	// VerifyByDefault turns on the optional Admin API pass for new captures.
	VerifyByDefault *bool `yaml:"verifyByDefault,omitempty" json:"verifyByDefault,omitempty"`
	// EncryptByDefault presents the capture wizard's encryption toggle as on.
	// Encryption is opt-in (D8) — this governs the presented default, and
	// declining it always remains a deliberate, respected choice.
	EncryptByDefault *bool `yaml:"encryptByDefault,omitempty" json:"encryptByDefault,omitempty"`
	// AllowSecretReveal lets an operator rule out secret extraction entirely
	// while keeping inspection available (UC-I9 A1).
	AllowSecretReveal *bool `yaml:"allowSecretReveal,omitempty" json:"allowSecretReveal,omitempty"`
	// ProbeStaleAfter is how long a probe result stays believable. A cached
	// "reachable" from three weeks ago is worse than no information.
	ProbeStaleAfter time.Duration `yaml:"probeStaleAfter,omitempty" json:"probeStaleAfter,omitempty"`
	// RetryMaxAttempts and RetryBaseDelay tune the resilience layer.
	RetryMaxAttempts int           `yaml:"retryMaxAttempts,omitempty" json:"retryMaxAttempts,omitempty"`
	RetryBaseDelay   time.Duration `yaml:"retryBaseDelay,omitempty" json:"retryBaseDelay,omitempty"`
	RetryMaxDelay    time.Duration `yaml:"retryMaxDelay,omitempty" json:"retryMaxDelay,omitempty"`
	// BreakerThreshold is how many consecutive failures open a circuit.
	BreakerThreshold int           `yaml:"breakerThreshold,omitempty" json:"breakerThreshold,omitempty"`
	BreakerCooldown  time.Duration `yaml:"breakerCooldown,omitempty" json:"breakerCooldown,omitempty"`

	Extra map[string]any `yaml:",inline" json:"-"`
}

// DefaultPreferences are the values used where the file says nothing. They live
// here rather than scattered at use sites so an operator reading this struct
// knows what an empty `preferences: {}` actually means.
func DefaultPreferences() Preferences {
	t := true
	return Preferences{
		UsersMode:         "different_files",
		UsersPerFile:      1000,
		VerifyByDefault:   &t,
		EncryptByDefault:  &t,
		AllowSecretReveal: &t,
		ProbeStaleAfter:   7 * 24 * time.Hour,
		RetryMaxAttempts:  5,
		RetryBaseDelay:    500 * time.Millisecond,
		RetryMaxDelay:     30 * time.Second,
		BreakerThreshold:  5,
		BreakerCooldown:   30 * time.Second,
	}
}

// Resolved fills in every unset preference from the defaults.
func (p Preferences) Resolved() Preferences {
	d := DefaultPreferences()
	out := p
	if out.UsersMode == "" {
		out.UsersMode = d.UsersMode
	}
	if out.UsersPerFile <= 0 {
		out.UsersPerFile = d.UsersPerFile
	}
	if out.VerifyByDefault == nil {
		out.VerifyByDefault = d.VerifyByDefault
	}
	if out.EncryptByDefault == nil {
		out.EncryptByDefault = d.EncryptByDefault
	}
	if out.AllowSecretReveal == nil {
		out.AllowSecretReveal = d.AllowSecretReveal
	}
	if out.ProbeStaleAfter <= 0 {
		out.ProbeStaleAfter = d.ProbeStaleAfter
	}
	if out.RetryMaxAttempts <= 0 {
		out.RetryMaxAttempts = d.RetryMaxAttempts
	}
	if out.RetryBaseDelay <= 0 {
		out.RetryBaseDelay = d.RetryBaseDelay
	}
	if out.RetryMaxDelay <= 0 {
		out.RetryMaxDelay = d.RetryMaxDelay
	}
	if out.BreakerThreshold <= 0 {
		out.BreakerThreshold = d.BreakerThreshold
	}
	if out.BreakerCooldown <= 0 {
		out.BreakerCooldown = d.BreakerCooldown
	}
	return out
}

// SSHHop is a jump/bastion host in the connection chain.
type SSHHop struct {
	Host          string  `yaml:"host" json:"host"`
	Port          int     `yaml:"port,omitempty" json:"port,omitempty"`
	User          string  `yaml:"user,omitempty" json:"user,omitempty"`
	Auth          SSHAuth `yaml:"auth,omitempty" json:"auth,omitempty"`
	CredentialRef string  `yaml:"credentialRef,omitempty" json:"credentialRef,omitempty"`

	Extra map[string]any `yaml:",inline" json:"-"`
}

// ResourcePreset overrides what an ephemeral clone requests, instead of
// inheriting the serving workload's own requests (UC-E4 A4).
type ResourcePreset struct {
	CPU    string `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	Memory string `yaml:"memory,omitempty" json:"memory,omitempty"`
}

// ProbeStamp records the last Test result so the list can show whether an
// environment is usable, and how long ago that was established.
type ProbeStamp struct {
	At              time.Time `yaml:"at" json:"at"`
	OK              bool      `yaml:"ok" json:"ok"`
	Summary         string    `yaml:"summary,omitempty" json:"summary,omitempty"`
	KeycloakVersion string    `yaml:"keycloakVersion,omitempty" json:"keycloakVersion,omitempty"`
	CloneCapable    bool      `yaml:"cloneCapable,omitempty" json:"cloneCapable,omitempty"`
	// Writable distinguishes "reachable, read-only" from "reachable and
	// writable" for a storage probe. Read-only is a legitimate configuration
	// for browsing, not a failure.
	Writable *bool `yaml:"writable,omitempty" json:"writable,omitempty"`
}

// Stale reports whether the stamp is old enough that believing it would be
// worse than having no information.
func (p *ProbeStamp) Stale(after time.Duration, now time.Time) bool {
	if p == nil {
		return false
	}
	return now.Sub(p.At) > after
}

// Environment is one place a Keycloak server runs.
type Environment struct {
	Name string          `yaml:"name" json:"name"`
	Kind EnvironmentKind `yaml:"kind" json:"kind"`

	// Local and SSH: the install root containing bin/kc.sh.
	ServerFolder string `yaml:"serverFolder,omitempty" json:"serverFolder,omitempty"`
	JavaHome     string `yaml:"javaHome,omitempty" json:"javaHome,omitempty"`

	// SSH.
	Host     string  `yaml:"host,omitempty" json:"host,omitempty"`
	Port     int     `yaml:"port,omitempty" json:"port,omitempty"`
	User     string  `yaml:"user,omitempty" json:"user,omitempty"`
	Auth     SSHAuth `yaml:"auth,omitempty" json:"auth,omitempty"`
	JumpHost *SSHHop `yaml:"jumpHost,omitempty" json:"jumpHost,omitempty"`
	Sudo     bool    `yaml:"sudo,omitempty" json:"sudo,omitempty"`

	// Docker.
	DockerEndpoint string `yaml:"dockerEndpoint,omitempty" json:"dockerEndpoint,omitempty"`
	Container      string `yaml:"container,omitempty" json:"container,omitempty"`
	Runtime        string `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Network        string `yaml:"network,omitempty" json:"network,omitempty"`

	// Kubernetes / OpenShift.
	Context        string          `yaml:"context,omitempty" json:"context,omitempty"`
	Kubeconfig     string          `yaml:"kubeconfig,omitempty" json:"kubeconfig,omitempty"`
	Namespace      string          `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Workload       string          `yaml:"workload,omitempty" json:"workload,omitempty"`
	ContainerName  string          `yaml:"containerName,omitempty" json:"containerName,omitempty"`
	ResourcePreset *ResourcePreset `yaml:"resourcePreset,omitempty" json:"resourcePreset,omitempty"`

	// Admin REST API — optional, and only ever used to verify what the export
	// produced and to detect external dependencies.
	AdminBaseURL       string `yaml:"adminBaseUrl,omitempty" json:"adminBaseUrl,omitempty"`
	AdminRealm         string `yaml:"adminRealm,omitempty" json:"adminRealm,omitempty"`
	AdminUser          string `yaml:"adminUser,omitempty" json:"adminUser,omitempty"`
	AdminClientID      string `yaml:"adminClientId,omitempty" json:"adminClientId,omitempty"`
	AdminCredentialRef string `yaml:"adminCredentialRef,omitempty" json:"adminCredentialRef,omitempty"`
	AdminInsecureTLS   bool   `yaml:"adminInsecureTls,omitempty" json:"adminInsecureTls,omitempty"`

	CredentialRef string      `yaml:"credentialRef,omitempty" json:"credentialRef,omitempty"`
	LastProbe     *ProbeStamp `yaml:"lastProbe,omitempty" json:"lastProbe,omitempty"`

	Extra map[string]any `yaml:",inline" json:"-"`
}

// Target renders what this environment points at, for the one-line summary in
// the environments list (UC-E9).
func (e Environment) Target() string {
	switch e.Kind {
	case EnvLocal:
		return e.ServerFolder
	case EnvSSH:
		return fmt.Sprintf("%s@%s:%s", e.User, e.Host, e.ServerFolder)
	case EnvDocker:
		return e.Container
	case EnvKubernetes:
		return e.Namespace + "/" + e.Workload
	default:
		return ""
	}
}

// Storage is one place snapshot bundles live.
type Storage struct {
	Name    string      `yaml:"name" json:"name"`
	Kind    StorageKind `yaml:"kind" json:"kind"`
	Default bool        `yaml:"default,omitempty" json:"default,omitempty"`
	// EncryptionRequired removes the opt-out for anything written here.
	EncryptionRequired bool `yaml:"encryptionRequired,omitempty" json:"encryptionRequired,omitempty"`

	// Disk and SSH: the folder everything is rooted at.
	Folder string `yaml:"folder,omitempty" json:"folder,omitempty"`

	// SSH.
	Host     string  `yaml:"host,omitempty" json:"host,omitempty"`
	Port     int     `yaml:"port,omitempty" json:"port,omitempty"`
	User     string  `yaml:"user,omitempty" json:"user,omitempty"`
	Auth     SSHAuth `yaml:"auth,omitempty" json:"auth,omitempty"`
	JumpHost *SSHHop `yaml:"jumpHost,omitempty" json:"jumpHost,omitempty"`

	// S3-compatible.
	Endpoint      string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	Region        string `yaml:"region,omitempty" json:"region,omitempty"`
	Bucket        string `yaml:"bucket,omitempty" json:"bucket,omitempty"`
	PathStyle     bool   `yaml:"pathStyle,omitempty" json:"pathStyle,omitempty"`
	PartSizeMB    int    `yaml:"partSizeMb,omitempty" json:"partSizeMb,omitempty"`
	StorageClass  string `yaml:"storageClass,omitempty" json:"storageClass,omitempty"`
	ServerSideEnc string `yaml:"serverSideEncryption,omitempty" json:"serverSideEncryption,omitempty"`

	// Azure Blob / Azurite.
	Account     string `yaml:"account,omitempty" json:"account,omitempty"`
	Container   string `yaml:"container,omitempty" json:"container,omitempty"`
	BlockSizeMB int    `yaml:"blockSizeMb,omitempty" json:"blockSizeMb,omitempty"`
	AccessTier  string `yaml:"accessTier,omitempty" json:"accessTier,omitempty"`

	// Prefix is the folder within a bucket or container, so one backend can
	// hold several independent snapshot trees.
	Prefix string `yaml:"prefix,omitempty" json:"prefix,omitempty"`

	CredentialRef string      `yaml:"credentialRef,omitempty" json:"credentialRef,omitempty"`
	LastProbe     *ProbeStamp `yaml:"lastProbe,omitempty" json:"lastProbe,omitempty"`

	Extra map[string]any `yaml:",inline" json:"-"`
}

// Root renders what this storage points at, for the storage list.
func (s Storage) Root() string {
	switch s.Kind {
	case StoreDisk:
		return s.Folder
	case StoreSSH:
		return fmt.Sprintf("%s@%s:%s", s.User, s.Host, s.Folder)
	case StoreS3:
		return strings.TrimSuffix(s.Bucket+"/"+s.Prefix, "/")
	case StoreAzure:
		return strings.TrimSuffix(s.Container+"/"+s.Prefix, "/")
	default:
		return ""
	}
}

// Environment finds an environment by name.
func (c Config) Environment(name string) (Environment, bool) {
	for _, e := range c.Environments {
		if e.Name == name {
			return e, true
		}
	}
	return Environment{}, false
}

// StorageByName finds a storage definition by name.
func (c Config) StorageByName(name string) (Storage, bool) {
	for _, s := range c.Storage {
		if s.Name == name {
			return s, true
		}
	}
	return Storage{}, false
}

// DefaultStorage returns the storage marked default, if there is one.
func (c Config) DefaultStorage() (Storage, bool) {
	for _, s := range c.Storage {
		if s.Default {
			return s, true
		}
	}
	return Storage{}, false
}
