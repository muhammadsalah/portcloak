// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Defining an environment and a storage from a script is the thing that makes
// the rest of this usable unattended: a CI job that provisions a throwaway
// Keycloak has to point PortCloak at it before it can capture anything.

func TestCLI_EnvAddWritesADefinitionAndNothingElse(t *testing.T) {
	home := scratchHome(t)

	res := runCLI(t, home, "env", "add", "kubernetes", "kc-c",
		"--namespace", "kc", "--workload", "deployment/kc-c",
		"--admin-url", "http://kc-c.example", "--admin-user", "admin")
	if res.Code != ExitOK {
		t.Fatalf("exit %d\n%s", res.Code, res.all())
	}

	body, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	yaml := string(body)
	for _, want := range []string{"name: kc-c", "kind: kubernetes", "namespace: kc", "workload: deployment/kc-c"} {
		if !strings.Contains(yaml, want) {
			t.Errorf("config.yaml is missing %q:\n%s", want, yaml)
		}
	}

	// It contacted nothing, so it recorded no probe result. Writing one would
	// be claiming to know something it never asked.
	if strings.Contains(yaml, "lastProbe") {
		t.Errorf("adding a definition recorded a probe it never ran:\n%s", yaml)
	}
	if !strings.Contains(res.all(), "probe") {
		t.Errorf("the operator was not told how to check it:\n%s", res.all())
	}
}

// Re-running the same script must not fail on the second pass, and must not
// quietly overwrite on the first. --replace is the difference, and it has to be
// asked for.
func TestCLI_AddRefusesADuplicateUnlessReplaceIsAsked(t *testing.T) {
	home := scratchHome(t)
	add := []string{"storage", "add", "disk", "out", "--folder", t.TempDir()}

	if res := runCLI(t, home, add...); res.Code != ExitOK {
		t.Fatalf("first add failed: exit %d\n%s", res.Code, res.all())
	}
	res := runCLI(t, home, add...)
	if res.Code != ExitPrecondition {
		t.Fatalf("a duplicate was accepted silently: exit %d\n%s", res.Code, res.all())
	}
	if !strings.Contains(res.all(), "--replace") {
		t.Errorf("the refusal does not name what would satisfy it:\n%s", res.all())
	}
	if res := runCLI(t, home, append(add, "--replace")...); res.Code != ExitOK {
		t.Fatalf("--replace was refused: exit %d\n%s", res.Code, res.all())
	}
}

// A secret is read from a file or stdin, never from an argument, because argv is
// visible in ps to every user on the machine.
func TestCLI_AddTakesNoSecretOnTheCommandLine(t *testing.T) {
	home := scratchHome(t)
	for _, args := range [][]string{
		{"env", "add", "ssh", "e", "--host", "h", "--user", "u", "--server-folder", "/opt/keycloak"},
		{"storage", "add", "s3", "s", "--bucket", "b", "--region", "us-east-1"},
	} {
		res := runCLI(t, home, append(args, "--help")...)
		out := res.all()
		if strings.Contains(out, "--credential ") || strings.Contains(out, "--password ") {
			t.Errorf("`pcloak %s` offers a secret as a flag value:\n%s", strings.Join(args[:3], " "), out)
		}
		if !strings.Contains(out, "--credential-file") || !strings.Contains(out, "--credential-stdin") {
			t.Errorf("`pcloak %s` offers no way to supply a secret off the command line:\n%s",
				strings.Join(args[:3], " "), out)
		}
	}
}

// Forgetting a storage is not emptying it. A storage definition is cheap to
// recreate and a snapshot is not, so the two are separate acts and the message
// has to say so.
func TestCLI_StorageRemoveSaysTheSnapshotsStay(t *testing.T) {
	home := scratchHome(t)
	folder := t.TempDir()

	if res := runCLI(t, home, "storage", "add", "disk", "out", "--folder", folder); res.Code != ExitOK {
		t.Fatalf("exit %d\n%s", res.Code, res.all())
	}
	res := runCLI(t, home, "storage", "rm", "out", "--yes")
	if res.Code != ExitOK {
		t.Fatalf("exit %d\n%s", res.Code, res.all())
	}
	if !strings.Contains(res.all(), "Nothing in it was deleted") {
		t.Errorf("the message does not say the snapshots stay:\n%s", res.all())
	}
	if _, err := os.Stat(folder); err != nil {
		t.Errorf("removing the definition touched the folder: %v", err)
	}
	if res := runCLI(t, home, "storage", "list"); strings.Contains(res.stdout(), "out") {
		t.Errorf("the definition survived removal:\n%s", res.stdout())
	}
}

// The advice on an empty listing has to name a command that exists. pcloak has
// no `job add`, and sending somebody to one is worse than saying nothing.
func TestCLI_NotFoundOnlySuggestsCommandsThatExist(t *testing.T) {
	home := scratchHome(t)
	for kind, want := range map[string]string{
		"env":      "pcloak env add",
		"storage":  "pcloak storage add",
		"job":      "",
		"snapshot": "",
	} {
		res := runCLI(t, home, kind, "show", "nothing-by-this-name")
		out := res.all()
		if want == "" {
			if strings.Contains(out, " add ") {
				t.Errorf("`%s show` suggests an add command that does not exist:\n%s", kind, out)
			}
			continue
		}
		if !strings.Contains(out, want) {
			t.Errorf("`%s show` does not point at %q:\n%s", kind, want, out)
		}
	}
}
