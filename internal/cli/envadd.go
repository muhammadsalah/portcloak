// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"portcloak/internal/app"
	"portcloak/internal/engine/config"
)

// Defining an environment was left out of the first command line, on the
// reasoning that it is a form with a dozen fields and a connection test behind
// it, and that unlike a key it is not what stands between a headless machine and
// a sealed snapshot.
//
// That was wrong in one direction. A CI job that provisions a throwaway Keycloak
// has to point PortCloak at it before it can capture anything, and without this
// that meant assembling YAML in a shell script and knowing the credential-handle
// naming convention by heart. Hand-editing config.yaml is a supported thing to
// do — it is a plain readable file on purpose — but it is not an interface, and
// describing it as one was the mistake.
//
// The connection-test half of the argument does not hold either: `env probe` is
// already a command, so the test a form runs is a thing a script can run too.

// One subcommand per kind rather than one command with every flag on it. The
// four kinds share barely a field between them — a local install has a folder, a
// Kubernetes workload has a namespace — and a single `env add` would carry
// twenty-five flags of which twenty are wrong for whatever you are doing. Split
// this way, --help lists only what applies.
func newEnvAddCmd(s Streams, g *globals) *cobra.Command {
	c := group("add", "Define an environment to capture from",
		`One subcommand per kind, because the fields have almost nothing in common.

The secret — an SSH password or key passphrase — never comes from an argument,
where ps would show it to every user on the machine. Use --credential-file, or
--credential-stdin, or leave it out and set it later in the app.

Nothing is contacted. Run `+"`pcloak env probe <name>`"+` afterwards to find out whether
the definition works.`)
	c.AddCommand(
		newEnvAddLocalCmd(s, g),
		newEnvAddSSHCmd(s, g),
		newEnvAddDockerCmd(s, g),
		newEnvAddKubernetesCmd(s, g),
	)
	return c
}

// adminFlags are the Admin API fields, which every kind may carry.
//
// The Admin API is optional everywhere: an offline export reads the database
// directly and works against a Keycloak that is not running. It is what verifies
// exported secrets are real values rather than `**********`, and what detects
// themes and provider JARs, so a definition without it captures fine and reports
// less.
type adminFlags struct {
	url        string
	user       string
	realm      string
	clientID   string
	insecure   bool
	secret     string
	secretFile string
	secretIn   bool
	secretAsk  bool
}

func (a *adminFlags) register(c *cobra.Command) {
	f := c.Flags()
	f.StringVar(&a.url, "admin-url", "", "base URL of the Admin API, if it is reachable")
	f.StringVar(&a.user, "admin-user", "", "Admin API user")
	f.StringVar(&a.realm, "admin-realm", "", "realm the admin account lives in (default master)")
	f.StringVar(&a.clientID, "admin-client-id", "", "client id to authenticate with (default admin-cli)")
	f.BoolVar(&a.insecure, "admin-insecure-tls", false, "do not verify the Admin API's TLS certificate")
	f.StringVar(&a.secret, "admin-password", "", "the Admin API password; visible in ps, so prefer one of the three below")
	f.StringVar(&a.secretFile, "admin-password-file", "", "read the Admin API password from this file (- for stdin)")
	f.BoolVar(&a.secretIn, "admin-password-stdin", false, "read the Admin API password from stdin")
	f.BoolVar(&a.secretAsk, "admin-password-prompt", false, "ask for the Admin API password on the terminal, without echo")
	c.MarkFlagsMutuallyExclusive("admin-password", "admin-password-file", "admin-password-stdin", "admin-password-prompt")
}

// given reports whether any source for the Admin password was offered, which is
// what decides whether to store one at all.
func (a *adminFlags) given() bool {
	return a.secret != "" || a.secretFile != "" || a.secretIn || a.secretAsk
}

