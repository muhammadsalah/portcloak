// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package index

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	_ "modernc.org/sqlite" // pure-Go driver: no cgo, so cross-compiling for three platforms stays simple.

	"portcloak/internal/engine/realm"
)

// Options configure an index build.
type Options struct {
	// Path is where the index file goes. Empty means in memory, which is what
	// a small realm gets: nothing touches disk at all.
	Path string
	// Name identifies the snapshot this index belongs to. It is used only to
	// make an in-memory database's name recognisable in a stack trace; the
	// uniqueness that actually matters is added here, not supplied.
	Name string
	// Progress is called as users are indexed. On a large realm this takes real
	// time, and an unexplained wait is worse than a slow one.
	Progress func(indexed int)
	// ProgressEvery throttles the callback.
	ProgressEvery int
}

// Index is one snapshot's projection store.
type Index struct {
	db   *sql.DB
	path string

	mu     sync.Mutex
	closed bool
	counts Counts
}

// Counts is the population summary computed during the build.
type Counts struct {
	Users int `json:"users"`
}

// InMemoryThreshold is the user count below which an index stays in memory.
//
// Small realms never touch disk, which removes the residue question entirely
// for the common case.
const InMemoryThreshold = 5000

// inMemorySeq numbers in-memory databases so that two of them are two of them.
//
// SQLite's shared cache keys an in-memory database by *name*, and the name here
// was the constant `:memory:`. Every index in the process was therefore the same
// database: opening a second snapshot found the first one's schema already
// present and failed with "table users already exists". The failure was the
// lucky outcome — the alternative shape of this bug mixes two realms' users into
// one searchable table and answers questions about the wrong organisation.
//
// The cache has to stay shared: a private in-memory database lives only as long
// as the connection that made it, and database/sql may retire an idle
// connection underneath us. So the isolation comes from the name.
var inMemorySeq atomic.Uint64

// Open creates an empty index.
//
// One index is one snapshot, on disk or in memory. Nothing is shared between
// two of them.
func Open(opts Options) (*Index, error) {
	dsn := fmt.Sprintf("file:portcloak-index-%s-%d?mode=memory&cache=shared&_pragma=busy_timeout(5000)",
		safeName(opts.Name), inMemorySeq.Add(1))
	path := ""
	if opts.Path != "" {
		if err := os.MkdirAll(filepath.Dir(opts.Path), 0o700); err != nil {
			return nil, err
		}
		// The file is created with restricted permissions before the driver
		// touches it, so it is never briefly world-readable.
		f, err := os.OpenFile(opts.Path, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o600)
		if err != nil {
			return nil, fmt.Errorf("creating the inspection index: %w", err)
		}
		_ = f.Close()
		path = opts.Path
		dsn = "file:" + opts.Path + "?_pragma=busy_timeout(5000)&_pragma=secure_delete(ON)"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening the inspection index: %w", err)
	}
	// One connection: the index is single-writer during build and read-only
	// afterwards, and a shared in-memory database needs the connection kept.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("creating the index schema: %w", err)
	}
	return &Index{db: db, path: path}, nil
}

// Path is the index file, or empty when it lives in memory.
func (i *Index) Path() string { return i.path }

// Counts is the population summary.
func (i *Index) Counts() Counts {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.counts
}

// Close drops the index and securely deletes its file.
//
// Deleting the whole index directory at any moment must always be safe, so this
// is best-effort about everything except actually removing the file.
func (i *Index) Close() error {
	i.mu.Lock()
	if i.closed {
		i.mu.Unlock()
		return nil
	}
	i.closed = true
	i.mu.Unlock()

	err := i.db.Close()
	if i.path != "" {
		if rmErr := os.Remove(i.path); rmErr != nil && !os.IsNotExist(rmErr) && err == nil {
			err = rmErr
		}
		// SQLite may have left sidecars behind depending on journal mode.
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			_ = os.Remove(i.path + suffix)
		}
	}
	return err
}

// BuildInput is one user file to stream in.
type BuildInput struct {
	// Name is what the file is called inside the bundle, recorded so a single
	// user's full detail can be re-read from it on demand.
	Name string
	// Path is where it is on disk.
	Path string
}

