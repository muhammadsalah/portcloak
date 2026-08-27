// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Package kc builds and interprets kc.sh invocations.
//
// kc.sh is a human-facing CLI, not a stable interface: it writes to both
// streams, exits zero while warning, and changes its banner between versions.
// The driver therefore never parses prose to decide success — it uses the exit
// code and inspects the directory that was produced. Prose parsing is confined
// to pulling out warnings, where being wrong is cosmetic.
package kc

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// UsersMode is how kc.sh export lays out users.
type UsersMode string

const (
	// UsersDifferentFiles splits users across numbered files. It is the
	// default, and it is not an optimisation: it is what makes a 120,000-user
	// realm survivable for the export, for a flaky link, and for the
	// inspection index.
	UsersDifferentFiles UsersMode = "different_files"
	// UsersRealmFile keeps users inside the realm file. Fine for small realms.
	UsersRealmFile UsersMode = "realm_file"
	// UsersSameFile writes one separate users file.
	UsersSameFile UsersMode = "same_file"
	// UsersSkip omits users entirely.
	UsersSkip UsersMode = "skip"
)

// ExportRequest is everything the driver needs to build an export invocation.
type ExportRequest struct {
	// KcPath is the kc.sh (or kc.bat) to run.
	KcPath string
	// Dir is the export directory inside the execution context.
	Dir string
	// Realm is always set: one snapshot holds exactly one realm, so an export
	// that did not name a realm would not be a snapshot PortCloak can produce.
	Realm string
	// UsersMode and UsersPerFile control the user layout.
	UsersMode    UsersMode
	UsersPerFile int
	// Ports are the free ports the embedded runtime binds.
	Ports Ports
	// Supported is what this kc.sh reported its export command accepts. An
	// unset set means discovery did not run or did not answer, and no port
	// option is emitted — see portArgs for why that is the safe direction.
	Supported OptionSet
	// Optimized asks Keycloak to skip the build step where the installation is
	// already built, which most container images are.
	Optimized bool
	// ExtraArgs are passed through verbatim for a version quirk an operator
	// needs to work around.
	ExtraArgs []string
}

// Ports mirrors the target port set without importing it, so kc stays a leaf
// package the target adapters can depend on freely.
type Ports struct {
	HTTP       int
	HTTPS      int
	Management int
}

// OptionSet is the set of option names one kc.sh subcommand accepts.
//
// Which port options `export` takes is not a property of PortCloak, of the
// design spec, or of the Keycloak documentation. It is a property of the
// binary in front of us, and it has changed twice in three minor releases —
// measured, not inferred, from the images in testdata/kc-help:
//
//	24.0        no port options at all
//	25.0–26.3   --http-management-port (and the management TLS options)
//	26.5        no port options at all
//
// `--http-port` and `--https-port` are accepted by neither `export` nor
// `import` on any of them. Passing one aborts the command before it reads the
// realm:
//
//	Option: '--http-port' not valid for command export
//
// So the set is discovered by asking the binary, rather than derived from a
// version table that would be wrong again by the next release.
type OptionSet map[string]bool

// Has reports whether the subcommand accepts an option, named with or without
// its leading dashes.
func (s OptionSet) Has(name string) bool { return s[strings.TrimLeft(name, "-")] }

// Known reports whether discovery produced an answer at all. An empty set
// means "not asked", never "accepts nothing" — the two lead to different
// decisions and collapsing them is how a failed probe turns into a rejected
// flag.
func (s OptionSet) Known() bool { return len(s) > 0 }

// ExportCommand is the built invocation.
type ExportCommand struct {
	Path string
	Args []string
	// PortsPassed records whether any port option made it onto the command
	// line. Where none did, a bind conflict cannot be resolved by reallocating
	// ports, so retrying one would be a loop with no exit.
	PortsPassed bool
}

// BuildHelp builds the invocation that lists a subcommand's options.
//
// `--help-all` rather than `--help`: on every version that has them, the port
// options sit under the additional options. It neither starts a server nor
// triggers a re-augmentation, and returns in well under a second.
func BuildHelp(kcPath, subcommand string) (ExportCommand, error) {
	if kcPath == "" {
		return ExportCommand{}, fmt.Errorf("no kc.sh path was given")
	}
	if subcommand == "" {
		return ExportCommand{}, fmt.Errorf("no kc.sh subcommand was named")
	}
	return ExportCommand{Path: kcPath, Args: []string{subcommand, "--help-all"}}, nil
}

