// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// JobKind is what a job is doing.
type JobKind string

const (
	JobCapture JobKind = "capture"
	JobRestore JobKind = "restore"
	JobIndex   JobKind = "index"
)

// JobState is where a job has got to.
type JobState string

const (
	JobQueued      JobState = "queued"
	JobRunning     JobState = "running"
	JobInterrupted JobState = "interrupted"
	JobCompleted   JobState = "completed"
	JobFailed      JobState = "failed"
	JobCancelled   JobState = "cancelled"
)

// Terminal reports whether a job will not move again on its own.
func (s JobState) Terminal() bool {
	return s == JobCompleted || s == JobFailed || s == JobCancelled
}

// Resumable reports whether the Activity screen should offer a Resume.
func (s JobState) Resumable() bool { return s == JobInterrupted }

// LedgerEntry is one line of the partial-failure ledger. It is what turns "it
// failed" into "user file batch 7 of 40 failed after 5 attempts (connection
// reset); resumable".
type LedgerEntry struct {
	Phase     string    `json:"phase"`
	Item      string    `json:"item,omitempty"`
	Attempts  int       `json:"attempts"`
	LastError string    `json:"lastError,omitempty"`
	Outcome   string    `json:"outcome"`
	Retryable bool      `json:"retryable"`
	At        time.Time `json:"at"`
}

// Checkpoint records exactly the work that has been durably committed, so a
// resume continues from there and never skips data that was not transferred.
//
// It is written after a unit commits, never before. A checkpoint that describes
// progress which did not actually happen is the silent-corruption failure
// class, and it is the worst outcome the tool has.
type Checkpoint struct {
	// Stage is the pipeline stage the checkpoint belongs to.
	Stage string `json:"stage"`
	// Key is the storage object being written, where one applies.
	Key string `json:"key,omitempty"`
	// ByteOffset is how much of Key is durably written — the SFTP and disk
	// resume position.
	ByteOffset int64 `json:"byteOffset,omitempty"`
	// UploadID and Parts carry an S3 multipart upload across a restart.
	UploadID string `json:"uploadId,omitempty"`
	Parts    []Part `json:"parts,omitempty"`
	// Blocks carries an Azure staged block list.
	Blocks []string `json:"blocks,omitempty"`
	// FetchedArtifacts are the export files already received in full. Per-user
	// files make target fetch resumable at file granularity.
	FetchedArtifacts []string `json:"fetchedArtifacts,omitempty"`
	// LocalBundle is a sealed bundle waiting to be uploaded. Keeping it means a
	// failed upload does not cost the capture.
	LocalBundle string `json:"localBundle,omitempty"`
	// Digest is the rolling SHA-256 of what has been committed so far.
	Digest string `json:"digest,omitempty"`
	// HashState is the marshalled SHA-256 state covering exactly ByteOffset
	// bytes. It is what lets a resumed upload verify the whole object against
	// Digest rather than only the part it sent.
	HashState []byte `json:"hashState,omitempty"`

	UpdatedAt time.Time `json:"updatedAt"`
}

// Part is one completed S3 multipart part.
type Part struct {
	Number int    `json:"number"`
	ETag   string `json:"etag"`
	Size   int64  `json:"size"`
}

// Provenance records how a job reached its target, and is copied into the
// snapshot so a bundle can say where it came from.
type Provenance struct {
	EnvironmentKind string `json:"environmentKind,omitempty"`
	ExecutionMode   string `json:"executionMode,omitempty"`
	CloneRef        string `json:"cloneRef,omitempty"`
	KeycloakVersion string `json:"keycloakVersion,omitempty"`
	CaptureMode     string `json:"captureMode,omitempty"`
}

// Job is the persisted record of one unit of work. It lives in its own file so
// a stuck job can be inspected — and if necessary deleted — by hand.
type Job struct {
	ID     string   `json:"id"`
	Kind   JobKind  `json:"kind"`
	State  JobState `json:"state"`
	Phase  string   `json:"phase,omitempty"`
	Realm  string   `json:"realm,omitempty"`
	Source string   `json:"source,omitempty"`

	Environment string `json:"environment,omitempty"`
	Storage     string `json:"storage,omitempty"`
	SnapshotID  string `json:"snapshotId,omitempty"`
	StorageKey  string `json:"storageKey,omitempty"`

	// Encrypted, and enough of how, to resume without asking again.
	//
	// A bool was all this used to be, and it was not enough: resuming rebuilt
	// the encryption configuration as "on, mode unset" and the run was refused
	// before it started with "Encryption is on but no mode was chosen" — so
	// every encrypted capture that was interrupted had to be started over.
	//
	// The mode and the recipients are public and are written here. The
	// passphrase is not: nothing sensitive is written to a job file, so a
	// passphrase-sealed capture asks for it again on resume rather than
	// PortCloak keeping one on disk to save a prompt.
	Encrypted      bool     `json:"encrypted"`
	EncryptionMode string   `json:"encryptionMode,omitempty"`
	Recipients     []string `json:"recipients,omitempty"`

	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`

	// Message is the operator-facing sentence describing the current state.
	Message string `json:"message,omitempty"`
	// Hint is what to do next, for a failure that has one.
	Hint      string `json:"hint,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`

	Ledger     []LedgerEntry `json:"ledger,omitempty"`
	Checkpoint *Checkpoint   `json:"checkpoint,omitempty"`
	Provenance Provenance    `json:"provenance"`

	// CompletedPhases lets the Activity screen tick the pipeline without
	// replaying the event stream.
	CompletedPhases []string `json:"completedPhases,omitempty"`
}

