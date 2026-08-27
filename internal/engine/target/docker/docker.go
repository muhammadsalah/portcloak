// Package docker captures from a containerised Keycloak through an ephemeral
// clone.
//
// The serving container is only ever inspected. Everything PortCloak runs
// happens inside a throwaway copy created from the same image and configuration
// — which is what makes "never disturb the serving instance" structural rather
// than a matter of being careful.
package docker

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/kc"
	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/target"
	"portcloak/internal/engine/target/clone"
)

// Platform is the Docker implementation of the clone platform.
type Platform struct {
	env config.Environment
	cli *client.Client
}

// NewPlatform connects to the Docker endpoint.
func NewPlatform(env config.Environment) (*Platform, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	if env.DockerEndpoint != "" {
		opts = append(opts, client.WithHost(env.DockerEndpoint))
	} else {
		opts = append(opts, client.FromEnv)
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, resil.Fatal("connect to Docker",
			fmt.Sprintf("PortCloak could not talk to Docker at %s.", endpointLabel(env)), err).
			WithAdvice("Check the endpoint, and that this account can use the Docker socket.")
	}
	return &Platform{env: env, cli: cli}, nil
}

// NewExecutor builds the target adapter.
func NewExecutor(env config.Environment) (*clone.Executor, error) {
	p, err := NewPlatform(env)
	if err != nil {
		return nil, err
	}
	return clone.NewExecutor(p), nil
}

func endpointLabel(env config.Environment) string {
	if env.DockerEndpoint != "" {
		return env.DockerEndpoint
	}
	return "the default endpoint"
}

// Close releases the client.
func (p *Platform) Close() error { return p.cli.Close() }

// Probe inspects the serving container read-only and reports what a capture
// would find. It never creates the clone it reports as feasible.
func (p *Platform) Probe(ctx context.Context) (target.TargetFacts, error) {
	facts := target.TargetFacts{
		Kind:         string(config.EnvDocker),
		Mode:         target.ModeEphemeralClone,
		ProbedAt:     time.Now(),
		ReadOnlyNote: "Nothing was created or changed. The probe inspects the serving container and reads nothing else.",
	}

	if _, err := p.cli.Ping(ctx); err != nil {
		facts.Fail("Docker endpoint", fmt.Sprintf("%s — %v", endpointLabel(p.env), err),
			"Check that Docker is running and that this account can use its socket.")
		return facts, nil
	}
	facts.Reachable = true
	facts.Pass("Docker endpoint", endpointLabel(p.env))

	info, err := p.cli.ContainerInspect(ctx, p.env.Container)
	if err != nil {
		facts.Fail("Serving container", fmt.Sprintf("%s — not found", p.env.Container),
			"Name the container or service running Keycloak. A container that is not running can still be selected, but a capture needs it to exist.")
		return facts, nil
	}
	state := "stopped"
	if info.State != nil && info.State.Running {
		state = "running"
	}
	facts.Pass("Serving container", fmt.Sprintf("%s · %s · %s", strings.TrimPrefix(info.Name, "/"), info.Config.Image, state))

	// A clone is created from the image the serving container already uses, so
	// the image being absent locally is the one thing that would make clone
	// creation fail — and it is worth reporting before a capture starts.
	if _, err := p.cli.ImageInspect(ctx, info.Config.Image); err != nil {
		facts.Fail("Image", fmt.Sprintf("%s is not present on this Docker host", info.Config.Image),
			"A clone is created from the serving container's own image. Pull it, or capture from a host that has it.")
		return facts, nil
	}
	facts.CloneCapable = true
	facts.CloneDetail = "can be created from " + info.Config.Image
	facts.Pass("Ephemeral clone", facts.CloneDetail)

	kcPath := kcPathIn(p.env.KcPath, info.Config.Env)
	facts.KcPath = kcPath
	if p.env.KcPath != "" {
		facts.Pass("kc.sh", kcPath+" (set on this environment)")
	} else {
		facts.Pass("kc.sh", kcPath)
	}

	if v := p.readVersion(ctx, info.ID, kcPath); v != "" {
		facts.KeycloakVersion = v
		facts.Pass("Keycloak version", v)
	} else if v := versionFromImage(info.Config.Image); v != "" {
		facts.KeycloakVersion = v
		facts.Warn("Keycloak version", v+" (read from the image tag)",
			"PortCloak could not ask the container directly, so this is the tag rather than a confirmed version.")
	} else {
		facts.Warn("Keycloak version", "could not be determined",
			"The export will still run.")
	}

	facts.TempDir = "/tmp"
	facts.HasTar = true
	facts.Ports = target.PortSet{HTTP: 8080, HTTPS: 8443, Management: 9000}
	facts.Pass("Free ports", "the clone has its own network namespace, so nothing can collide")

	return facts, nil
}

