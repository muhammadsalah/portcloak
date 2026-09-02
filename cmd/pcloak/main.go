// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Command pcloak is PortCloak's command line.
//
// This package contains the bootstrap and nothing else. The commands live in
// internal/cli, everything they can reach lives in internal/app, and every
// decision the tool makes lives in internal/engine — the same engine the desktop
// app drives, reading the same ~/.portcloak.
//
// Nothing here imports Wails, and nothing it reaches may either: that is what
// lets this binary be built with CGO_ENABLED=0 for every platform and run on a
// machine with no webview toolkit, which is most of the machines a realm is
// actually captured from.
package main

import (
	"os"

	"portcloak/internal/app"
	"portcloak/internal/cli"
)

// Stamped at build time by build/package.sh, exactly as the desktop binary is:
//
//	-ldflags "-X main.version=0.0.4 -X main.commit=abc123 -X main.date=..."
//
// Left unstamped, a plain `go build` still reports a real commit and date, which
// app.NewBuild recovers from the VCS information the Go toolchain embeds.
var (
	version = "0.0.4-dev"
	commit  = ""
	date    = ""
)

func main() {
	build := app.NewBuild(version, commit, date)
	// The version a snapshot manifest records is the plain string, because a
	// manifest field is a format promise that must not grow a struct under it.
	cli.SetVersion(build.Version)
	os.Exit(cli.Main(build, os.Args[1:], cli.DefaultStreams()))
}