func (a *adminFlags) apply(e *config.Environment) {
	e.AdminBaseURL = a.url
	e.AdminUser = a.user
	e.AdminRealm = a.realm
	e.AdminClientID = a.clientID
	e.AdminInsecureTLS = a.insecure
}

// credFlags is the definition's own secret: an SSH password, or the passphrase
// on an SSH key.
type credFlags struct {
	value  string
	file   string
	stdin  bool
	prompt bool
}

// register attaches the credential flags with a generic description.
//
// registerAs names the thing being read, so each command's --help says which
// secret it wants rather than "the secret": an S3 store wants both halves of a
// key joined by a colon, an SSH one wants a password. A flag that does not say
// what it holds is a flag somebody fills in wrongly once and debugs twice.
func (cf *credFlags) register(c *cobra.Command) {
	cf.registerAs(c, "the SSH password or key passphrase")
}

func (cf *credFlags) registerAs(c *cobra.Command, what string) {
	f := c.Flags()
	f.StringVar(&cf.value, "credential", "", what+"; visible in ps, so prefer one of the three below")
	f.StringVar(&cf.file, "credential-file", "", "read "+what+" from this file (- for stdin)")
	f.BoolVar(&cf.stdin, "credential-stdin", false, "read "+what+" from stdin")
	f.BoolVar(&cf.prompt, "credential-prompt", false, "ask for "+what+" on the terminal, without echo")
	c.MarkFlagsMutuallyExclusive("credential", "credential-file", "credential-stdin", "credential-prompt")
}

func (cf *credFlags) read(r *run, what string) (string, error) {
	if cf.value != "" {
		warnArgvSecret(r, what)
	}
	if cf.prompt && !isTerminal(r.s.Err) {
		// The rule for the whole surface: every prompt has a flag, and where
		// there is nobody to ask, the refusal names the flag rather than the
		// absence. "There is no terminal to ask on" is true and useless.
		return "", precondition(
			"pcloak: --credential-prompt was given and there is no terminal to ask on.\n" +
				"  Use --credential-file <path>, --credential-stdin, or --credential <value>.")
	}
	// A definition with no secret is ordinary — a local install, a disk folder,
	// an S3 bucket reached through an instance role — so nothing offered and
	// nothing asked for means nothing stored, rather than a prompt.
	return (secretSource{
		value: cf.value, file: cf.file, stdin: cf.stdin, prompt: cf.prompt,
	}).read(r, "Enter "+what, false)
}

// saveEnv is the one write path, shared by all four kinds.
//
// Everything the definition needs is decided before anything is written, so a
// bad flag combination fails without leaving half an environment behind.
func saveEnv(cmd *cobra.Command, s Streams, g *globals, e config.Environment,
	a *adminFlags, cf *credFlags, replace bool) error {

	// Exclusive: this is a read-modify-write of config.yaml, and a concurrent
	// one would silently drop whichever change landed first.
	r, err := open(cmd, g, s, config.LockExclusive)
	if err != nil {
		return err
	}
	defer r.close()

	cfg := app.NewConfigController(r.eng)
	_, exists := r.eng.Config.Config().Environment(e.Name)
	if exists && !replace {
		return precondition(fmt.Sprintf(
			"pcloak: there is already an environment called %q.\n"+
				"  Pass --replace to overwrite it, which is what a re-run of the same script wants.", e.Name))
	}

	secret, err := cf.read(r, "the secret for "+e.Name)
	if err != nil {
		return asPrecondition(err)
	}

	original := ""
	if exists {
		original = e.Name
	}
	if f := cfg.SaveEnvironment(original, e, secret); f != nil {
		return exitWith(ExitFailed, "pcloak: "+f.Message+hintLine(f.Hint))
	}

	// The Admin password is stored under its own handle, and only after the
	// environment exists — SaveAdminCredential resolves the environment by name
	// to work out where to file it.
	if a.given() {
		if a.secret != "" {
			warnArgvSecret(r, "the Admin API password")
		}
		if a.secretAsk && !isTerminal(r.s.Err) {
			return precondition(
				"pcloak: --admin-password-prompt was given and there is no terminal to ask on.\n" +
					"  Use --admin-password-file <path>, --admin-password-stdin, or --admin-password <value>.")
		}
		pass, err := (secretSource{
			value: a.secret, file: a.secretFile, stdin: a.secretIn,
			prompt: a.secretAsk, required: true,
		}).read(r, "Admin API password", false)
		if err != nil {
			return asPrecondition(err)
		}
		if f := cfg.SaveAdminCredential(e.Name, pass); f != nil {
			return exitWith(ExitFailed, "pcloak: "+f.Message)
		}
	}

	verb := "Added"
	if exists {
		verb = "Replaced"
	}
	r.out.Line("%s the %s environment %q.", verb, e.Kind, e.Name)
	r.out.Note("Nothing was contacted. Check it with: pcloak env probe %s", e.Name)
	return nil
}

