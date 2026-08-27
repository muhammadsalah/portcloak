package app

import (
	"strings"
	"testing"
)

// TestNewBuild_KeepsStampedValues checks the path a release takes: main is
// linked with all three values and they are reported verbatim.
func TestNewBuild_KeepsStampedValues(t *testing.T) {
	b := NewBuild("0.0.1", "abc123def456", "2026-08-27T20:01:21Z")

	if b.Version != "0.0.1" {
		t.Errorf("Version = %q, want 0.0.1", b.Version)
	}
	if b.Date != "2026-08-27T20:01:21Z" {
		t.Errorf("Date = %q, want the stamped value", b.Date)
	}
	// The commit is either the stamped value or that value with -dirty
	// appended, depending on whether the tree this test ran in was clean.
	if !strings.HasPrefix(b.Commit, "abc123def456") {
		t.Errorf("Commit = %q, want it to start with the stamped hash", b.Commit)
	}
	if b.Go == "" || b.Platform == "" {
		t.Error("Go and Platform are read from the runtime and must never be empty")
	}
}

// TestNewBuild_FillsTheBlanks is the path an unstamped `go build` takes. The
// fields must never come out empty, because an empty version in an About panel
// reads as a broken app rather than an unstamped one.
func TestNewBuild_FillsTheBlanks(t *testing.T) {
	b := NewBuild("", "", "")

	if b.Version != "0.0.0-unknown" {
		t.Errorf("Version = %q, want the 0.0.0-unknown placeholder", b.Version)
	}
	if b.Commit == "" {
		t.Error("Commit must fall back to the VCS revision or the unknown placeholder")
	}
	if b.Date == "" {
		t.Error("Date must fall back to the VCS time or the unknown placeholder")
	}
}

// TestNewBuild_MarksADirtyTreeOnce guards against the suffix being appended
// again every time a build is constructed from an already-marked commit.
func TestNewBuild_MarksADirtyTreeOnce(t *testing.T) {
	b := NewBuild("0.0.1", "abc123-dirty", "2026-08-27T20:01:21Z")

	if n := strings.Count(b.Commit, "-dirty"); n > 1 {
		t.Errorf("Commit = %q, -dirty appended %d times", b.Commit, n)
	}
}

// TestBuild_DisplayDate covers both branches: a stamped RFC 3339 date is made
// readable, and anything else is shown as-is rather than replaced by a guess.
func TestBuild_DisplayDate(t *testing.T) {
	got := Build{Date: "2026-08-27T20:01:21Z"}.DisplayDate()
	if got != "27 August 2026, 20:01 UTC" {
		t.Errorf("DisplayDate() = %q, want %q", got, "27 August 2026, 20:01 UTC")
	}

	if got := (Build{Date: "unknown"}).DisplayDate(); got != "unknown" {
		t.Errorf("DisplayDate() = %q, want the unparseable value passed through", got)
	}
}

// TestBuild_StringCarriesEveryField pins the one-line form, which is what the
// About panel's copy button puts on the clipboard for a bug report.
func TestBuild_StringCarriesEveryField(t *testing.T) {
	s := Build{
		Version:  "0.0.1",
		Commit:   "abc123",
		Date:     "2026-08-27T20:01:21Z",
		Go:       "go1.27.0",
		Platform: "darwin/arm64",
	}.String()

	for _, want := range []string{"0.0.1", "abc123", "2026-08-27T20:01:21Z", "go1.27.0", "darwin/arm64"} {
		if !strings.Contains(s, want) {
			t.Errorf("String() = %q, missing %q", s, want)
		}
	}
}
