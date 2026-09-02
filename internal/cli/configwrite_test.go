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

// A secret can be supplied four ways, and every command that takes one offers
// all four. The direct flag exists because refusing it sends people writing
// passwords to temporary files to get past a flag that will not take one — but
// the safer three have to be there beside it, or the warning on the direct one
// is telling somebody off for the only option they had.
func TestCLI_EverySecretHasAllFourWaysIn(t *testing.T) {
	home := scratchHome(t)
	for _, tc := range []struct {
		args   []string
		prefix string
	}{
		{[]string{"env", "add", "ssh", "e"}, "--credential"},
		{[]string{"storage", "add", "s3", "s"}, "--credential"},
		{[]string{"env", "add", "ssh", "e"}, "--admin-password"},
	} {
		res := runCLI(t, home, append(tc.args, "--help")...)
		out := res.all()
		for _, suffix := range []string{"", "-file", "-stdin", "-prompt"} {
			if !strings.Contains(out, tc.prefix+suffix) {
				t.Errorf("`pcloak %s` does not offer %s:\n%s",
					strings.Join(tc.args[:3], " "), tc.prefix+suffix, out)
			}
		}
		// The direct one has to say what it costs, in the help and not only in
		// the warning: somebody reading --help is choosing between them.
		if !strings.Contains(out, tc.prefix+" string            ") &&
			!strings.Contains(out, "visible in ps") {
			t.Errorf("`pcloak %s` does not say %s is visible in ps:\n%s",
				strings.Join(tc.args[:3], " "), tc.prefix, out)
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

// A secret may be given directly, because refusing one sends people writing
// passwords to temporary files to get past a flag that will not take one, which
// is worse. What it must not do is happen silently: argv is visible in ps to
// every user on the machine, and shells record it.
func TestCLI_ASecretOnTheCommandLineIsAcceptedAndWarnedAbout(t *testing.T) {
	home := scratchHome(t)

	res := runCLI(t, home, "storage", "add", "s3", "ci",
		"--bucket", "b", "--region", "us-east-1", "--credential", "AKIA:secret")
	if res.Code != ExitOK {
		t.Fatalf("exit %d\n%s", res.Code, res.all())
	}
	warning := res.stderr()
	for _, want := range []string{"ps", "history"} {
		if !strings.Contains(warning, want) {
			t.Errorf("the warning does not say %q costs anything:\n%s", want, warning)
		}
	}
	// On stderr, so it cannot corrupt a pipe reading the result.
	if strings.Contains(res.stdout(), "history") {
		t.Errorf("the warning reached stdout:\n%s", res.stdout())
	}
	// And --quiet silences it, for somebody who has decided and does not need
	// telling on every run.
	res = runCLI(t, home, "-q", "storage", "add", "s3", "ci2",
		"--bucket", "b", "--region", "us-east-1", "--credential", "AKIA:secret")
	if strings.Contains(res.stderr(), "history") {
		t.Errorf("--quiet did not silence the warning:\n%s", res.stderr())
	}
}

// The four ways to supply one secret are alternatives, not a precedence puzzle.
// Two at once is a mistake worth catching rather than resolving.
func TestCLI_SecretSourcesAreMutuallyExclusive(t *testing.T) {
	home := scratchHome(t)
	res := runCLI(t, home, "storage", "add", "s3", "x",
		"--bucket", "b", "--region", "r", "--credential", "a", "--credential-stdin")
	if res.Code != ExitUsage {
		t.Fatalf("two secret sources at once should be a usage error; got exit %d\n%s", res.Code, res.all())
	}
}

// Every prompt has a flag, and where there is nobody to ask the refusal names
// the flag rather than the absence. "There is no terminal to ask on" is true and
// useless.
func TestCLI_PromptWithNoTerminalNamesTheFlagsInstead(t *testing.T) {
	home := scratchHome(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"storage", "add", "ssh", "r", "--host", "h", "--user", "u",
			"--folder", "/s", "--credential-prompt"}, "--credential-file"},
		{[]string{"env", "add", "ssh", "e", "--host", "h", "--user", "u",
			"--server-folder", "/o", "--admin-url", "http://x", "--admin-password-prompt"}, "--admin-password-file"},
	} {
		res := runCLI(t, home, tc.args...)
		if res.Code != ExitPrecondition {
			t.Errorf("%v: expected a precondition refusal, got exit %d\n%s", tc.args[:3], res.Code, res.all())
		}
		if !strings.Contains(res.all(), tc.want) {
			t.Errorf("%v: the refusal does not name %s:\n%s", tc.args[:3], tc.want, res.all())
		}
		// Prefixed once, not twice: the helper already wrote a whole sentence.
		if strings.Contains(res.all(), "pcloak: pcloak:") {
			t.Errorf("%v: the message is double-prefixed:\n%s", tc.args[:3], res.all())
		}
	}
}

// Anything cobra rejects before RunE — an unknown flag, the wrong number of
// arguments, a missing required flag, two mutually exclusive ones — is a usage
// error and not a failed attempt. A script that retries on failure must not
// retry a typo.
func TestCLI_UsageErrorsExitTwo(t *testing.T) {
	home := scratchHome(t)
	for _, args := range [][]string{
		{"storage", "add", "s3", "x", "--bogus-flag"},
		{"storage", "add", "s3"},
		{"env", "add", "ssh", "e"},
		{"snapshot", "show"},
	} {
		if res := runCLI(t, home, args...); res.Code != ExitUsage {
			t.Errorf("`pcloak %s` exited %d, not %d\n%s",
				strings.Join(args, " "), res.Code, ExitUsage, res.all())
		}
	}
}