func newEnvAddLocalCmd(s Streams, g *globals) *cobra.Command {
	var a adminFlags
	var cf credFlags
	var replace bool
	e := config.Environment{Kind: config.EnvLocal}

	c := &cobra.Command{
		Use:   "local <name>",
		Short: "A Keycloak installed on this machine",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e.Name = args[0]
			a.apply(&e)
			return saveEnv(cmd, s, g, e, &a, &cf, replace)
		},
	}
	f := c.Flags()
	f.StringVar(&e.ServerFolder, "server-folder", "", "the install root, the folder containing bin/kc.sh (required)")
	f.StringVar(&e.JavaHome, "java-home", "", "JAVA_HOME to run kc.sh with, if it is not on PATH")
	_ = c.MarkFlagRequired("server-folder")
	a.register(c)
	cf.register(c)
	c.Flags().BoolVar(&replace, "replace", false, "overwrite an environment of this name if one exists")
	return c
}

func newEnvAddSSHCmd(s Streams, g *globals) *cobra.Command {
	var a adminFlags
	var cf credFlags
	var replace bool
	var auth string
	e := config.Environment{Kind: config.EnvSSH}

	c := &cobra.Command{
		Use:   "ssh <name>",
		Short: "A Keycloak on a remote host, reached over SSH",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e.Name = args[0]
			e.Auth = config.SSHAuth(auth)
			a.apply(&e)
			return saveEnv(cmd, s, g, e, &a, &cf, replace)
		},
	}
	f := c.Flags()
	f.StringVar(&e.Host, "host", "", "host to connect to (required)")
	f.StringVar(&e.User, "user", "", "user to connect as (required)")
	f.IntVar(&e.Port, "port", 0, "SSH port (default 22)")
	f.StringVar(&auth, "auth", string(config.SSHAgent), "key, agent or password")
	f.BoolVar(&e.Sudo, "sudo", false, "run kc.sh through sudo")
	f.StringVar(&e.ServerFolder, "server-folder", "", "the install root on the remote host (required)")
	f.StringVar(&e.JavaHome, "java-home", "", "JAVA_HOME on the remote host")
	_ = c.MarkFlagRequired("host")
	_ = c.MarkFlagRequired("user")
	_ = c.MarkFlagRequired("server-folder")
	a.register(c)
	cf.register(c)
	c.Flags().BoolVar(&replace, "replace", false, "overwrite an environment of this name if one exists")
	return c
}

