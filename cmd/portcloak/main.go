// Command portcloak is the PortCloak desktop application.
//
// This package contains the Wails bootstrap and nothing else. Every decision
// the tool makes lives in internal/engine, which never imports Wails — that is
// what lets the whole capture and restore pipeline be driven from a test binary
// with no desktop runtime present.
package main

import (
	"fmt"
	"os"

	"portcloak/internal/app"
)

// Stamped at build time by build/package.sh:
//
//	-ldflags "-X main.version=0.0.1 -X main.commit=abc123 -X main.date=..."
//
// Left unstamped, a plain `go build` still reports a real commit and date:
// app.NewBuild recovers both from the VCS information the Go toolchain embeds
// on its own, and marks the build dirty if the tree had uncommitted changes.
var (
	version = "0.0.1-dev"
	commit  = ""
	date    = ""
)

func main() {
	if err := app.Run(app.NewBuild(version, commit, date)); err != nil {
		fmt.Fprintln(os.Stderr, "PortCloak could not start:", err)
		os.Exit(1)
	}
}
