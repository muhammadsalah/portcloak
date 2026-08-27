package config

import (
	"fmt"
	"sort"
	"strings"
)

// Problem is one thing wrong with a configuration file, expressed as a sentence
// about the operator's file rather than as a field-tag violation.
type Problem struct {
	// Line is where in config.yaml the problem is, or 0 if it could not be
	// located — which happens for a problem about the file as a whole.
	Line int `json:"line"`
	// Path is the location in the document, e.g. environments[2].host.
	Path string `json:"path"`
	// Message is the sentence shown to the operator.
	Message string `json:"message"`
}

func (p Problem) Error() string {
	if p.Line > 0 {
		return fmt.Sprintf("line %d: %s", p.Line, p.Message)
	}
	return p.Message
}

// ValidationError carries every problem at once. Reporting the first one and
// stopping makes an operator fix a file one error per run.
type ValidationError struct {
	File     string
	Problems []Problem
}

func (v *ValidationError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s has %d problem", v.File, len(v.Problems))
	if len(v.Problems) != 1 {
		b.WriteByte('s')
	}
	b.WriteString(":")
	for _, p := range v.Problems {
		b.WriteString("\n  ")
		b.WriteString(p.Error())
	}
	return b.String()
}

// Validate checks the whole configuration and returns every problem it finds.
// locate maps a document path to a line number and may be nil.
func Validate(c *Config, locate func(path string) int) []Problem {
	var problems []Problem
	add := func(path, format string, args ...any) {
		line := 0
		if locate != nil {
			line = locate(path)
		}
		problems = append(problems, Problem{Line: line, Path: path, Message: fmt.Sprintf(format, args...)})
	}

	seenEnv := map[string]int{}
	for i, e := range c.Environments {
		base := fmt.Sprintf("environments[%d]", i)
		validateEnvironment(e, base, add)
		if e.Name == "" {
			continue
		}
		if prev, dup := seenEnv[e.Name]; dup {
			add(base+".name", "two environments are both named %q; the one at entry %d and this one. Names have to be unique because that is how a capture refers to them.", e.Name, prev+1)
		}
		seenEnv[e.Name] = i
	}

	seenStore := map[string]int{}
	defaults := 0
	for i, s := range c.Storage {
		base := fmt.Sprintf("storage[%d]", i)
		validateStorage(s, base, add)
		if s.Default {
			defaults++
		}
		if s.Name == "" {
			continue
		}
		if prev, dup := seenStore[s.Name]; dup {
			add(base+".name", "two storage definitions are both named %q; the one at entry %d and this one.", s.Name, prev+1)
		}
		seenStore[s.Name] = i
	}
	if defaults > 1 {
		add("storage", "%d storage definitions are marked default. Exactly one can be, because it is what a new capture pre-selects.", defaults)
	}

	sort.SliceStable(problems, func(i, j int) bool { return problems[i].Line < problems[j].Line })
	return problems
}

type addFunc func(path, format string, args ...any)

func validateEnvironment(e Environment, base string, add addFunc) {
	if strings.TrimSpace(e.Name) == "" {
		add(base+".name", "this environment has no name. PortCloak refers to environments by name everywhere, so it needs one.")
	}
	switch e.Kind {
	case EnvLocal:
		if e.ServerFolder == "" {
			add(base+".serverFolder", "the local environment %q does not say where Keycloak is installed. Point serverFolder at the install root — the folder containing bin/kc.sh.", e.Name)
		}
		rejectForeign(e, base, add, "host", "namespace", "container", "context", "workload", "dockerEndpoint")
	case EnvSSH:
		if e.Host == "" {
			add(base+".host", "the SSH environment %q has no host.", e.Name)
		}
		if e.User == "" {
			add(base+".user", "the SSH environment %q has no user to connect as.", e.Name)
		}
		if e.ServerFolder == "" {
			add(base+".serverFolder", "the SSH environment %q does not say where Keycloak is installed on %s.", e.Name, e.Host)
		}
		if e.Auth != "" && !validSSHAuth(e.Auth) {
			add(base+".auth", "%q is not an SSH auth method. Use key, agent or password.", e.Auth)
		}
		if e.JumpHost != nil && e.JumpHost.Host == "" {
			add(base+".jumpHost.host", "the jump host for %q has no host.", e.Name)
		}
		rejectForeign(e, base, add, "namespace", "container", "context", "workload", "dockerEndpoint")
	case EnvDocker:
		if e.Container == "" {
			add(base+".container", "the Docker environment %q does not name the container or service running Keycloak.", e.Name)
		}
		if e.Runtime != "" && e.Runtime != "docker" && e.Runtime != "podman" && e.Runtime != "nerdctl" {
			add(base+".runtime", "%q is not a container runtime PortCloak knows. Use docker, podman or nerdctl.", e.Runtime)
		}
		rejectForeign(e, base, add, "host", "namespace", "context", "workload", "serverFolder")
	case EnvKubernetes:
		if e.Namespace == "" {
			add(base+".namespace", "the Kubernetes environment %q has no namespace.", e.Name)
		}
		if e.Workload == "" {
			add(base+".workload", "the Kubernetes environment %q does not name the Deployment or StatefulSet running Keycloak. Use the form deployment/keycloak or statefulset/keycloak.", e.Name)
		} else if !validWorkloadRef(e.Workload) {
			add(base+".workload", "%q is not a workload reference PortCloak understands. Use deployment/<name> or statefulset/<name>.", e.Workload)
		}
		rejectForeign(e, base, add, "host", "container", "serverFolder", "dockerEndpoint")
	case "":
		add(base+".kind", "the environment %q has no kind. It has to be one of local, ssh, docker or kubernetes.", e.Name)
	default:
		add(base+".kind", "%q is not an environment kind PortCloak knows. It has to be one of local, ssh, docker or kubernetes.", e.Kind)
	}

	if e.CredentialRef != "" && !ValidHandle(e.CredentialRef) {
		add(base+".credentialRef", "%q is not a credential handle. Handles look like keychain://portcloak/<kind>/<name> and the value itself stays in this machine's keychain.", e.CredentialRef)
	}
	if e.AdminCredentialRef != "" && !ValidHandle(e.AdminCredentialRef) {
		add(base+".adminCredentialRef", "%q is not a credential handle.", e.AdminCredentialRef)
	}
}

