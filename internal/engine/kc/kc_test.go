// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package kc

import (
	"fmt"
	"strings"
	"testing"
)

// The default invocation on a Keycloak that takes a management port — 25.0
// through 26.3 of the versions measured. --http-port and --https-port are not
// offered by any of them and must not appear here.
func TestBuildExport_DefaultInvocation(t *testing.T) {
	cmd, err := BuildExport(ExportRequest{
		KcPath:       "/opt/keycloak/bin/kc.sh",
		Dir:          "/tmp/portcloak-01J2K4",
		Realm:        "acme",
		UsersMode:    UsersDifferentFiles,
		UsersPerFile: 1000,
		Ports:        Ports{HTTP: 41823, HTTPS: 41824, Management: 41825},
		Supported:    OptionSet{"dir": true, "realm": true, "users": true, "http-management-port": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "/opt/keycloak/bin/kc.sh export --dir /tmp/portcloak-01J2K4 --realm acme " +
		"--users different_files --users-per-file 1000 " +
		"--http-management-port 41825"
	if got := cmd.String(); got != want {
		t.Fatalf("built\n  %s\nwant\n  %s", got, want)
	}
	if !cmd.PortsPassed {
		t.Error("a management port was passed, so the command should say so")
	}
}

func TestBuildExport_SingleFileVariant(t *testing.T) {
	cmd, err := BuildExport(ExportRequest{
		KcPath: "/opt/keycloak/bin/kc.sh", Dir: "/tmp/pc", Realm: "small", UsersMode: UsersRealmFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := cmd.String()
	if !strings.Contains(got, "--file /tmp/pc/small.json") {
		t.Errorf("the single-file variant should write one document: %s", got)
	}
	if strings.Contains(got, "--users-per-file") {
		t.Errorf("--users-per-file has no meaning outside different_files: %s", got)
	}
}

// One snapshot holds exactly one realm, so an export that did not name one is
// not something PortCloak can produce.
func TestBuildExport_RequiresARealm(t *testing.T) {
	if _, err := BuildExport(ExportRequest{KcPath: "kc.sh", Dir: "/tmp/pc"}); err == nil {
		t.Fatal("an export with no realm was accepted")
	}
}

func TestBuildImport_Strategies(t *testing.T) {
	cases := map[ImportStrategy]string{
		StrategyOverwrite: "--override true",
		StrategySkip:      "--override false",
	}
	for strategy, want := range cases {
		cmd, err := BuildImport(ImportRequest{KcPath: "kc.sh", Dir: "/tmp/in", Strategy: strategy})
		if err != nil {
			t.Fatalf("%s: %v", strategy, err)
		}
		if !strings.Contains(cmd.String(), want) {
			t.Errorf("%s built %s, want it to contain %s", strategy, cmd, want)
		}
	}

	// Merge has no offline equivalent. Silently downgrading it to overwrite
	// would apply a different, destructive operation than the one previewed.
	if _, err := BuildImport(ImportRequest{KcPath: "kc.sh", Dir: "/tmp/in", Strategy: StrategyMerge}); err == nil {
		t.Fatal("merge should not build an offline import invocation")
	}
	if _, err := BuildImport(ImportRequest{KcPath: "kc.sh", Dir: "/tmp/in"}); err == nil {
		t.Fatal("an import with no strategy was accepted")
	}
}

func TestParseVersion_AcrossBannerShapes(t *testing.T) {
	cases := map[string]string{
		"Keycloak 25.0.2\n": "25.0.2",
		"26.0.7\n":          "26.0.7",
		"Keycloak - Version 22.0.5\nJVM: 17.0.9\n":  "22.0.5",
		"WARN  [io.quarkus] Keycloak 24.0.1 on JVM": "24.0.1",
		"nothing useful here":                       "",
	}
	for in, want := range cases {
		if got := ParseVersion(in); got != want {
			t.Errorf("ParseVersion(%q) = %q, want %q", in, got, want)
		}
	}
	// Some releases write the version to stderr instead.
	if got := ParseVersion("", "Keycloak 23.0.0\n"); got != "23.0.0" {
		t.Errorf("version on stderr was not found: %q", got)
	}
}

func TestParseOutput_WarningsAndErrors(t *testing.T) {
	stdout := `INFO  [org.keycloak.exportimport] Exporting realm acme
WARN  [org.keycloak.services] Realm acme references theme 'acme-login' which is not deployed
INFO  Export finished
`
	stderr := `WARN  [org.keycloak.services] Realm acme references theme 'acme-login' which is not deployed
ERROR [org.keycloak] Failed to write users-12.json
`
	o := ParseOutput(stdout, stderr)
	if len(o.Warnings) != 1 {
		t.Fatalf("got %d warnings, want 1 deduplicated: %v", len(o.Warnings), o.Warnings)
	}
	if len(o.Errors) != 1 {
		t.Fatalf("got %d errors, want 1: %v", len(o.Errors), o.Errors)
	}
	if o.BindConflict {
		t.Error("this output is not a bind conflict")
	}
}

func TestParseOutput_BindConflict(t *testing.T) {
	o := ParseOutput("", "ERROR: java.net.BindException: Address already in use")
	if !o.BindConflict {
		t.Fatal("a bind conflict was not recognised, so a retryable failure would be reported as fatal")
	}
	msg, advice, retryable := ClassifyFailure("acme", o, "java.net.BindException: Address already in use")
	if !retryable {
		t.Error("a bind conflict must be retryable")
	}
	if !strings.Contains(msg, "bind") || advice == "" {
		t.Errorf("the operator-facing sentence is unhelpful: %q / %q", msg, advice)
	}
}

func TestReadLayout_OrdersUserFilesNumerically(t *testing.T) {
	l := ReadLayout("acme", []string{
		"acme-users-10.json", "acme-realm.json", "acme-users-2.json",
		"acme-users-0.json", "acme-users-1.json", "README.txt",
	})
	if l.RealmFile != "acme-realm.json" {
		t.Fatalf("realm file was %q", l.RealmFile)
	}
	want := []string{"acme-users-0.json", "acme-users-1.json", "acme-users-2.json", "acme-users-10.json"}
	if len(l.UserFiles) != len(want) {
		t.Fatalf("got %v", l.UserFiles)
	}
	for i := range want {
		if l.UserFiles[i] != want[i] {
			t.Fatalf("user files ordered lexically, not numerically: %v", l.UserFiles)
		}
	}
	if len(l.Other) != 1 || l.Other[0] != "README.txt" {
		t.Errorf("unexpected classification of extra files: %v", l.Other)
	}
	if !l.Complete() {
		t.Error("a layout with a realm file should read as complete")
	}
}

// kc.sh can exit zero having written a realm file and then died before the user
// files. Treating that as success would ship a snapshot missing its users.
func TestReadLayout_TruncatedExportIsNotComplete(t *testing.T) {
	l := ReadLayout("acme", []string{"acme-users-0.json", "acme-users-1.json"})
	if l.Complete() {
		t.Fatal("an export with no realm file was reported complete")
	}
}

func TestClassifyFailure_ProducesSentencesNotExitCodes(t *testing.T) {
	cases := []struct {
		stderr    string
		wantWord  string
		retryable bool
	}{
		{"ERROR: Realm 'acme' not found", "realm called", false},
		{"java.io.IOException: No space left on device", "disk space", false},
		{"Connection refused to jdbc:postgresql://db:5432", "database", true},
		{"Permission denied", "refused permission", false},
		{"Unrecognized option: --users-per-file", "did not recognise", false},
	}
	for _, c := range cases {
		o := ParseOutput("", c.stderr)
		o.ExitCode = 1
		msg, _, retryable := ClassifyFailure("acme", o, c.stderr)
		if !strings.Contains(strings.ToLower(msg), c.wantWord) {
			t.Errorf("stderr %q produced %q, want it to mention %q", c.stderr, msg, c.wantWord)
		}
		if retryable != c.retryable {
			t.Errorf("stderr %q classified retryable=%v, want %v", c.stderr, retryable, c.retryable)
		}
	}
}

// A real Keycloak logs with a timestamp in front of the level. Reading the
// level only at the start of a line dropped every warning and every error the
// server itself produced, which left a failed export reporting its exit code.
func TestParseOutput_ReadsTimestampedServerLines(t *testing.T) {
	stderr := `2026-08-28 06:02:41,127 WARN  [com.arjuna.ats.arjuna] (Transaction Reaper Worker 0) ARJUNA012108: CheckedAction::check - atomic action aborting with 1 threads active!
2026-08-28 06:02:41,470 ERROR [org.keycloak.quarkus.runtime.cli.ExecutionExceptionHandler] (main) ERROR: Failed to start server in (nonserver) mode
2026-08-28 06:02:41,471 ERROR [org.keycloak.quarkus.runtime.cli.ExecutionExceptionHandler] (main) ERROR: Transaction was rolled back in a different thread`
	o := ParseOutput("", stderr)
	if len(o.Warnings) == 0 {
		t.Error("a timestamped WARN line reached the ledger as nothing")
	}
	if len(o.Errors) == 0 {
		t.Fatal("a timestamped ERROR line reached the ledger as nothing")
	}
	for _, e := range o.Errors {
		if strings.HasPrefix(strings.ToUpper(e), "ERROR") {
			t.Errorf("the level was left inside the message: %q", e)
		}
	}
}

// An export that outran the transaction timeout used to arrive as "kc.sh export
// exited with code 1", which sent an operator looking at disk space.
func TestClassifyFailure_NamesTheTransactionTimeout(t *testing.T) {
	stderr := `2026-08-28 06:02:41,127 WARN  [com.arjuna.ats.arjuna] (Transaction Reaper Worker 0) ARJUNA012121: TransactionReaper::doCancellations worker successfully canceled TX
2026-08-28 06:02:41,470 ERROR [org.keycloak.quarkus.runtime.cli.ExecutionExceptionHandler] (main) ERROR: Failed to start server in (nonserver) mode
2026-08-28 06:02:41,471 ERROR [org.keycloak.quarkus.runtime.cli.ExecutionExceptionHandler] (main) ERROR: Database operation failed
2026-08-28 06:02:41,471 ERROR [org.keycloak.quarkus.runtime.cli.ExecutionExceptionHandler] (main) ERROR: Transaction was rolled back in a different thread`
	o := ParseOutput("", stderr)
	o.ExitCode = 1
	msg, advice, retryable := ClassifyFailure("corp-a", o, stderr)
	if !strings.Contains(strings.ToLower(msg), "transaction") {
		t.Errorf("the timeout was reported as %q", msg)
	}
	if !strings.Contains(strings.ToLower(advice), "users-per-file") {
		t.Errorf("the advice does not say what to change: %q", advice)
	}
	if strings.Contains(strings.ToLower(msg+advice), "disk space") && !strings.Contains(advice, "not disk space") {
		t.Errorf("the advice points at disk space: %q", advice)
	}
	if retryable {
		t.Error("a timeout retried unchanged fails the same way")
	}
}

// The page size an operator can ask for is bounded at both ends: below ten the
// file count buys nothing, and above a thousand is the page that does not
// finish inside one transaction.
func TestClampUsersPerFile_HoldsTheRange(t *testing.T) {
	for _, c := range []struct{ in, want int }{
		{0, UsersPerFileDefault},
		{-5, UsersPerFileDefault},
		{1, UsersPerFileMin},
		{10, 10},
		{250, 250},
		{1000, 1000},
		{5000, UsersPerFileMax},
	} {
		if got := ClampUsersPerFile(c.in); got != c.want {
			t.Errorf("%d clamped to %d, want %d", c.in, got, c.want)
		}
	}
}

// Transactions cannot be turned off, so the flag lifts the limit on them —
// through the environment, because no kc.sh option reaches it.
func TestBuildExport_LiftsTheTransactionLimitOnlyWhenAsked(t *testing.T) {
	req := ExportRequest{KcPath: "/opt/keycloak/bin/kc.sh", Dir: "/tmp/x", Realm: "acme"}

	plain, err := BuildExport(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.Env) != 0 {
		t.Errorf("an ordinary export carried environment it did not ask for: %v", plain.Env)
	}

	req.NoTransactionTimeout = true
	lifted, err := BuildExport(req)
	if err != nil {
		t.Fatal(err)
	}
	if lifted.Env[TransactionTimeoutVar] != "0" {
		t.Errorf("the limit was not lifted: %v", lifted.Env)
	}
	// The option list is untouched: this is not something kc.sh takes, and
	// passing it as one aborts the command before it reads the realm.
	if strings.Contains(strings.Join(lifted.Args, " "), "transaction") {
		t.Errorf("the environment leaked onto the command line: %v", lifted.Args)
	}
	// An operator reading the log has to be able to see it was lifted.
	if !strings.HasPrefix(lifted.String(), TransactionTimeoutVar+"=0 ") {
		t.Errorf("the rendered command hides the environment: %q", lifted.String())
	}
}

// The restore needs the same escape hatch as the capture: the import writes
// users a page at a time in the same way, and a destination federated to the
// same directory validates them on the way in.
func TestBuildImport_LiftsTheTransactionLimitOnlyWhenAsked(t *testing.T) {
	req := ImportRequest{KcPath: "/opt/keycloak/bin/kc.sh", Dir: "/tmp/x", Strategy: StrategyOverwrite}

	plain, err := BuildImport(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.Env) != 0 {
		t.Errorf("an ordinary import carried environment it did not ask for: %v", plain.Env)
	}

	req.NoTransactionTimeout = true
	lifted, err := BuildImport(req)
	if err != nil {
		t.Fatal(err)
	}
	if lifted.Env[TransactionTimeoutVar] != "0" {
		t.Errorf("the limit was not lifted: %v", lifted.Env)
	}
	if strings.Contains(strings.Join(lifted.Args, " "), "transaction") {
		t.Errorf("the environment leaked onto the command line: %v", lifted.Args)
	}
}

// kc.sh numbers user files 0, 1, … 10, which anything ordering names as text
// reads as 0, 1, 10, 2. The snapshot carries them padded so that ordering is
// the same as the order the pages were written.
func TestPadUserFiles_MakesEveryNumberTheSameWidth(t *testing.T) {
	in := ExportLayout{
		RealmFile: "acme-realm.json",
		UserFiles: []string{"acme-users-0.json", "acme-users-1.json", "acme-users-10.json"},
		Other:     []string{"acme-keys.json"},
	}
	out, renames := PadUserFiles(in)

	want := []string{"acme-users-000.json", "acme-users-001.json", "acme-users-010.json"}
	if strings.Join(out.UserFiles, ",") != strings.Join(want, ",") {
		t.Errorf("padded to %v, want %v", out.UserFiles, want)
	}
	if renames["acme-users-10.json"] != "acme-users-010.json" {
		t.Errorf("the rename map does not carry the change: %v", renames)
	}
	// Only the user files are touched. The realm file's name is what the
	// import looks for, and everything else travels as it was written.
	if out.RealmFile != "acme-realm.json" || strings.Join(out.Other, ",") != "acme-keys.json" {
		t.Errorf("something other than a user file was renamed: %+v", out)
	}
}

// Three digits is the floor, not the width. A realm big enough to need four
// gets four, or the same problem returns at file 1,000.
func TestPadUserFiles_WidensForALargeExport(t *testing.T) {
	in := ExportLayout{RealmFile: "acme-realm.json"}
	for _, n := range []int{0, 42, 1200} {
		in.UserFiles = append(in.UserFiles, fmt.Sprintf("acme-users-%d.json", n))
	}
	out, _ := PadUserFiles(in)
	for _, name := range out.UserFiles {
		if len(name) != len("acme-users-0000.json") {
			t.Errorf("%q is not the same width as the rest: %v", name, out.UserFiles)
		}
	}
}

// A federated realm's user files pad on the same rule: the prefix is whatever
// came before the number, whatever it happens to be.
func TestPadUserFiles_PadsFederatedUserFilesToo(t *testing.T) {
	out, _ := PadUserFiles(ExportLayout{
		RealmFile: "acme-realm.json",
		UserFiles: []string{"acme-federated-users-7.json"},
	})
	if out.UserFiles[0] != "acme-federated-users-007.json" {
		t.Errorf("federated users padded to %q", out.UserFiles[0])
	}
}

// Two files staged under one name is a snapshot that silently lost a page of
// users. Where padding would collide, nothing is renamed at all.
func TestPadUserFiles_LeavesEverythingAloneOnACollision(t *testing.T) {
	in := ExportLayout{
		RealmFile: "acme-realm.json",
		UserFiles: []string{"acme-users-0.json", "acme-users-000.json"},
	}
	out, renames := PadUserFiles(in)
	if renames != nil {
		t.Errorf("a colliding rename was accepted: %v", renames)
	}
	if strings.Join(out.UserFiles, ",") != strings.Join(in.UserFiles, ",") {
		t.Errorf("the layout was changed anyway: %v", out.UserFiles)
	}
}

// An export already at the right width is carried as it is, so nothing is
// renamed for the sake of it.
func TestPadUserFiles_IsANoOpWhenAlreadyPadded(t *testing.T) {
	in := ExportLayout{RealmFile: "acme-realm.json", UserFiles: []string{"acme-users-000.json"}}
	if _, renames := PadUserFiles(in); len(renames) != 0 {
		t.Errorf("a padded export was renamed again: %v", renames)
	}
}

func TestStrategyExplanation_IsAboutResourcesNotFlags(t *testing.T) {
	for _, s := range []ImportStrategy{StrategyOverwrite, StrategySkip, StrategyMerge} {
		e := StrategyExplanation(s)
		if e == "" {
			t.Errorf("%s has no explanation", s)
		}
		if strings.Contains(e, "--override") {
			t.Errorf("%s is explained in terms of a Keycloak flag: %q", s, e)
		}
	}
}