// JobStore persists jobs as one JSON file each, under ~/.portcloak/jobs/.
type JobStore struct {
	mu   sync.Mutex
	home Home
	now  func() time.Time
}

// NewJobStore creates a job store rooted at home.
func NewJobStore(home Home) *JobStore {
	return &JobStore{home: home, now: time.Now}
}

// Rebind points the store at a moved home folder. Job files are read on demand,
// so there is nothing to reload.
func (s *JobStore) Rebind(home Home) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.home = home
}

// Save writes a job atomically. Every caller goes through here rather than
// writing the file directly, so a checkpoint can never be half-written.
func (s *JobStore) Save(j *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	j.UpdatedAt = s.now()
	b, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.home.JobFile(j.ID), append(b, '\n'), 0o600)
}

// Load reads one job.
func (s *JobStore) Load(id string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(id)
}

func (s *JobStore) loadLocked(id string) (*Job, error) {
	b, err := os.ReadFile(s.home.JobFile(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: job %q", ErrNotFound, id)
		}
		return nil, err
	}
	var j Job
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, fmt.Errorf("job %s has an unreadable record: %w", id, err)
	}
	return &j, nil
}

// List returns every job, newest first.
func (s *JobStore) List() ([]*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.home.JobsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Job
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		j, err := s.loadLocked(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			// An unreadable job record must not hide the readable ones.
			continue
		}
		out = append(out, j)
	}
	sort.Slice(out, func(i, k int) bool { return out[i].CreatedAt.After(out[k].CreatedAt) })
	return out, nil
}

// Delete removes a job record.
func (s *JobStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.home.JobFile(id))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// AdoptRunning turns jobs that were running when the process died into
// Interrupted, so they are offered on the next launch instead of appearing to
// still be in flight (UC-O9 A2).
//
// It is called once on startup, before anything new is queued.
func (s *JobStore) AdoptRunning() ([]*Job, error) {
	jobs, err := s.List()
	if err != nil {
		return nil, err
	}
	var adopted []*Job
	for _, j := range jobs {
		if j.State != JobRunning && j.State != JobQueued {
			continue
		}
		j.State = JobInterrupted
		j.Message = "PortCloak closed while this job was running."
		j.Retryable = true
		if err := s.Save(j); err != nil {
			return nil, err
		}
		adopted = append(adopted, j)
	}
	return adopted, nil
}

// PurgeFinished removes the records of jobs that have reached a terminal state,
// reporting how many went and how much space they freed. Interrupted jobs are
// left alone: discarding one is job control, not housekeeping (UC-O4 vs O10).
func (s *JobStore) PurgeFinished() (removed int, bytes int64, err error) {
	jobs, err := s.List()
	if err != nil {
		return 0, 0, err
	}
	for _, j := range jobs {
		if !j.State.Terminal() {
			continue
		}
		path := s.home.JobFile(j.ID)
		if st, statErr := os.Stat(path); statErr == nil {
			bytes += st.Size()
		}
		if err := s.Delete(j.ID); err != nil {
			return removed, bytes, err
		}
		removed++
	}
	return removed, bytes, nil
}

// Running reports whether any live job is using a named environment or storage,
// in the shape DeleteEnvironment and DeleteStorage expect.
func (s *JobStore) Running() JobLookup {
	return func(kind, name string) (string, bool) {
		jobs, err := s.List()
		if err != nil {
			return "", false
		}
		for _, j := range jobs {
			if j.State.Terminal() {
				continue
			}
			switch kind {
			case "environment":
				if j.Environment == name {
					return j.ID, true
				}
			case "storage":
				if j.Storage == name {
					return j.ID, true
				}
			}
		}
		return "", false
	}
}

// Append adds a ledger entry, collapsing repeated attempts at the same item so
// a flapping link produces one growing line rather than forty.
func (j *Job) Append(e LedgerEntry) {
	for i := range j.Ledger {
		if j.Ledger[i].Phase == e.Phase && j.Ledger[i].Item == e.Item {
			j.Ledger[i].Attempts = e.Attempts
			j.Ledger[i].LastError = e.LastError
			j.Ledger[i].Outcome = e.Outcome
			j.Ledger[i].Retryable = e.Retryable
			j.Ledger[i].At = e.At
			return
		}
	}
	j.Ledger = append(j.Ledger, e)
}

// CompletePhase records a phase as done, once.
func (j *Job) CompletePhase(phase string) {
	for _, p := range j.CompletedPhases {
		if p == phase {
			return
		}
	}
	j.CompletedPhases = append(j.CompletedPhases, phase)
}

// WorkPath is a scratch path scoped to one job, under the PortCloak home rather
// than the system temp directory, because these files hold realm material.
func (h Home) WorkPath(jobID string, name string) string {
	return filepath.Join(h.WorkDir(), jobID, name)
}