// Build streams user files into the index, one account at a time.
//
// Bounded memory regardless of realm size is the whole requirement: reading a
// file into a slice first would defeat it before any of the rest mattered.
func (i *Index) Build(ctx context.Context, inputs []BuildInput, providers map[string]string, opts Options) error {
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // the commit below is what matters.

	insertUser, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO users (
			id, username, username_lc, email, email_lc, first_name, last_name, name_lc,
			enabled, email_verified, origin, has_password, password_algo, password_iterations,
			otp_count, webauthn_count, recovery_codes, second_factor, required_actions,
			created_at, service_account, source_file, source_index
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer insertUser.Close() //nolint:errcheck

	insertRealmRole, err := tx.PrepareContext(ctx, `INSERT INTO user_realm_roles (user_id, role) VALUES (?,?)`)
	if err != nil {
		return err
	}
	defer insertRealmRole.Close() //nolint:errcheck

	insertClientRole, err := tx.PrepareContext(ctx, `INSERT INTO user_client_roles (user_id, client, role) VALUES (?,?,?)`)
	if err != nil {
		return err
	}
	defer insertClientRole.Close() //nolint:errcheck

	insertGroup, err := tx.PrepareContext(ctx, `INSERT INTO user_groups (user_id, path) VALUES (?,?)`)
	if err != nil {
		return err
	}
	defer insertGroup.Close() //nolint:errcheck

	insertAction, err := tx.PrepareContext(ctx, `INSERT INTO user_required_actions (user_id, action) VALUES (?,?)`)
	if err != nil {
		return err
	}
	defer insertAction.Close() //nolint:errcheck

	insertIdentity, err := tx.PrepareContext(ctx, `INSERT INTO user_federated_identities (user_id, provider, username) VALUES (?,?,?)`)
	if err != nil {
		return err
	}
	defer insertIdentity.Close() //nolint:errcheck

	every := opts.ProgressEvery
	if every <= 0 {
		every = 500
	}

	total := 0
	for _, in := range inputs {
		ordinal := 0
		_, err := realm.StreamUsersFile(ctx, in.Path, func(u realm.User) error {
			id := u.ID
			if id == "" {
				id = fmt.Sprintf("%s#%d", in.Name, ordinal)
			}
			s := realm.Summarise(u)

			_, err := insertUser.ExecContext(ctx,
				id, u.Username, strings.ToLower(u.Username),
				nullable(u.Email), strings.ToLower(u.Email),
				nullable(u.FirstName), nullable(u.LastName),
				strings.ToLower(strings.TrimSpace(u.FirstName+" "+u.LastName)),
				boolInt(u.Enabled), boolInt(u.EmailVerified),
				realm.Origin(u, providers),
				boolInt(s.HasPassword), nullable(s.PasswordAlgorithm), s.PasswordIterations,
				s.OTPCount, s.WebAuthnCount, boolInt(s.RecoveryCodes), s.SecondFactor(),
				strings.Join(u.RequiredActions, ","),
				u.CreatedTimestamp, nullable(u.ServiceAccountID),
				in.Name, ordinal,
			)
			if err != nil {
				return err
			}
			for _, r := range u.RealmRoles {
				if _, err := insertRealmRole.ExecContext(ctx, id, r); err != nil {
					return err
				}
			}
			for client, roles := range u.ClientRoles {
				for _, r := range roles {
					if _, err := insertClientRole.ExecContext(ctx, id, client, r); err != nil {
						return err
					}
				}
			}
			for _, g := range u.Groups {
				if _, err := insertGroup.ExecContext(ctx, id, g); err != nil {
					return err
				}
			}
			for _, a := range u.RequiredActions {
				if _, err := insertAction.ExecContext(ctx, id, a); err != nil {
					return err
				}
			}
			for _, fi := range u.FederatedIdentities {
				if _, err := insertIdentity.ExecContext(ctx, id, fi.IdentityProvider, fi.UserName); err != nil {
					return err
				}
			}

			ordinal++
			total++
			if opts.Progress != nil && total%every == 0 {
				opts.Progress(total)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("indexing %s: %w", in.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	if opts.Progress != nil {
		opts.Progress(total)
	}

	i.mu.Lock()
	i.counts = Counts{Users: total}
	i.mu.Unlock()
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// safeName reduces a snapshot id to something that cannot be mistaken for part
// of the DSN. It is cosmetic — the sequence number is what makes the name unique
// — so an empty or unusable id is simply left out.
func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "anon"
	}
	return b.String()
}