func validateStorage(s Storage, base string, add addFunc) {
	if strings.TrimSpace(s.Name) == "" {
		add(base+".name", "this storage definition has no name.")
	}
	switch s.Kind {
	case StoreDisk:
		if s.Folder == "" {
			add(base+".folder", "the disk storage %q has no root folder.", s.Name)
		}
		rejectForeignStorage(s, base, add, "host", "bucket", "container", "endpoint")
	case StoreSSH:
		if s.Host == "" {
			add(base+".host", "the SSH storage %q has no host.", s.Name)
		}
		if s.User == "" {
			add(base+".user", "the SSH storage %q has no user to connect as.", s.Name)
		}
		if s.Folder == "" {
			add(base+".folder", "the SSH storage %q has no remote folder to write into.", s.Name)
		}
		if s.Auth != "" && !validSSHAuth(s.Auth) {
			add(base+".auth", "%q is not an SSH auth method. Use key, agent or password.", s.Auth)
		}
		rejectForeignStorage(s, base, add, "bucket", "container", "endpoint")
	case StoreS3:
		if s.Bucket == "" {
			add(base+".bucket", "the S3 storage %q has no bucket.", s.Name)
		}
		if s.Region == "" && s.Endpoint == "" {
			add(base+".region", "the S3 storage %q needs a region, an endpoint, or both. Without either, the SDK has nowhere to send a request.", s.Name)
		}
		if s.PartSizeMB != 0 && s.PartSizeMB < 5 {
			add(base+".partSizeMb", "an S3 multipart part has to be at least 5 MB; %q asks for %d.", s.Name, s.PartSizeMB)
		}
		rejectForeignStorage(s, base, add, "folder", "container")
	case StoreAzure:
		if s.Container == "" {
			add(base+".container", "the Azure storage %q has no container.", s.Name)
		}
		if s.Account == "" && s.Endpoint == "" {
			add(base+".account", "the Azure storage %q needs an account name or an endpoint. Point endpoint at Azurite's dev endpoint to use the emulator.", s.Name)
		}
		rejectForeignStorage(s, base, add, "folder", "bucket")
	case "":
		add(base+".kind", "the storage %q has no kind. It has to be one of disk, ssh, s3 or azure.", s.Name)
	default:
		add(base+".kind", "%q is not a storage kind PortCloak knows. It has to be one of disk, ssh, s3 or azure.", s.Kind)
	}

	if s.CredentialRef != "" && !ValidHandle(s.CredentialRef) {
		add(base+".credentialRef", "%q is not a credential handle. Handles look like keychain://portcloak/<kind>/<name>.", s.CredentialRef)
	}
}

// rejectForeign catches a field that belongs to a different kind. It is a real
// mistake worth naming: an SSH environment carrying a namespace is almost
// always a half-finished edit, and silently ignoring it makes the file lie.
func rejectForeign(e Environment, base string, add addFunc, fields ...string) {
	present := map[string]bool{
		"host":           e.Host != "",
		"namespace":      e.Namespace != "",
		"container":      e.Container != "",
		"context":        e.Context != "",
		"workload":       e.Workload != "",
		"serverFolder":   e.ServerFolder != "",
		"dockerEndpoint": e.DockerEndpoint != "",
	}
	for _, f := range fields {
		if present[f] {
			add(base+"."+f, "a %s environment has no %s. Remove it, or change the kind.", e.Kind, f)
		}
	}
}

func rejectForeignStorage(s Storage, base string, add addFunc, fields ...string) {
	present := map[string]bool{
		"host":      s.Host != "",
		"bucket":    s.Bucket != "",
		"container": s.Container != "",
		"endpoint":  s.Endpoint != "",
		"folder":    s.Folder != "",
	}
	for _, f := range fields {
		if present[f] {
			add(base+"."+f, "a %s storage has no %s. Remove it, or change the kind.", s.Kind, f)
		}
	}
}

func validSSHAuth(a SSHAuth) bool {
	return a == SSHKey || a == SSHAgent || a == SSHPassword
}

func validWorkloadRef(ref string) bool {
	kind, name, ok := strings.Cut(ref, "/")
	if !ok || name == "" {
		return false
	}
	switch strings.ToLower(kind) {
	case "deployment", "deployments", "statefulset", "statefulsets":
		return true
	}
	return false
}