// readVersion asks the serving container for its Keycloak version.
//
// This is the one place the serving container is exec'd into, and it is a
// read-only `--version` on the probe path only. A capture never execs into it.
func (p *Platform) readVersion(ctx context.Context, containerID, kcPath string) string {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	res, err := p.exec(ctx, containerID, target.Command{Path: kcPath, Args: []string{"--version"}})
	if err != nil {
		return ""
	}
	return kc.ParseVersion(res.Stdout, res.Stderr)
}

func versionFromImage(ref string) string {
	_, tag, ok := strings.Cut(ref, ":")
	if !ok {
		return ""
	}
	return kc.ParseVersion(tag)
}

// kcPathIn decides which kc.sh the export will run.
//
// A path set on the environment wins outright: it is the operator saying where
// their own image puts it, and a custom build is exactly the case the two
// guesses below get wrong.
func kcPathIn(configured string, env []string) string {
	if configured != "" {
		return configured
	}
	for _, e := range env {
		if home, ok := strings.CutPrefix(e, "KEYCLOAK_HOME="); ok {
			return path.Join(home, "bin", "kc.sh")
		}
	}
	return "/opt/keycloak/bin/kc.sh"
}

// Inspect reads the serving container and derives a clone spec from it.
func (p *Platform) Inspect(ctx context.Context, jobID string, realms []string) (clone.Spec, error) {
	info, err := p.cli.ContainerInspect(ctx, p.env.Container)
	if err != nil {
		return clone.Spec{}, resil.Fatal("read the serving container",
			fmt.Sprintf("PortCloak could not inspect %s.", p.env.Container), err)
	}

	env := map[string]string{}
	for _, e := range info.Config.Env {
		if k, v, ok := strings.Cut(e, "="); ok {
			env[k] = v
		}
	}

	return clone.Spec{
		JobID:   jobID,
		Image:   info.Config.Image,
		Env:     env,
		WorkDir: target.WorkDirFor(jobID),
	}, nil
}

// Create materialises the clone.
func (p *Platform) Create(ctx context.Context, spec clone.Spec) (string, error) {
	info, err := p.cli.ContainerInspect(ctx, p.env.Container)
	if err != nil {
		return "", resil.Fatal("read the serving container",
			fmt.Sprintf("PortCloak could not inspect %s.", p.env.Container), err)
	}

	envList := make([]string, 0, len(spec.Env))
	for k, v := range spec.Env {
		envList = append(envList, k+"="+v)
	}
	sort.Strings(envList)

	cfg := &container.Config{
		Image: spec.Image,
		Env:   envList,
		// The entrypoint is replaced by a hang, so the clone boots nothing and
		// serves nothing.
		Entrypoint: strslice(spec.Command),
		Cmd:        nil,
		Labels:     spec.Labels,
		// No exposed ports and no health check: a clone that answered a health
		// check or published a port would be a clone that could receive traffic.
		ExposedPorts: nil,
		Healthcheck:  &container.HealthConfig{Test: []string{"NONE"}},
		User:         info.Config.User,
		WorkingDir:   info.Config.WorkingDir,
		Tty:          false,
	}

	host := &container.HostConfig{
		// No port bindings, ever.
		PortBindings: nil,
		AutoRemove:   false,
		// The serving container's mounts travel because they may hold the
		// database TLS material the clone needs to connect.
		Binds:       info.HostConfig.Binds,
		Mounts:      info.HostConfig.Mounts,
		CapAdd:      info.HostConfig.CapAdd,
		CapDrop:     info.HostConfig.CapDrop,
		SecurityOpt: info.HostConfig.SecurityOpt,
		Resources:   info.HostConfig.Resources,
		// The clone joins the same networks so the database is reachable, but
		// never inherits an alias — see the network config below.
		NetworkMode:   info.HostConfig.NetworkMode,
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyDisabled},
	}
	if info.HostConfig.ReadonlyRootfs {
		host.ReadonlyRootfs = true
		// A read-only root filesystem needs somewhere for the export to land.
		host.Tmpfs = map[string]string{spec.WorkDir: "rw,mode=1700"}
	}

	// This is the Docker form of the label trap. A network alias inherited from
	// the serving container would let service discovery route real traffic into
	// a container that serves nothing, so aliases are stripped while network
	// membership is kept.
	netCfg := &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{}}
	if info.NetworkSettings != nil {
		for name := range info.NetworkSettings.Networks {
			netCfg.EndpointsConfig[name] = &network.EndpointSettings{Aliases: nil}
		}
	}

	created, err := p.cli.ContainerCreate(ctx, cfg, host, netCfg, nil, clone.Name(spec.JobID))
	if err != nil {
		return "", resil.Fatal("create the ephemeral clone",
			fmt.Sprintf("Docker refused to create the clone: %v", err), err).
			WithAdvice("Nothing was left behind. Check that this account can create containers on the endpoint.")
	}
	ref := "container/" + created.ID[:12]

	if err := p.cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		// The container exists even though it did not start, so it is removed
		// here rather than left for the sweep.
		_ = p.cli.ContainerRemove(ctx, created.ID, container.RemoveOptions{Force: true})
		return "", resil.Fatal("start the ephemeral clone",
			fmt.Sprintf("The clone was created but would not start: %v", err), err)
	}
	return "container/" + created.ID, ref2(ref, err)
}

