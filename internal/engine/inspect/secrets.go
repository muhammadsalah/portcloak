package inspect

import (
	"context"
	"fmt"
	"strings"

	"portcloak/internal/engine/manifest"
	"portcloak/internal/engine/obs"
	"portcloak/internal/engine/realm"
	"portcloak/internal/engine/resil"
)

// LedgerEntry is one row of the secret ledger view.
//
// It carries no value, which is what makes the ledger safe to read, screenshot
// and export wholesale.
type LedgerEntry struct {
	Location  string `json:"location"`
	Kind      string `json:"kind"`
	KindLabel string `json:"kindLabel"`
	Carried   bool   `json:"carried"`
	Masked    bool   `json:"masked"`
	Status    string `json:"status"`
	Note      string `json:"note,omitempty"`
	Algorithm string `json:"algorithm,omitempty"`
	// Revealable is false for an entry with nothing behind it — a secret the
	// source masked has no value to show, and offering Reveal on it would
	// promise something that cannot be delivered.
	Revealable bool `json:"revealable"`
}

// Ledger renders the secret ledger for the inspector.
func (s *Session) Ledger() []LedgerEntry {
	out := make([]LedgerEntry, 0, len(s.Manifest.Secrets))
	for _, sec := range s.Manifest.Secrets {
		e := LedgerEntry{
			Location:   sec.Location,
			Kind:       string(sec.Kind),
			KindLabel:  manifest.KindLabel(sec.Kind),
			Carried:    sec.Carried,
			Masked:     sec.Masked,
			Note:       sec.Note,
			Algorithm:  sec.Algorithm,
			Revealable: sec.Carried && !sec.Masked,
		}
		switch {
		case sec.Masked:
			e.Status = "Masked at source · partial"
		case sec.Carried:
			e.Status = "Carried"
		default:
			e.Status = "Not carried"
		}
		out = append(out, e)
	}
	return out
}

// LedgerSummary is the footer line under the ledger.
func (s *Session) LedgerSummary() string {
	total, carried, masked := s.Manifest.SecretCounts()
	parts := []string{fmt.Sprintf("%d secret%s", total, plural(total)), fmt.Sprintf("%d carried", carried)}
	if masked > 0 {
		parts = append(parts, fmt.Sprintf("%d masked at source", masked))
	}
	return strings.Join(parts, " · ")
}

// RevealRequest is one deliberate, audited disclosure.
type RevealRequest struct {
	Location string
	// Reason is optional and recorded, because the question a ledger answers
	// six months later is usually "why".
	Reason string
}

// Reveal decrypts one secret and returns its value.
//
// It is one secret at a time, on an explicit action, and it writes an audit
// entry naming what was revealed and when. The value never reaches a log, a
// progress event or an export.
func (s *Session) Reveal(ctx context.Context, req RevealRequest, audit *obs.AuditLog, allowed bool) (string, error) {
	if !allowed {
		// Reveal can be switched off in preferences, so a snapshot can be
		// inspected with secret extraction ruled out entirely.
		return "", resil.Fatal("reveal a secret",
			"Revealing secrets is switched off in preferences.", nil).
			WithAdvice("Turn it back on in Preferences if you need it, or inspect without it.")
	}

	var entry manifest.Secret
	found := false
	for _, sec := range s.Manifest.Secrets {
		if sec.Location == req.Location {
			entry, found = sec, true
			break
		}
	}
	if !found {
		return "", resil.Fatal("reveal a secret",
			fmt.Sprintf("This snapshot's ledger has no entry at %s.", req.Location), nil)
	}
	if entry.Masked {
		return "", resil.Fatal("reveal a secret",
			fmt.Sprintf("%s was exported masked, so there is no real value in this snapshot to show.", req.Location), nil).
			WithAdvice("Set it by hand at the destination after importing.")
	}
	if !entry.Carried {
		return "", resil.Fatal("reveal a secret",
			fmt.Sprintf("%s was not carried in this snapshot.", req.Location), nil)
	}

	rep, err := s.Representation()
	if err != nil {
		return "", err
	}
	value, ok := lookupSecret(rep, req.Location)
	if !ok || value == "" {
		return "", resil.Fatal("reveal a secret",
			fmt.Sprintf("PortCloak could not find a value at %s inside this snapshot.", req.Location), nil)
	}

	// The audit entry is written before the value is returned, so a reveal is
	// recorded even if the caller never renders it.
	if audit != nil {
		outcome := "revealed"
		detail := fmt.Sprintf("revealed %s for %s", manifest.KindLabel(entry.Kind), req.Location)
		if !s.Envelope.Encryption.Enabled {
			// Saying so avoids implying the reveal added protection that was
			// never there.
			outcome = "revealed from an unencrypted snapshot"
		}
		if err := audit.Record(obs.AuditEntry{
			Action:     obs.ActionSecretReveal,
			Outcome:    outcome,
			Realm:      s.Realm,
			SnapshotID: s.ID,
			Storage:    s.Storage,
			Detail:     detail,
			Reason:     req.Reason,
		}); err != nil {
			return "", err
		}
	}
	return value, nil
}

