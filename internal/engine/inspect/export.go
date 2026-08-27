package inspect

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"portcloak/internal/engine/inspect/index"
	"portcloak/internal/engine/manifest"
	"portcloak/internal/engine/obs"
	"portcloak/internal/engine/resil"
)

// ExportView names a table that can be exported.
type ExportView string

const (
	ExportUsers        ExportView = "users"
	ExportClients      ExportView = "clients"
	ExportSecretLedger ExportView = "secretLedger"
	ExportCompleteness ExportView = "completeness"
	ExportDependencies ExportView = "dependencies"
	ExportKeys         ExportView = "keys"
)

// ExportFormat is CSV or JSON.
type ExportFormat string

const (
	FormatCSV  ExportFormat = "csv"
	FormatJSON ExportFormat = "json"
)

// ExportRequest is what the operator asked to write out.
type ExportRequest struct {
	View   ExportView
	Format ExportFormat
	Path   string
	// Filter narrows a user export to the rows currently shown, so what lands
	// in the file is what was on screen.
	Filter index.UserFilter
}

// ExportResult describes what was written.
type ExportResult struct {
	Path  string `json:"path"`
	Rows  int    `json:"rows"`
	Bytes int64  `json:"bytes"`
	Note  string `json:"note"`
}

// Export writes a view to disk, redacted by the same rules as the UI.
//
// Exports carry presence, never values. That is what keeps evidence-gathering
// for a ticket from becoming an accidental secret-exfiltration path, and it is
// why the export is itself audited.
func (s *Session) Export(ctx context.Context, req ExportRequest, audit *obs.AuditLog) (ExportResult, error) {
	if req.Path == "" {
		return ExportResult{}, resil.Fatal("export", "No destination was given.", nil)
	}
	if err := os.MkdirAll(filepath.Dir(req.Path), 0o700); err != nil {
		return ExportResult{}, resil.Fatal("export",
			fmt.Sprintf("PortCloak could not create %s.", filepath.Dir(req.Path)), err)
	}
	f, err := os.OpenFile(req.Path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return ExportResult{}, resil.Fatal("export",
			fmt.Sprintf("PortCloak could not write to %s.", req.Path), err)
	}
	defer f.Close() //nolint:errcheck

	rows, err := s.writeExport(ctx, f, req)
	if err != nil {
		_ = os.Remove(req.Path)
		return ExportResult{}, err
	}
	if err := f.Sync(); err != nil {
		return ExportResult{}, err
	}
	info, _ := f.Stat()

	result := ExportResult{Path: req.Path, Rows: rows, Note: exportNote(req.View)}
	if info != nil {
		result.Bytes = info.Size()
	}

	if audit != nil {
		// Exporting creates a copy of directory data outside the tool, so it is
		// recorded like any other disclosure.
		_ = audit.Record(obs.AuditEntry{
			Action:     obs.ActionExportView,
			Outcome:    "exported",
			Realm:      s.Realm,
			SnapshotID: s.ID,
			Detail:     fmt.Sprintf("%s as %s · %d rows · redacted", req.View, req.Format, rows),
		})
	}
	return result, nil
}

func exportNote(v ExportView) string {
	switch v {
	case ExportUsers:
		return "Credential presence only — hashes, OTP seeds and passkey material are never written."
	case ExportSecretLedger:
		return "Locations and kinds only — no secret value is written."
	default:
		return "Redacted by the same rules as the screen."
	}
}

func (s *Session) writeExport(ctx context.Context, f *os.File, req ExportRequest) (int, error) {
	switch req.Format {
	case FormatJSON:
		return s.writeExportJSON(ctx, f, req)
	case FormatCSV, "":
		return s.writeExportCSV(ctx, f, req)
	default:
		return 0, resil.Fatal("export", fmt.Sprintf("%q is not a format PortCloak writes.", req.Format), nil)
	}
}

func (s *Session) writeExportJSON(ctx context.Context, f *os.File, req ExportRequest) (int, error) {
	var payload any
	rows := 0

	switch req.View {
	case ExportUsers:
		idx, err := s.Index(ctx, nil)
		if err != nil {
			return 0, err
		}
		var out []index.UserRow
		if err := idx.AllUserIDs(ctx, req.Filter, func(r index.UserRow) error {
			out = append(out, r)
			rows++
			return nil
		}); err != nil {
			return 0, err
		}
		payload = out
	case ExportClients:
		payload = s.Manifest.Clients
		rows = len(s.Manifest.Clients)
	case ExportSecretLedger:
		ledger := s.Ledger()
		payload = ledger
		rows = len(ledger)
	case ExportCompleteness:
		payload = s.Manifest.Completeness
		rows = len(s.Manifest.Completeness.Categories)
	case ExportDependencies:
		payload = s.Manifest.ExternalDependencies
		rows = len(s.Manifest.ExternalDependencies)
	case ExportKeys:
		payload = s.Manifest.Keys
		rows = len(s.Manifest.Keys)
	default:
		return 0, resil.Fatal("export", fmt.Sprintf("%q is not a view PortCloak exports.", req.View), nil)
	}

	b, err := marshalIndent(payload)
	if err != nil {
		return 0, err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return 0, err
	}
	return rows, nil
}