// optionDefRe matches an option where kc.sh *defines* one: at the start of a
// line, optionally behind its short form. Description text is indented and
// wrapped, so a prose mention — "if the `db-url` option is set" — is not a
// definition and does not enter the set.
var optionDefRe = regexp.MustCompile(`(?m)^(?:-[a-zA-Z], )?--([a-z0-9][a-z0-9-]*)`)

// ParseOptions reads the option names out of kc.sh help output. Both streams
// are accepted because kc.sh has used both across releases.
func ParseOptions(streams ...string) OptionSet {
	out := OptionSet{}
	for _, s := range streams {
		for _, m := range optionDefRe.FindAllStringSubmatch(strings.ReplaceAll(s, "\r\n", "\n"), -1) {
			out[m[1]] = true
		}
	}
	return out
}

// portArgs emits the port options this kc.sh actually accepts.
//
// The two failures are not symmetric. Passing an option the subcommand does
// not take is fatal and unconditional — the command exits before it reads the
// realm, on every capture, forever. Omitting one it would have taken risks a
// bind conflict, and only when something is already listening on that port.
// So the flag is omitted whenever there is any doubt, including when discovery
// produced no answer at all.
func portArgs(supported OptionSet, p Ports) []string {
	if !supported.Known() {
		return nil
	}
	var args []string
	for _, o := range []struct {
		flag string
		port int
	}{
		{"http-port", p.HTTP},
		{"https-port", p.HTTPS},
		{"http-management-port", p.Management},
	} {
		if o.port > 0 && supported.Has(o.flag) {
			args = append(args, "--"+o.flag, strconv.Itoa(o.port))
		}
	}
	return args
}

// String renders the command as it would be typed, for the streamed log panel.
func (c ExportCommand) String() string {
	return c.Path + " " + strings.Join(c.Args, " ")
}

// BuildExport produces the invocation from [03 §3.8].
func BuildExport(r ExportRequest) (ExportCommand, error) {
	if r.KcPath == "" {
		return ExportCommand{}, fmt.Errorf("no kc.sh path was given")
	}
	if r.Realm == "" {
		return ExportCommand{}, fmt.Errorf("no realm was named; one snapshot holds exactly one realm")
	}
	if r.Dir == "" {
		return ExportCommand{}, fmt.Errorf("no export directory was given")
	}

	mode := r.UsersMode
	if mode == "" {
		mode = UsersDifferentFiles
	}

	args := []string{"export"}
	if mode == UsersRealmFile {
		// The single-file variant writes one document rather than a directory.
		args = append(args, "--file", path.Join(r.Dir, r.Realm+".json"))
	} else {
		args = append(args, "--dir", r.Dir)
	}
	args = append(args, "--realm", r.Realm, "--users", string(mode))

	if mode == UsersDifferentFiles {
		n := r.UsersPerFile
		if n <= 0 {
			n = 1000
		}
		args = append(args, "--users-per-file", strconv.Itoa(n))
	}

	ports := portArgs(r.Supported, r.Ports)
	args = append(args, ports...)
	if r.Optimized {
		args = append(args, "--optimized")
	}
	args = append(args, r.ExtraArgs...)

	return ExportCommand{Path: r.KcPath, Args: args, PortsPassed: len(ports) > 0}, nil
}

// ImportStrategy is what happens to a resource that already exists.
type ImportStrategy string

const (
	// StrategyOverwrite replaces an existing realm entirely.
	StrategyOverwrite ImportStrategy = "overwrite"
	// StrategySkip creates only what is missing and leaves the rest alone.
	StrategySkip ImportStrategy = "skip"
	// StrategyMerge applies the snapshot over the existing realm.
	StrategyMerge ImportStrategy = "merge"
)

// ImportRequest is everything the driver needs to build an import invocation.
type ImportRequest struct {
	KcPath   string
	Dir      string
	File     string
	Strategy ImportStrategy
	Ports    Ports
	// Supported is what this kc.sh reported its import command accepts. It is
	// discovered per subcommand rather than shared with export: they have
	// matched on every version measured, and assuming they always will is the
	// assumption this whole mechanism exists to stop making.
	Supported OptionSet
	Optimized bool
	ExtraArgs []string
}

