package kc

import (
	"strings"
	"testing"
)

func TestBuildExport_DefaultInvocation(t *testing.T) {
	cmd, err := BuildExport(ExportRequest{
		KcPath:       "/opt/keycloak/bin/kc.sh",
		Dir:          "/tmp/portcloak-01J2K4",
		Realm:        "acme",
		UsersMode:    UsersDifferentFiles,
		UsersPerFile: 1000,
		Ports:        Ports{HTTP: 41823, HTTPS: 41824, Management: 41825},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "/opt/keycloak/bin/kc.sh export --dir /tmp/portcloak-01J2K4 --realm acme " +
		"--users different_files --users-per-file 1000 " +
		"--http-port 41823 --https-port 41824 --http-management-port 41825"
	if got := cmd.String(); got != want {
		t.Fatalf("built\n  %s\nwant\n  %s", got, want)
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
