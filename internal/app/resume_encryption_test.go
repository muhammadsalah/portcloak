package app

import (
	"strings"
	"testing"
	"time"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/snapshot"
)

// An interrupted capture is exactly the case encryption must survive: the run
// that failed is the one worth not doing twice.
//
// The job carried `Encrypted bool` and nothing else, so resuming rebuilt the
// configuration as "on, mode unset" and the run was refused before it started
// with "Encryption is on but no mode was chosen" — an internal complaint about
// a field the operator never saw, and no way forward but starting over.

func interruptedCapture(t *testing.T, eng *Engine, j *config.Job) {
	t.Helper()
	j.Kind = config.JobCapture
	j.State = config.JobInterrupted
	if j.CreatedAt.IsZero() {
		j.CreatedAt = time.Now()
	}
	if err := eng.Jobs.Save(j); err != nil {
		t.Fatalf("the job could not be written: %v", err)
	}
}

// Recipients are age public keys. They are not secret, they are on the job, and
// a resume rebuilds from them without asking anyone anything.
func TestResume_RebuildsRecipientEncryptionWithoutAsking(t *testing.T) {
	eng := emptyEngine(t)
	interruptedCapture(t, eng, &config.Job{
		ID: "job-recipients", Realm: "corp-a",
		Environment: "prod", Storage: "archive",
		Encrypted:      true,
		EncryptionMode: string(snapshot.EncryptionRecipients),
		Recipients:     []string{"age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"},
	})

	res := NewJobsController(eng).Resume("job-recipients", "")

	// The environment and storage do not exist in this empty home, so the run
	// fails — but it must fail on reaching them, never on the encryption
	// settings it was told to reuse.
	if res.Failure != nil && strings.Contains(res.Failure.Message, "Encryption is on") {
		t.Errorf("the encryption decision was not rebuilt from the job: %s", res.Failure.Message)
	}
	if res.Failure != nil && strings.Contains(res.Failure.Message, "no mode was chosen") {
		t.Errorf("the mode was lost across the resume: %s", res.Failure.Message)
	}
}

// A passphrase is not written to a job file, so it is the one part of the
// decision that has to be asked for again. The refusal has to say that in a
// sentence the operator can act on.
func TestResume_AsksForThePassphraseItDoesNotKeep(t *testing.T) {
	eng := emptyEngine(t)
	interruptedCapture(t, eng, &config.Job{
		ID: "job-passphrase", Realm: "corp-a",
		Environment: "prod", Storage: "archive",
		Encrypted:      true,
		EncryptionMode: string(snapshot.EncryptionPassphrase),
	})
	jobs := NewJobsController(eng)

	res := jobs.Resume("job-passphrase", "")
	if res.Failure == nil {
		t.Fatal("a passphrase-sealed capture resumed with no passphrase")
	}
	if !strings.Contains(res.Failure.Message, "passphrase") {
		t.Errorf("the refusal does not say what is missing: %q", res.Failure.Message)
	}
	if strings.Contains(res.Failure.Message, "no mode was chosen") {
		t.Errorf("the operator is being shown an internal complaint: %q", res.Failure.Message)
	}

	// And the screen is told to ask, rather than finding out from a rejection.
	view := jobs.List()
	var found bool
	for _, j := range view.Jobs {
		if j.ID == "job-passphrase" {
			found = true
			if !j.NeedsPassphrase {
				t.Error("the row does not say a passphrase is needed, so nothing prompts for one")
			}
		}
	}
	if !found {
		t.Fatal("the interrupted job is not on the Activity screen at all")
	}

	// Given one, it gets past the encryption check and on to the real work.
	res = jobs.Resume("job-passphrase", "the-original-passphrase")
	if res.Failure != nil && strings.Contains(res.Failure.Message, "passphrase") {
		t.Errorf("a supplied passphrase was still refused: %s", res.Failure.Message)
	}
}

// A job written before the mode was recorded cannot be resumed into the same
// bundle, and saying so beats re-running an export that would seal to nothing.
func TestResume_RefusesAJobWhoseEncryptionWasNeverRecorded(t *testing.T) {
	eng := emptyEngine(t)
	interruptedCapture(t, eng, &config.Job{
		ID: "job-legacy", Realm: "corp-a",
		Environment: "prod", Storage: "archive",
		Encrypted: true,
	})

	res := NewJobsController(eng).Resume("job-legacy", "")
	if res.Failure == nil {
		t.Fatal("a job with no recorded encryption mode was resumed")
	}
	if !strings.Contains(res.Failure.Message, "did not record how") {
		t.Errorf("the refusal does not explain itself: %q", res.Failure.Message)
	}
	if res.Failure.Hint == "" {
		t.Error("the refusal offers no way forward")
	}
}

// An unencrypted capture never had a decision to lose, and must not acquire a
// prompt it has no use for.
func TestResume_AnUnencryptedCaptureIsNotAskedForAnything(t *testing.T) {
	eng := emptyEngine(t)
	interruptedCapture(t, eng, &config.Job{
		ID: "job-plain", Realm: "corp-a",
		Environment: "prod", Storage: "archive",
	})

	for _, j := range NewJobsController(eng).List().Jobs {
		if j.ID == "job-plain" && j.NeedsPassphrase {
			t.Error("an unencrypted capture is asking for a passphrase")
		}
	}
	res := NewJobsController(eng).Resume("job-plain", "")
	if res.Failure != nil && strings.Contains(res.Failure.Message, "passphrase") {
		t.Errorf("an unencrypted capture was refused over a passphrase: %s", res.Failure.Message)
	}
}

// A guard on the guards: the recipients case must be reaching the real work,
// not passing because it failed earlier for an unrelated reason.
func TestResume_RecipientCaseReachesTheEnvironment(t *testing.T) {
	eng := emptyEngine(t)
	interruptedCapture(t, eng, &config.Job{
		ID: "job-reaches", Realm: "corp-a",
		Environment: "prod", Storage: "archive",
		Encrypted:      true,
		EncryptionMode: string(snapshot.EncryptionRecipients),
		Recipients:     []string{"age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"},
	})
	res := NewJobsController(eng).Resume("job-reaches", "")
	if res.Failure == nil {
		t.Skip("the capture started; nothing more to assert here")
	}
	t.Logf("failure: %s", res.Failure.Message)
	if !strings.Contains(strings.ToLower(res.Failure.Message), "prod") &&
		!strings.Contains(strings.ToLower(res.Failure.Message), "environment") &&
		!strings.Contains(strings.ToLower(res.Failure.Message), "found") {
		t.Errorf("the resume stopped somewhere unexpected: %q", res.Failure.Message)
	}
}
