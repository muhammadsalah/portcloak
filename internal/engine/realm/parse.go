package realm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Load reads a realm file.
//
// A realm file is bounded by the realm's configuration rather than by its user
// count — in the default export mode the users live elsewhere — so this one is
// safe to read whole.
func Load(path string) (*Representation, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-only.

	var rep Representation
	dec := json.NewDecoder(f)
	dec.UseNumber()
	if err := dec.Decode(&rep); err != nil {
		return nil, fmt.Errorf("reading the realm file %s: %w", path, err)
	}
	return &rep, nil
}

// StreamUsers parses a users file one account at a time.
//
// Bounded memory regardless of realm size is the whole requirement: a realm
// with 120,000 users has to be indexable on an ordinary laptop, and reading the
// file into a slice first would defeat that before any of the rest of the
// design mattered.
//
// It accepts both shapes an export produces: a wrapper object with a "users"
// array, and a bare array.
func StreamUsers(ctx context.Context, r io.Reader, fn func(User) error) (int, error) {
	dec := json.NewDecoder(r)
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil {
		if err == io.EOF {
			return 0, nil
		}
		return 0, fmt.Errorf("reading the users file: %w", err)
	}

	switch delim, _ := tok.(json.Delim); delim {
	case '[':
		return streamArray(ctx, dec, fn)
	case '{':
		// Walk to the "users" key, skipping anything else the wrapper holds.
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return 0, err
			}
			key, _ := keyTok.(string)
			if key != "users" {
				var skip json.RawMessage
				if err := dec.Decode(&skip); err != nil {
					return 0, err
				}
				continue
			}
			openTok, err := dec.Token()
			if err != nil {
				return 0, err
			}
			if d, _ := openTok.(json.Delim); d != '[' {
				return 0, fmt.Errorf(`the "users" field in this file is not an array`)
			}
			return streamArray(ctx, dec, fn)
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("this does not look like a Keycloak users file")
	}
}

func streamArray(ctx context.Context, dec *json.Decoder, fn func(User) error) (int, error) {
	n := 0
	for dec.More() {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		var u User
		if err := dec.Decode(&u); err != nil {
			return n, fmt.Errorf("reading user %d: %w", n+1, err)
		}
		if err := fn(u); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// StreamUsersFile opens a users file and streams it.
func StreamUsersFile(ctx context.Context, path string, fn func(User) error) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close() //nolint:errcheck // read-only.
	return StreamUsers(ctx, f, fn)
}

// CredentialSummary is what PortCloak records about a user's credentials:
// presence and metadata, never a value. There is nowhere in this struct to put
// a hash, an OTP seed or passkey material, and that is the point.
type CredentialSummary struct {
	HasPassword        bool   `json:"hasPassword"`
	PasswordAlgorithm  string `json:"passwordAlgorithm,omitempty"`
	PasswordIterations int    `json:"passwordIterations,omitempty"`
	OTPCount           int    `json:"otpCount"`
	WebAuthnCount      int    `json:"webauthnCount"`
	RecoveryCodes      bool   `json:"recoveryCodes"`
}

// SecondFactor renders the 2FA state as the facet the inspector shows.
func (c CredentialSummary) SecondFactor() string {
	switch {
	case c.OTPCount > 0 && c.WebAuthnCount > 0:
		return "both"
	case c.WebAuthnCount > 0:
		return "passkey"
	case c.OTPCount > 0:
		return "otp"
	default:
		return "none"
	}
}

// Summarise reads a user's credentials for presence and metadata only.
func Summarise(u User) CredentialSummary {
	var s CredentialSummary
	for _, c := range u.Credentials {
		switch strings.ToLower(c.Type) {
		case CredentialPassword:
			s.HasPassword = true
			algo, iterations := passwordMetadata(c)
			if algo != "" {
				s.PasswordAlgorithm = algo
			}
			if iterations > 0 {
				s.PasswordIterations = iterations
			}
		case CredentialOTP, CredentialTOTP, CredentialHOTP:
			s.OTPCount++
		case CredentialWebAuthn, CredentialWebAuthnPwl:
			s.WebAuthnCount++
		case CredentialRecoveryCode:
			s.RecoveryCodes = true
		}
	}
	return s
}

// passwordMetadata reads the algorithm and iteration count out of a password
// credential, across the two shapes Keycloak has used.
//
// Only the metadata is touched. credentialData is parsed for its algorithm
// field; secretData, which holds the hash and salt, is never opened.
func passwordMetadata(c Credential) (algorithm string, iterations int) {
	if c.Algorithm != "" {
		return c.Algorithm, c.HashIterations
	}
	if c.CredentialData == "" {
		return "", 0
	}
	var data struct {
		Algorithm      string      `json:"algorithm"`
		HashIterations json.Number `json:"hashIterations"`
	}
	if err := json.Unmarshal([]byte(c.CredentialData), &data); err != nil {
		return "", 0
	}
	n, _ := strconv.Atoi(data.HashIterations.String())
	return data.Algorithm, n
}

// Origin says whether a user lives locally or comes from a federation
// provider. It matters because federated users are not duplicated into the
// export and need their directory reachable at the destination.
func Origin(u User, providerNames map[string]string) string {
	if u.FederationLink == "" {
		if u.ServiceAccountID != "" {
			return "service account"
		}
		return "local"
	}
	if name, ok := providerNames[u.FederationLink]; ok && name != "" {
		return name
	}
	return "federated"
}

// FlattenGroups walks a group tree into a flat list of paths, which is the
// shape both the manifest counts and the inspector's facets need.
func FlattenGroups(groups []Group) []Group {
	var out []Group
	var walk func(prefix string, gs []Group)
	walk = func(prefix string, gs []Group) {
		for _, g := range gs {
			path := g.Path
			if path == "" {
				path = prefix + "/" + g.Name
			}
			flat := g
			flat.Path = path
			flat.SubGroups = nil
			out = append(out, flat)
			walk(path, g.SubGroups)
		}
	}
	walk("", groups)
	return out
}