func newEnvAddDockerCmd(s Streams, g *globals) *cobra.Command {
	var a adminFlags
	var cf credFlags
	var replace bool
	e := config.Environment{Kind: config.EnvDocker}

	c := &cobra.Command{
		Use:   "docker <name>",
		Short: "A Keycloak in a container",
		Long: `The container named here is the one PortCloak reads to derive a clone from. It is
never exec'd into: the export runs in an ephemeral copy with the same image and
the same configuration, so the serving container is untouched.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e.Name = args[0]
			a.apply(&e)
			return saveEnv(cmd, s, g, e, &a, &cf, replace)
		},
	}
	f := c.Flags()
	f.StringVar(&e.Container, "container", "", "the container or compose service running Keycloak (required)")
	f.StringVar(&e.DockerEndpoint, "endpoint", "", "Docker endpoint, if not the local socket")
	f.StringVar(&e.Runtime, "runtime", "", "docker, podman or nerdctl (default docker)")
	f.StringVar(&e.Network, "network", "", "network to attach the clone to")
	f.StringVar(&e.KcPath, "kc-path", "", "where kc.sh lives in the image, if it is not the official path")
	_ = c.MarkFlagRequired("container")
	a.register(c)
	cf.register(c)
	c.Flags().BoolVar(&replace, "replace", false, "overwrite an environment of this name if one exists")
	return c
}

func newEnvAddKubernetesCmd(s Streams, g *globals) *cobra.Command {
	var a adminFlags
	var cf credFlags
	var replace bool
	e := config.Environment{Kind: config.EnvKubernetes}

	c := &cobra.Command{
		Use:     "kubernetes <name>",
		Aliases: []string{"k8s", "openshift"},
		Short:   "A Keycloak in a Kubernetes or OpenShift workload",
		Long: `The workload named here is read to derive a Job from. No serving pod is exec'd
into: the export runs in a clone with the workload's own image, environment,
service account and security context, and every inherited selector label
stripped so the production Service cannot route traffic into it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e.Name = args[0]
			a.apply(&e)
			return saveEnv(cmd, s, g, e, &a, &cf, replace)
		},
	}
	f := c.Flags()
	f.StringVar(&e.Namespace, "namespace", "", "namespace the workload runs in (required)")
	f.StringVar(&e.Workload, "workload", "", "deployment/<name> or statefulset/<name> (required)")
	f.StringVar(&e.Context, "context", "", "kubeconfig context, if not the current one")
	f.StringVar(&e.Kubeconfig, "kubeconfig", "", "kubeconfig file, if not the default")
	f.StringVar(&e.ContainerName, "container-name", "", "container within the pod, if it has more than one")
	f.StringVar(&e.KcPath, "kc-path", "", "where kc.sh lives in the image, if it is not the official path")
	_ = c.MarkFlagRequired("namespace")
	_ = c.MarkFlagRequired("workload")
	a.register(c)
	cf.register(c)
	c.Flags().BoolVar(&replace, "replace", false, "overwrite an environment of this name if one exists")
	return c
}

func newEnvRemoveCmd(s Streams, g *globals) *cobra.Command {
	return &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm", "delete"},
		Short:   "Forget an environment, and the credentials behind it",
		Long: `Removes the definition and deletes its keychain entries — the connection secret,
the Admin API password, and a jump host's if it has one.

Snapshots already captured from it are untouched: a snapshot records the
environment it came from as a fact about the past, not as a reference to
something that has to still exist.

An environment a job is currently running against is refused rather than removed
out from under it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := open(cmd, g, s, config.LockExclusive)
			if err != nil {
				return err
			}
			defer r.close()

			envs := app.NewConfigController(r.eng).Load().Environments
			found := false
			for _, e := range envs {
				if e.Name == args[0] {
					found = true
					break
				}
			}
			if !found {
				return notFound("environment", args[0],
					names(envs, func(e app.EnvironmentView) string { return e.Name }))
			}
			if !g.yes && !confirm(r, "Remove the environment %q and its stored credentials?", args[0]) {
				return exitWith(ExitOK, "Left alone.")
			}
			if f := app.NewConfigController(r.eng).DeleteEnvironment(args[0]); f != nil {
				return exitWith(ExitPrecondition, "pcloak: "+f.Message+hintLine(f.Hint))
			}
			r.out.Line("Removed the environment %q.", args[0])
			return nil
		},
	}
}