// BuildImport produces the offline import invocation.
//
// kc.sh offers OVERWRITE_EXISTING and IGNORE_EXISTING. Merge has no offline
// equivalent — it is partialImport against a running server — so asking for it
// here is a programming error rather than something to silently downgrade.
func BuildImport(r ImportRequest) (ExportCommand, error) {
	if r.KcPath == "" {
		return ExportCommand{}, fmt.Errorf("no kc.sh path was given")
	}
	if r.Dir == "" && r.File == "" {
		return ExportCommand{}, fmt.Errorf("no import directory or file was given")
	}

	args := []string{"import"}
	if r.File != "" {
		args = append(args, "--file", r.File)
	} else {
		args = append(args, "--dir", r.Dir)
	}

	switch r.Strategy {
	case StrategyOverwrite:
		args = append(args, "--override", "true")
	case StrategySkip:
		args = append(args, "--override", "false")
	case StrategyMerge:
		return ExportCommand{}, fmt.Errorf("merge is applied through the Admin API's partialImport, not through kc.sh import")
	case "":
		return ExportCommand{}, fmt.Errorf("no import strategy was chosen")
	default:
		return ExportCommand{}, fmt.Errorf("%q is not an import strategy", r.Strategy)
	}

	ports := portArgs(r.Supported, r.Ports)
	args = append(args, ports...)
	if r.Optimized {
		args = append(args, "--optimized")
	}
	args = append(args, r.ExtraArgs...)

	return ExportCommand{Path: r.KcPath, Args: args, PortsPassed: len(ports) > 0}, nil
}

// StrategyExplanation says what a strategy does to an existing resource, in
// those terms rather than in terms of a Keycloak flag name.
func StrategyExplanation(s ImportStrategy) string {
	switch s {
	case StrategyOverwrite:
		return "A resource that already exists with the same name is replaced by the one from the snapshot."
	case StrategySkip:
		return "A resource that already exists is left exactly as it is. Only genuinely new ones are created."
	case StrategyMerge:
		return "Fields present in the snapshot are applied; fields absent from it keep their destination value."
	default:
		return ""
	}
}

var (
	// Keycloak reports its version on several banner shapes across releases.
	versionRe = regexp.MustCompile(`(?i)keycloak[^\d]{0,20}(\d+\.\d+(?:\.\d+)?)`)
	bareVerRe = regexp.MustCompile(`^\s*(\d+\.\d+(?:\.\d+)?)\s*$`)

	warnRe = regexp.MustCompile(`(?i)^\s*(?:\[?WARN(?:ING)?\]?|WARN)\b[:\s]*(.*)$`)
	errRe  = regexp.MustCompile(`(?i)^\s*(?:\[?ERROR\]?|SEVERE)\b[:\s]*(.*)$`)
)

// ParseVersion pulls a Keycloak version out of `kc.sh --version` output, which
// lands on stdout on some releases and stderr on others.
func ParseVersion(streams ...string) string {
	for _, s := range streams {
		for _, line := range strings.Split(s, "\n") {
			if m := bareVerRe.FindStringSubmatch(line); m != nil {
				return m[1]
			}
			if m := versionRe.FindStringSubmatch(line); m != nil {
				return m[1]
			}
		}
	}
	return ""
}

// Outcome is what the driver made of an invocation.
type Outcome struct {
	ExitCode int
	Warnings []string
	Errors   []string
	// BindConflict marks the one failure that is worth retrying with fresh
	// ports rather than reporting.
	BindConflict bool
	// RejectedOption and RejectedCommand name an option this kc.sh does not
	// take on that subcommand. It is captured separately because the wording
	// matches neither "unknown option" nor "unrecognized option", so it used to
	// fall through to "kc.sh export exited with code 2" — which says nothing
	// about the one thing that has to change.
	RejectedOption  string
	RejectedCommand string
}

// ParseOutput extracts warnings and errors from the two streams. Success is
// never decided here — that is the exit code plus the produced directory.
func ParseOutput(stdout, stderr string) Outcome {
	var out Outcome
	seen := map[string]bool{}
	for _, stream := range []string{stdout, stderr} {
		for _, line := range strings.Split(stream, "\n") {
			line = strings.TrimRight(line, "\r")
			if m := warnRe.FindStringSubmatch(line); m != nil {
				msg := strings.TrimSpace(m[1])
				if msg != "" && !seen["w:"+msg] {
					seen["w:"+msg] = true
					out.Warnings = append(out.Warnings, msg)
				}
				continue
			}
			if m := errRe.FindStringSubmatch(line); m != nil {
				msg := strings.TrimSpace(m[1])
				if msg != "" && !seen["e:"+msg] {
					seen["e:"+msg] = true
					out.Errors = append(out.Errors, msg)
				}
			}
		}
	}
	out.BindConflict = looksLikeBindConflict(stdout) || looksLikeBindConflict(stderr)
	// Both streams, because kc.sh puts this one on stdout on some releases.
	if m := rejectedOptionRe.FindStringSubmatch(stdout + "\n" + stderr); m != nil {
		out.RejectedOption, out.RejectedCommand = m[1], m[2]
	}
	return out
}

