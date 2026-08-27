package config

import "fmt"

// Readiness is the separate question from validity.
//
// Validate answers "is this file structurally sound", and a file that fails it
// is refused outright — silently dropping half of it would lose an operator's
// work. Readiness answers "can this entry actually be used yet", which is a
// softer thing: a half-finished entry saves happily and shows as not ready,
// because refusing to save an entry until it is complete is how an operator
// loses the four fields they had already typed (UC-E1 A2, UC-E7).
type Readiness struct {
	Ready  bool   `json:"ready"`
	Reason string `json:"reason,omitempty"`
}

// EnvironmentReadiness reports whether an environment has everything a capture
// would need from its definition. It does not touch the network — that is what
// Probe is for.
//
// The bar is narrow on purpose: a field belongs here only if Validate lets it
// be empty *and* the adapter has no usable default for it. That is true of an
// SSH private key and of an S3 access key, and it is not true of a Docker
// endpoint or a kubeconfig context — both of which are blank in most working
// configurations, because blank is how you say "the local socket" and "whatever
// kubectl is pointed at".
//
// Both were on this list once, and the result was a Docker environment that
// captured perfectly while the screen above it said it was not usable yet. The
// Docker suggestion was worse than merely wrong: it offered `runtime` as the
// way to become ready, and nothing in the engine reads that field, so taking
// the advice would have cleared the banner and changed nothing.
func EnvironmentReadiness(e Environment) Readiness {
	switch e.Kind {
	case EnvSSH:
		switch e.Auth {
		case "":
			return notReady("choose how PortCloak should authenticate to %s — a private key, the SSH agent, or a password.", e.Host)
		case SSHKey, SSHPassword:
			if e.CredentialRef == "" {
				return notReady("the %s for %s has not been supplied yet, so there is nothing in this machine's keychain to connect with.", authNoun(e.Auth), e.Host)
			}
		}
		if e.JumpHost != nil {
			switch e.JumpHost.Auth {
			case SSHKey, SSHPassword:
				if e.JumpHost.CredentialRef == "" {
					return notReady("the jump host %s still needs its %s.", e.JumpHost.Host, authNoun(e.JumpHost.Auth))
				}
			}
		}
	}
	return Readiness{Ready: true}
}

// StorageReadiness reports whether a storage definition has everything needed
// to reach it.
func StorageReadiness(s Storage) Readiness {
	switch s.Kind {
	case StoreSSH:
		switch s.Auth {
		case "":
			return notReady("choose how PortCloak should authenticate to %s.", s.Host)
		case SSHKey, SSHPassword:
			if s.CredentialRef == "" {
				return notReady("the %s for %s has not been supplied yet.", authNoun(s.Auth), s.Host)
			}
		}
	case StoreS3:
		if s.CredentialRef == "" {
			return notReady("the access key for %s has not been supplied yet.", s.Bucket)
		}
	case StoreAzure:
		if s.CredentialRef == "" {
			return notReady("the connection string, key or SAS for %s has not been supplied yet.", s.Container)
		}
	}
	return Readiness{Ready: true}
}

func notReady(format string, args ...any) Readiness {
	return Readiness{Ready: false, Reason: fmt.Sprintf(format, args...)}
}

func authNoun(a SSHAuth) string {
	switch a {
	case SSHKey:
		return "private key"
	case SSHPassword:
		return "password"
	default:
		return "credential"
	}
}