// RevealNote is the sentence shown beside a revealed value.
func (s *Session) RevealNote() string {
	if s.Envelope.Encryption.Enabled {
		return "Recorded in the audit log. The value itself was not written anywhere."
	}
	return "This snapshot is not encrypted, so this value was already in the clear inside it. The reveal was still recorded."
}

// lookupSecret resolves a ledger location back to its value inside the realm
// representation.
//
// The ledger's location strings are produced by the manifest builder, so this
// is the inverse of that mapping rather than a general path evaluator — which
// keeps the set of things that can be read deliberately small.
func lookupSecret(rep *realm.Representation, location string) (string, bool) {
	switch {
	case strings.HasPrefix(location, "clients["):
		name, field, ok := bracketed(location, "clients[")
		if !ok {
			return "", false
		}
		for _, c := range rep.Clients {
			if c.ClientID != name {
				continue
			}
			switch field {
			case "secret":
				return c.Secret, true
			case "registrationAccessToken":
				return c.RegistrationAccessToken, true
			}
		}

	case strings.HasPrefix(location, "components["):
		ref, field, ok := bracketed(location, "components[")
		if !ok {
			return "", false
		}
		_, name, hasSlash := strings.Cut(ref, "/")
		if !hasSlash {
			name = ref
		}
		key := strings.TrimPrefix(field, "config.")
		for _, group := range rep.Components {
			for _, c := range group {
				if c.Name == name {
					if v := c.ConfigValue(key); v != "" {
						return v, true
					}
				}
			}
		}

	case strings.HasPrefix(location, "identityProviders["):
		alias, field, ok := bracketed(location, "identityProviders[")
		if !ok {
			return "", false
		}
		key := strings.TrimPrefix(field, "config.")
		for _, idp := range rep.IdentityProviders {
			if idp.Alias == alias {
				return idp.Config[key], true
			}
		}

	case location == "smtpServer.password":
		return rep.SMTPServer["password"], true

	case strings.HasPrefix(location, "authenticatorConfig["):
		alias, field, ok := bracketed(location, "authenticatorConfig[")
		if !ok {
			return "", false
		}
		key := strings.TrimPrefix(field, "config.")
		for _, cfg := range rep.AuthenticatorConfig {
			if cfg.Alias == alias {
				return cfg.Config[key], true
			}
		}

	case strings.HasPrefix(location, "attributes."):
		return rep.Attributes[strings.TrimPrefix(location, "attributes.")], true
	}
	return "", false
}

// bracketed splits `prefix[name].field` into its parts.
func bracketed(location, prefix string) (name, field string, ok bool) {
	rest := strings.TrimPrefix(location, prefix)
	name, after, ok := strings.Cut(rest, "]")
	if !ok {
		return "", "", false
	}
	return name, strings.TrimPrefix(after, "."), true
}