func ref2(ref string, err error) error { return nil }

// WaitRunning waits for the clone to report itself running.
func (p *Platform) WaitRunning(ctx context.Context, ref string) error {
	id := containerID(ref)
	deadline := time.Now().Add(2 * time.Minute)
	for {
		info, err := p.cli.ContainerInspect(ctx, id)
		if err != nil {
			return resil.Retry("wait for the ephemeral clone",
				"PortCloak lost sight of the clone while waiting for it to start.", err)
		}
		if info.State != nil && info.State.Running {
			return nil
		}
		if info.State != nil && info.State.ExitCode != 0 && info.State.Status == "exited" {
			return resil.Fatal("wait for the ephemeral clone",
				fmt.Sprintf("The clone exited immediately with code %d.", info.State.ExitCode), nil).
				WithAdvice("The image may not have a shell for the hang command PortCloak uses.")
		}
		if time.Now().After(deadline) {
			return resil.Fatal("wait for the ephemeral clone",
				"The clone did not start within two minutes.", nil)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// Exec runs a command inside the clone, streaming output live.
func (p *Platform) Exec(ctx context.Context, ref string, cmd target.Command) (target.ExecResult, error) {
	return p.exec(ctx, containerID(ref), cmd)
}

func (p *Platform) exec(ctx context.Context, id string, cmd target.Command) (target.ExecResult, error) {
	argv := append([]string{cmd.Path}, cmd.Args...)
	envList := make([]string, 0, len(cmd.Env))
	for k, v := range cmd.Env {
		envList = append(envList, k+"="+v)
	}
	sort.Strings(envList)

	created, err := p.cli.ContainerExecCreate(ctx, id, container.ExecOptions{
		Cmd: argv, Env: envList, WorkingDir: cmd.Dir,
		AttachStdout: true, AttachStderr: true,
	})
	if err != nil {
		return target.ExecResult{}, resil.Retry("run the command",
			"PortCloak could not start a command inside the clone.", err)
	}

	attached, err := p.cli.ContainerExecAttach(ctx, created.ID, container.ExecStartOptions{})
	if err != nil {
		return target.ExecResult{}, resil.Retry("run the command",
			"PortCloak could not attach to the command inside the clone.", err)
	}
	defer attached.Close()

	started := time.Now()
	outPipe, errPipe := newLineWriter(cmd.OnStdout), newLineWriter(cmd.OnStderr)
	// Docker multiplexes both streams over one connection; stdcopy splits them.
	if _, err := stdcopy.StdCopy(outPipe, errPipe, attached.Reader); err != nil && !errors.Is(err, io.EOF) {
		return target.ExecResult{}, resil.Retry("run the command",
			"The connection to the clone dropped while the command was running.", err)
	}
	outPipe.flush()
	errPipe.flush()

	inspect, err := p.cli.ContainerExecInspect(ctx, created.ID)
	if err != nil {
		return target.ExecResult{}, resil.Retry("run the command",
			"PortCloak could not read how the command ended.", err)
	}
	return target.ExecResult{
		ExitCode: inspect.ExitCode,
		Stdout:   outPipe.String(),
		Stderr:   errPipe.String(),
		Duration: time.Since(started),
	}, nil
}

// CopyOut streams a directory out of the clone while it is still alive.
//
// The Engine API produces its own tar stream, so an image without tar — a
// distroless base, for instance — is survivable.
func (p *Platform) CopyOut(ctx context.Context, ref, dir string, sink target.ArtifactSink) error {
	rc, _, err := p.cli.CopyFromContainer(ctx, containerID(ref), dir)
	if err != nil {
		if client.IsErrNotFound(err) {
			// A directory that is not there is not an empty capture. Reporting
			// success here would produce a snapshot that looks complete and
			// contains nothing, which is the fidelity-loss failure this tool
			// exists to prevent.
			return resil.Fatal("collect the exported files",
				fmt.Sprintf("%s does not exist inside the clone, so there is nothing to collect.", dir), err).
				WithAdvice("The export wrote nothing. Its output is in the job log.")
		}
		return resil.Retry("collect the exported files",
			fmt.Sprintf("PortCloak could not read %s out of the clone.", dir), err)
	}
	defer rc.Close() //nolint:errcheck

	base := path.Base(strings.TrimRight(dir, "/"))
	tr := tar.NewReader(rc)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return resil.Retry("collect the exported files",
				"The stream from the clone ended unexpectedly.", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		// Docker roots its tar at the copied directory's own name.
		name := strings.TrimPrefix(strings.TrimPrefix(hdr.Name, base), "/")
		if name == "" {
			continue
		}
		if err := sink.Artifact(ctx, target.Artifact{Name: name, Size: hdr.Size, Mode: hdr.Mode}, tr); err != nil {
			return err
		}
	}
}

// CopyIn writes a file into the clone, for the restore path.
func (p *Platform) CopyIn(ctx context.Context, ref, dest string, size int64, owner clone.FileOwner, r io.Reader) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	// The uid and gid are the clone's own. CopyToContainer unpacks as root and
	// honours them, so the default zeroes would land the realm as root:root
	// 0600 — arriving successfully and then unreadable by the kc.sh that has to
	// import it. 0600 is kept: the file holds unmasked secrets.
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg, Name: path.Base(dest), Size: size, Mode: 0o600,
		Uid: owner.UID, Gid: owner.GID,
	}); err != nil {
		return err
	}
	if _, err := io.Copy(tw, r); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	err := p.cli.CopyToContainer(ctx, containerID(ref), path.Dir(dest), &buf,
		container.CopyToContainerOptions{})
	if err != nil {
		return resil.Retry("send the snapshot",
			fmt.Sprintf("PortCloak could not write %s into the clone.", dest), err)
	}
	return nil
}