func (s *Session) writeExportCSV(ctx context.Context, f *os.File, req ExportRequest) (int, error) {
	w := csv.NewWriter(f)
	defer w.Flush()
	rows := 0

	switch req.View {
	case ExportUsers:
		if err := w.Write([]string{
			"username", "email", "firstName", "lastName", "enabled", "emailVerified",
			"origin", "hasPassword", "passwordAlgorithm", "passwordIterations",
			"otpEnrolments", "passkeys", "recoveryCodes", "secondFactor",
			"groups", "requiredActions",
		}); err != nil {
			return 0, err
		}
		idx, err := s.Index(ctx, nil)
		if err != nil {
			return 0, err
		}
		if err := idx.AllUserIDs(ctx, req.Filter, func(r index.UserRow) error {
			rows++
			return w.Write([]string{
				r.Username, r.Email, r.FirstName, r.LastName,
				strconv.FormatBool(r.Enabled), strconv.FormatBool(r.EmailVerified),
				r.Origin, strconv.FormatBool(r.HasPassword), r.PasswordAlgorithm,
				strconv.Itoa(r.PasswordIterations), strconv.Itoa(r.OTPCount),
				strconv.Itoa(r.WebAuthnCount), strconv.FormatBool(r.RecoveryCodes),
				r.SecondFactor, strings.Join(r.Groups, " "), strings.Join(r.RequiredActions, " "),
			})
		}); err != nil {
			return 0, err
		}

	case ExportClients:
		if err := w.Write([]string{"clientId", "name", "enabled", "protocol", "confidential", "secretPresent", "secretMasked", "mappers", "authorization"}); err != nil {
			return 0, err
		}
		for _, c := range s.Manifest.Clients {
			rows++
			if err := w.Write([]string{
				c.ClientID, c.Name, strconv.FormatBool(c.Enabled), c.Protocol,
				strconv.FormatBool(c.Confidential), strconv.FormatBool(c.SecretPresent),
				strconv.FormatBool(c.SecretMasked), strconv.Itoa(c.Mappers),
				strconv.FormatBool(c.Authorization),
			}); err != nil {
				return 0, err
			}
		}

	case ExportSecretLedger:
		// Types and locations only.
		if err := w.Write([]string{"location", "kind", "status", "carried", "masked", "note"}); err != nil {
			return 0, err
		}
		for _, e := range s.Ledger() {
			rows++
			if err := w.Write([]string{
				e.Location, e.KindLabel, e.Status,
				strconv.FormatBool(e.Carried), strconv.FormatBool(e.Masked), e.Note,
			}); err != nil {
				return 0, err
			}
		}

	case ExportCompleteness:
		if err := w.Write([]string{"category", "status", "count", "reason"}); err != nil {
			return 0, err
		}
		for _, c := range s.Manifest.Completeness.Categories {
			rows++
			if err := w.Write([]string{c.Name, string(c.Status), strconv.Itoa(c.Count), c.Reason}); err != nil {
				return 0, err
			}
		}

	case ExportDependencies:
		if err := w.Write([]string{"type", "name", "detectedAt", "referencedBy", "action", "consequence"}); err != nil {
			return 0, err
		}
		for _, d := range s.Manifest.ExternalDependencies {
			rows++
			if err := w.Write([]string{string(d.Type), d.Name, d.DetectedAt, d.ReferencedBy, d.Action, d.Consequence}); err != nil {
				return 0, err
			}
		}

	case ExportKeys:
		if err := w.Write([]string{"kid", "provider", "type", "algorithm", "use", "active", "privateCarried"}); err != nil {
			return 0, err
		}
		for _, k := range s.Manifest.Keys {
			rows++
			if err := w.Write([]string{
				k.KID, k.Provider, k.Type, k.Algorithm, k.Use,
				strconv.FormatBool(k.Active), strconv.FormatBool(k.PrivateCarried),
			}); err != nil {
				return 0, err
			}
		}

	default:
		return 0, resil.Fatal("export", fmt.Sprintf("%q is not a view PortCloak exports.", req.View), nil)
	}

	w.Flush()
	return rows, w.Error()
}

// VerifyReport is the result of checking a snapshot without restoring it.
type VerifyReport struct {
	SnapshotID  string             `json:"snapshotId"`
	Realm       string             `json:"realm"`
	OK          bool               `json:"ok"`
	Message     string             `json:"message"`
	Decryptable bool               `json:"decryptable"`
	Root        string             `json:"root"`
	Artifacts   []VerifiedArtifact `json:"artifacts"`
	// Note states plainly that nothing was contacted, because "is my backup
	// good" should be a routine, safe check rather than a restore drill.
	Note string `json:"note"`
}

// VerifiedArtifact is one artifact's result.
type VerifiedArtifact struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Digest string `json:"digest,omitempty"`
	Note   string `json:"note,omitempty"`
}

// VerifyReport recomputes the integrity tree for an open snapshot.
func (s *Session) VerifyReport() VerifyReport {
	r := VerifyReport{
		SnapshotID:  s.ID,
		Realm:       s.Realm,
		OK:          s.Verify.OK,
		Message:     s.Verify.Message,
		Decryptable: s.Verify.Decryptable,
		Root:        s.Verify.RootActual,
		Note:        "No environment was contacted. Verification reads the snapshot only.",
	}
	for _, a := range s.Verify.Artifacts {
		r.Artifacts = append(r.Artifacts, VerifiedArtifact{
			Name: a.Name, OK: a.OK, Digest: a.Actual, Note: a.Note,
		})
	}
	return r
}

// Dependencies is the external dependency view, shared by capture, inspection
// and the restore preconditions step — one set of records, three views.
func (s *Session) Dependencies() []manifest.Dependency { return s.Manifest.ExternalDependencies }
