package kc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixtures under testdata/kc-help are the real `kc.sh export --help-all`
// output of four Keycloak images, captured verbatim. They are the evidence for
// the rule this package now follows, and they are the reason it is a discovery
// rather than a version table: the answer is different in 24.0, in 25.0, and
// again in 26.5.
//
// If a future Keycloak changes it once more, add its output here and this test
// says what changed rather than a capture failing in front of an operator.
var helpFixtures = map[string]struct {
	// httpManagementPort is whether `export` offers --http-management-port.
	httpManagementPort bool
}{
	"export-24.0.txt":   {httpManagementPort: false},
	"export-25.0.4.txt": {httpManagementPort: true},
	"export-26.3.txt":   {httpManagementPort: true},
	"export-26.5.0.txt": {httpManagementPort: false},
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "kc-help", name))
	if err != nil {
		t.Fatalf("the help fixture could not be read: %v", err)
	}
	return string(b)
}

// TestParseOptions_AgainstRealHelpOutput pins what each version actually
// offers, and pins the two facts the whole mechanism rests on: no version
// offers --http-port or --https-port to export, and every version offers the
// options PortCloak always passes.
func TestParseOptions_AgainstRealHelpOutput(t *testing.T) {
	for name, want := range helpFixtures {
		t.Run(name, func(t *testing.T) {
			opts := ParseOptions(readFixture(t, name))

			if !opts.Known() {
				t.Fatal("no options were parsed out of real help output")
			}
			for _, required := range []string{"dir", "file", "realm", "users", "optimized", "help-all"} {
				if !opts.Has(required) {
					t.Errorf("export should offer --%s and this parse did not find it", required)
				}
			}
			for _, absent := range []string{"http-port", "https-port"} {
				if opts.Has(absent) {
					t.Errorf("--%s is not an export option on any measured Keycloak; the parse invented it", absent)
				}
			}
			if got := opts.Has("http-management-port"); got != want.httpManagementPort {
				t.Errorf("--http-management-port support read as %v, want %v", got, want.httpManagementPort)
			}
		})
	}
}

// A definition is at the start of a line; a mention inside a description is
// not. Every version's help refers to `db-url` in prose, and 26.5 mentions
// several option names mid-sentence — none of those are options of `export`.
func TestParseOptions_IgnoresProseMentions(t *testing.T) {
	opts := ParseOptions(`
--db-url <jdbc-url>  The full database JDBC URL. If not provided, a default URL is set based on
                       the selected vendor. Ignored when --http-port is set.
--realm <realm>      Set the name of the realm to export.
`)
	if !opts.Has("db-url") || !opts.Has("realm") {
		t.Fatalf("the two definitions should both be found: %v", opts)
	}
	if opts.Has("http-port") {
		t.Error("an option named inside a description is not an option of this command")
	}
}

// Where kc.sh could not be asked, no port option is emitted. Passing one it
// rejects fails every capture; omitting one it would have taken risks a bind
// conflict only when something is already listening.
func TestBuildExport_OmitsPortsWhenSupportIsUnknown(t *testing.T) {
	cmd, err := BuildExport(ExportRequest{
		KcPath: "kc.sh", Dir: "/tmp/pc", Realm: "acme",
		Ports: Ports{HTTP: 41823, HTTPS: 41824, Management: 41825},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cmd.String(), "-port") {
		t.Errorf("no port option should be guessed at: %s", cmd.String())
	}
	if cmd.PortsPassed {
		t.Error("PortsPassed must be false when no port reached the command line")
	}
}

// 26.5 takes no port option at all, and the built command has to reflect that
// rather than pass the one 26.3 took.
func TestBuildExport_AgainstEachRealVersion(t *testing.T) {
	for name, want := range helpFixtures {
		t.Run(name, func(t *testing.T) {
			cmd, err := BuildExport(ExportRequest{
				KcPath: "kc.sh", Dir: "/tmp/pc", Realm: "acme",
				Ports:     Ports{HTTP: 41823, HTTPS: 41824, Management: 41825},
				Supported: ParseOptions(readFixture(t, name)),
			})
			if err != nil {
				t.Fatal(err)
			}
			got := cmd.String()
			if strings.Contains(got, "--http-port") || strings.Contains(got, "--https-port") {
				t.Errorf("an option this Keycloak rejects reached the command line: %s", got)
			}
			if has := strings.Contains(got, "--http-management-port 41825"); has != want.httpManagementPort {
				t.Errorf("management port present = %v, want %v: %s", has, want.httpManagementPort, got)
			}
			if cmd.PortsPassed != want.httpManagementPort {
				t.Errorf("PortsPassed = %v, want %v", cmd.PortsPassed, want.httpManagementPort)
			}
		})
	}
}

func TestBuildImport_UsesTheImportCommandsOwnOptions(t *testing.T) {
	cmd, err := BuildImport(ImportRequest{
		KcPath: "kc.sh", Dir: "/tmp/in", Strategy: StrategyOverwrite,
		Ports:     Ports{HTTP: 1, HTTPS: 2, Management: 3},
		Supported: OptionSet{"dir": true, "override": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cmd.String(), "-port") {
		t.Errorf("this import command takes no port option: %s", cmd.String())
	}
}

// The wording Keycloak uses here matches neither of the "unknown option"
// phrasings, so a rejected flag used to be reported as "exited with code 2".
func TestClassifyFailure_NamesARejectedOption(t *testing.T) {
	const stderr = `Option: '--http-port' not valid for command export
Possible solutions: --http-access-log-enabled, --http-management-port
Try 'kc.sh export --help' for more information on the available options.`

	o := ParseOutput("", stderr)
	o.ExitCode = 2
	if o.RejectedOption != "--http-port" || o.RejectedCommand != "export" {
		t.Fatalf("the rejected option was not recognised: %q on %q", o.RejectedOption, o.RejectedCommand)
	}

	message, advice, retryable := ClassifyFailure("acme", o, stderr)
	if retryable {
		t.Error("a rejected option is the same every time; retrying it is a loop")
	}
	if !strings.Contains(message, "--http-port") || !strings.Contains(message, "export") {
		t.Errorf("the message should name the option and the command: %q", message)
	}
	if advice == "" {
		t.Error("a rejected option needs advice; it is not something an operator can guess at")
	}
}

// kc.sh puts this line on stdout on some releases, and the classifier reads
// both streams for exactly that reason.
func TestParseOutput_FindsARejectedOptionOnStdout(t *testing.T) {
	o := ParseOutput("Option: '--https-port' not valid for command import\n", "")
	if o.RejectedOption != "--https-port" || o.RejectedCommand != "import" {
		t.Fatalf("stdout was not searched: %q on %q", o.RejectedOption, o.RejectedCommand)
	}
}

func TestBuildHelp(t *testing.T) {
	cmd, err := BuildHelp("/opt/keycloak/bin/kc.sh", "export")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cmd.String(), "/opt/keycloak/bin/kc.sh export --help-all"; got != want {
		t.Errorf("built %q, want %q", got, want)
	}
	if _, err := BuildHelp("", "export"); err == nil {
		t.Error("a help command with no kc.sh path was accepted")
	}
	if _, err := BuildHelp("kc.sh", ""); err == nil {
		t.Error("a help command with no subcommand was accepted")
	}
}