// Destroy removes the clone. A clone that is already gone is success: the
// desired end state has been reached either way.
func (p *Platform) Destroy(ctx context.Context, ref string) error {
	err := p.cli.ContainerRemove(ctx, containerID(ref), container.RemoveOptions{Force: true, RemoveVolumes: true})
	if err != nil && !client.IsErrNotFound(err) {
		return err
	}
	return nil
}

// FindOrphans lists containers carrying PortCloak's own label.
func (p *Platform) FindOrphans(ctx context.Context) ([]target.Orphan, error) {
	args := filters.NewArgs()
	args.Add("label", target.LabelEphemeral)

	list, err := p.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: args})
	if err != nil {
		// An environment that could not be checked is reported as unchecked,
		// never as clean.
		return nil, resil.Retry("check for orphaned clones",
			fmt.Sprintf("PortCloak could not list containers on %s, so it cannot say whether any were left behind.", endpointLabel(p.env)), err)
	}

	out := make([]target.Orphan, 0, len(list))
	for _, c := range list {
		out = append(out, target.Orphan{
			Environment: p.env.Name,
			Kind:        string(config.EnvDocker),
			Ref:         "container/" + c.ID,
			JobID:       c.Labels[target.LabelJob],
			CreatedAt:   time.Unix(c.Created, 0),
			State:       c.State,
		})
	}
	return out, nil
}

func containerID(ref string) string {
	return strings.TrimPrefix(ref, "container/")
}

func strslice(s []string) []string {
	out := make([]string, len(s))
	copy(out, s)
	return out
}

// lineWriter turns a byte stream into whole lines for the UI, and keeps the
// full text for the driver to parse afterwards.
type lineWriter struct {
	onLine func(string)
	buf    bytes.Buffer
	all    strings.Builder
}

func newLineWriter(onLine func(string)) *lineWriter {
	return &lineWriter{onLine: onLine}
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.all.Write(p)
	if w.onLine == nil {
		return len(p), nil
	}
	w.buf.Write(p)
	for {
		line, err := w.buf.ReadString('\n')
		if err != nil {
			// Put the partial line back for the next write.
			w.buf.Reset()
			w.buf.WriteString(line)
			break
		}
		w.onLine(strings.TrimRight(line, "\r\n"))
	}
	return len(p), nil
}

func (w *lineWriter) flush() {
	if w.onLine == nil || w.buf.Len() == 0 {
		return
	}
	sc := bufio.NewScanner(&w.buf)
	for sc.Scan() {
		w.onLine(sc.Text())
	}
}

func (w *lineWriter) String() string { return w.all.String() }

// unused keeps the image import honest while the pull path is out of scope for
// 0.0.1: PortCloak never pulls, it only checks that the image is present.
var _ = image.InspectOptions{}