// rejectedOptionRe matches how Keycloak reports an option the subcommand does
// not take:
//
//	Option: '--http-port' not valid for command export
var rejectedOptionRe = regexp.MustCompile(`Option: '(--[a-z0-9-]+)' not valid for command ([a-z-]+)`)

func looksLikeBindConflict(s string) bool {
	for _, needle := range []string{
		"Address already in use", "address already in use",
		"java.net.BindException", "Unable to start HTTP server",
		"Port already bound",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// ExportLayout is what an export directory turned out to contain.
type ExportLayout struct {
	// RealmFile is the realm representation, relative to the export directory.
	RealmFile string
	// UserFiles are the per-batch user files, in numeric order.
	UserFiles []string
	// Other is anything else the export produced.
	Other []string
}

// Complete reports whether the layout looks like a finished export.
//
// A truncated export directory is the failure this exists to catch: kc.sh can
// exit zero having written a realm file and then died before the user files,
// and treating that as success would ship a snapshot missing its users.
func (l ExportLayout) Complete() bool { return l.RealmFile != "" }

var userFileRe = regexp.MustCompile(`^(.*)-users-(\d+)\.json$`)

// ReadLayout classifies the files an export produced.
func ReadLayout(realm string, names []string) ExportLayout {
	var l ExportLayout
	type numbered struct {
		name string
		n    int
	}
	var users []numbered

	for _, name := range names {
		base := path.Base(name)
		switch {
		case base == realm+"-realm.json", base == realm+".json":
			l.RealmFile = name
		case userFileRe.MatchString(base):
			m := userFileRe.FindStringSubmatch(base)
			n, _ := strconv.Atoi(m[2])
			users = append(users, numbered{name: name, n: n})
		default:
			l.Other = append(l.Other, name)
		}
	}
	sort.Slice(users, func(i, j int) bool { return users[i].n < users[j].n })
	for _, u := range users {
		l.UserFiles = append(l.UserFiles, u.name)
	}
	sort.Strings(l.Other)
	return l
}

// ClassifyFailure turns a non-zero exit into the sentence an operator can act
// on, rather than "exit status 1".
func ClassifyFailure(realm string, o Outcome, stderr string) (message, advice string, retryable bool) {
	if o.RejectedOption != "" {
		return fmt.Sprintf("This Keycloak's %s command does not accept %s, so it stopped before reading the realm.",
				o.RejectedCommand, o.RejectedOption),
			"PortCloak asks kc.sh which options it takes before building the command, so this means the answer it got did not match the command it ran. Re-test the environment to refresh the probe.",
			false
	}
	if o.BindConflict {
		return "The export could not bind the ports it was given — something claimed them in the moment between PortCloak reserving them and Keycloak starting.",
			"This race is unavoidable and harmless. PortCloak reallocates and tries again.",
			true
	}
	joined := strings.ToLower(stderr + " " + strings.Join(o.Errors, " "))
	switch {
	case strings.Contains(joined, "realm") && strings.Contains(joined, "not found"):
		return fmt.Sprintf("Keycloak does not have a realm called %q.", realm),
			"Check the realm name against the list the probe found on this environment.", false
	case strings.Contains(joined, "no space left"):
		return "The target ran out of disk space partway through the export.",
			"Free space in the export directory, or point the environment at somewhere with more room.", false
	case strings.Contains(joined, "connection") && strings.Contains(joined, "refused"):
		return "The export could not reach the Keycloak database.",
			"The export reads the realm straight from the database, so it needs the same connectivity the server has.", true
	case strings.Contains(joined, "access denied"), strings.Contains(joined, "permission denied"):
		return "The export was refused permission.",
			"Check that the account running kc.sh can read the installation and reach the database.", false
	case strings.Contains(joined, "unrecognized option"), strings.Contains(joined, "unknown option"):
		return "This Keycloak build did not recognise one of the options PortCloak passed.",
			"The environment's Keycloak version may be older than the flags require; the probe records which version was found.", false
	}
	if len(o.Errors) > 0 {
		return o.Errors[0], "", false
	}
	return fmt.Sprintf("kc.sh export exited with code %d.", o.ExitCode), "", false
}
