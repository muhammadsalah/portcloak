package app

import (
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

// Build identifies the binary: which version it claims to be, which commit it
// was cut from, and when it was compiled.
//
// The release gate in spec/rollout/11-release-0.0.1.md requires all three to be
// embedded and shown in the app, and the reason is support rather than vanity.
// A snapshot carries the producing version in its manifest, so when a bundle
// will not restore the first question is which build wrote it — and the answer
// has to be readable from the About panel by whoever is holding the machine.
type Build struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	// Go and Platform are not stamped; they are read from the runtime, and
	// they are here because "works on my machine" is usually an architecture.
	Go       string `json:"go"`
	Platform string `json:"platform"`
}

// NewBuild fills in the parts the linker did not.
//
// The three stamped fields are passed in from main, which is the only place
// -ldflags can reach. Any that arrived empty are recovered from the build info
// Go embeds automatically, so a plain `go build ./cmd/portcloak` with no flags
// still reports a real commit instead of a blank. That matters more than it
// looks: the builds most likely to produce a confusing bug report are the
// unstamped local ones.
func NewBuild(version, commit, date string) Build {
	b := Build{
		Version:  strings.TrimSpace(version),
		Commit:   strings.TrimSpace(commit),
		Date:     strings.TrimSpace(date),
		Go:       runtime.Version(),
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		var revision, modified, vcsTime string
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				revision = s.Value
			case "vcs.modified":
				modified = s.Value
			case "vcs.time":
				vcsTime = s.Value
			}
		}
		if b.Commit == "" && revision != "" {
			b.Commit = shortCommit(revision)
		}
		// A binary built from a dirty tree is not the commit it names, and
		// saying so is the difference between a reproducible bug report and a
		// wasted afternoon.
		if modified == "true" && b.Commit != "" && !strings.HasSuffix(b.Commit, "-dirty") {
			b.Commit += "-dirty"
		}
		if b.Date == "" {
			b.Date = vcsTime
		}
	}

	if b.Version == "" {
		b.Version = "0.0.0-unknown"
	}
	if b.Commit == "" {
		b.Commit = "unknown"
	}
	if b.Date == "" {
		b.Date = "unknown"
	}
	return b
}

// shortCommit trims a full hash to the 12 characters git itself abbreviates to
// in logs, which is short enough to read aloud and long enough to be unique.
func shortCommit(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
}

// String renders the build for a log line or a support ticket.
func (b Build) String() string {
	return b.Version + " (" + b.Commit + ", " + b.Date + ", " + b.Go + ", " + b.Platform + ")"
}

// DisplayDate renders the build date the way a person reads one. Stamped dates
// are RFC 3339 in UTC; anything that does not parse is shown verbatim rather
// than replaced with a guess.
func (b Build) DisplayDate() string {
	t, err := time.Parse(time.RFC3339, b.Date)
	if err != nil {
		return b.Date
	}
	return t.UTC().Format("2 January 2006, 15:04 MST")
}
