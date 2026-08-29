// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package app

// FirstRun is what a screen shows instead of an empty list.
//
// It is the same two steps wherever it appears, because they are the same two
// steps: an environment to read from, and somewhere to put what is read. Only
// the heading and the opening sentence differ, since what is empty differs.
// Snapshots has nothing to list; Activity has nothing that has run.
type FirstRun struct {
	Heading string `json:"heading"`
	Body    string `json:"body"`

	NeedsEnvironment bool   `json:"needsEnvironment"`
	NeedsStorage     bool   `json:"needsStorage"`
	EnvironmentBody  string `json:"environmentBody"`
	StorageBody      string `json:"storageBody"`
	NoAccountHeading string `json:"noAccountHeading"`
	NoAccountBody    string `json:"noAccountBody"`
	ConfigFile       string `json:"configFile"`
}

// firstRun builds the shared screen. The caller supplies only what is different
// about its own emptiness.
func (e *Engine) firstRun(heading, body string) *FirstRun {
	cfg := e.Config.Config()
	return &FirstRun{
		Heading:          heading,
		Body:             body,
		NeedsEnvironment: len(cfg.Environments) == 0,
		NeedsStorage:     len(cfg.Storage) == 0,
		EnvironmentBody:  "A Keycloak you can reach: on this machine, over SSH, in Docker, or in a Kubernetes namespace. PortCloak reads it. It never restarts or reconfigures the instance serving your logins.",
		StorageBody:      "A folder for the snapshots: on disk, on a host over SSH, in an S3 bucket, or in Azure Blob. You can mark one as requiring encryption, and nothing plaintext will ever be written there.",
		NoAccountHeading: "There is no account and no sign-in",
		NoAccountBody:    "PortCloak is a local tool. Everything it knows lives in plain files in your home folder. config.yaml holds your environments and storage, and every credential goes to this machine's keychain, referenced by handle. You can read that file, diff it, and commit it without leaking anything.",
		ConfigFile:       e.Home().ConfigFile(),
	}
}
