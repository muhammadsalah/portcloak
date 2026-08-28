// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package k8s

import (
	"strings"
	"testing"

	"portcloak/internal/engine/target"
)

// The exec subresource has no environment field, so an adapter that does
// nothing about it accepts a Command carrying Env and runs it without one.
// Local, SSH and Docker all apply it; this is the fourth adapter agreeing.
func TestExecArgv_CarriesTheEnvironment(t *testing.T) {
	argv := execArgv(target.Command{
		Path: "/opt/keycloak/bin/kc.sh",
		Args: []string{"export", "--realm", "acme"},
		Env: map[string]string{
			"QUARKUS_TRANSACTION_MANAGER_DEFAULT_TRANSACTION_TIMEOUT": "0",
			"A_SECOND": "value",
		},
	})
	if argv[0] != "env" {
		t.Fatalf("the environment was dropped: %v", argv)
	}
	// Sorted, so the same command renders the same way every run.
	if argv[1] != "A_SECOND=value" {
		t.Errorf("the variables are not in a stable order: %v", argv)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "QUARKUS_TRANSACTION_MANAGER_DEFAULT_TRANSACTION_TIMEOUT=0") {
		t.Errorf("the transaction limit did not reach the command: %v", argv)
	}
	if !strings.HasSuffix(joined, "kc.sh export --realm acme") {
		t.Errorf("the command itself was mangled: %v", argv)
	}
}

// A command with no environment is run as itself, not wrapped in env(1) for
// nothing.
func TestExecArgv_LeavesAPlainCommandAlone(t *testing.T) {
	argv := execArgv(target.Command{Path: "/bin/sh", Args: []string{"-c", "true"}})
	if len(argv) != 3 || argv[0] != "/bin/sh" {
		t.Errorf("a plain command was rewritten: %v", argv)
	}
}

// The working directory wraps whatever the environment produced, rather than
// the two features cancelling each other out.
func TestExecArgv_WorkingDirectoryWrapsTheEnvironment(t *testing.T) {
	argv := execArgv(target.Command{
		Path: "/bin/sh", Args: []string{"-c", "echo $X"},
		Env: map[string]string{"X": "1"}, Dir: "/tmp/work dir",
	})
	if argv[0] != "/bin/sh" || argv[1] != "-c" {
		t.Fatalf("the directory wrapper is missing: %v", argv)
	}
	if !strings.Contains(argv[2], "cd '/tmp/work dir'") {
		t.Errorf("the directory was not quoted: %q", argv[2])
	}
	// shellJoin quotes every token, which changes nothing about how the shell
	// resolves env(1) — quoting affects word splitting, not command lookup.
	if !strings.Contains(argv[2], "exec 'env' 'X=1'") {
		t.Errorf("the environment was lost inside the wrapper: %q", argv[2])
	}
}
