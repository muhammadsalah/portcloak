// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Package k8s captures from a clustered Keycloak through an ephemeral clone.
//
// The clone reads the shared database, so it does not matter which replica is
// serving traffic — which removes the "which pod do I pick" confusion the
// cluster case used to have. Sessions, the one genuinely cluster-specific
// complication, are out of scope.
//
// OpenShift travels the same API path. The inherited securityContext and
// service account keep the clone inside the same SCC that already admits the
// serving pod, which is why they are on the keep list rather than normalised
// away.
package k8s

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/kc"
	"portcloak/internal/engine/resil"
	"portcloak/internal/engine/target"
	"portcloak/internal/engine/target/clone"
)

// Platform is the Kubernetes implementation of the clone platform.
type Platform struct {
	env       config.Environment
	clientset kubernetes.Interface
	restCfg   *rest.Config
	namespace string
}

// NewPlatform builds a client from the configured context.
//
// Kubeconfig loading is delegated entirely to client-go's standard rules rather
// than parsed here: contexts, exec-plugin auth, OpenShift token auth and
// proxies vary more than any other input in the tool, and re-implementing that
// resolution is how a supported setup becomes an unsupported one.
func NewPlatform(env config.Environment) (*Platform, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if env.Kubeconfig != "" {
		rules.ExplicitPath = env.Kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{}
	if env.Context != "" {
		overrides.CurrentContext = env.Context
	}

	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)
	restCfg, err := cc.ClientConfig()
	if err != nil {
		return nil, resil.Fatal("connect to the cluster",
			fmt.Sprintf("PortCloak could not build a client for the context %q.", env.Context), err).
			WithAdvice("Check that the context exists in your kubeconfig and that you are logged in.")
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, resil.Fatal("connect to the cluster", "PortCloak could not build a Kubernetes client.", err)
	}

	ns := env.Namespace
	if ns == "" {
		ns, _, _ = cc.Namespace()
	}
	return &Platform{env: env, clientset: clientset, restCfg: restCfg, namespace: ns}, nil
}

// NewExecutor builds the target adapter.
func NewExecutor(env config.Environment) (*clone.Executor, error) {
	p, err := NewPlatform(env)
	if err != nil {
		return nil, err
	}
	return clone.NewExecutor(p), nil
}

// Close releases nothing; the REST client holds no long-lived connection.
func (p *Platform) Close() error { return nil }

// requiredVerbs is the RBAC a capture needs, documented up front and checked
// during probe so a refusal is a sentence at step one rather than a failure at
// step four.
var requiredVerbs = []struct {
	verb     string
	resource string
	sub      string
}{
	{"get", "pods", ""},
	{"list", "pods", ""},
	{"create", "pods", ""},
	{"delete", "pods", ""},
	{"create", "pods", "exec"},
}

// Probe reads the workload spec and checks everything that would stop a
// capture, without creating anything.
func (p *Platform) Probe(ctx context.Context) (target.TargetFacts, error) {
	facts := target.TargetFacts{
		Kind:         string(config.EnvKubernetes),
		Mode:         target.ModeEphemeralClone,
		ProbedAt:     time.Now(),
		ReadOnlyNote: "Nothing was written to the cluster. The probe only reads.",
	}

	if _, err := p.clientset.Discovery().ServerVersion(); err != nil {
		facts.Fail("Cluster", fmt.Sprintf("%s — %v", p.env.Context, err),
			"Check that the context is current and that your credentials have not expired.")
		return facts, nil
	}
	facts.Reachable = true
	facts.Pass("Cluster", p.env.Context)

	if _, err := p.clientset.CoreV1().Namespaces().Get(ctx, p.namespace, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			facts.Fail("Namespace", p.namespace+" — not found",
				"That is the namespace PortCloak looked for on this context.")
			return facts, nil
		}
		if apierrors.IsForbidden(err) {
			// Not being able to read the namespace object is common and not
			// fatal; what matters is the verbs below.
			facts.Warn("Namespace", p.namespace+" — cannot be read directly", "")
		}
	} else {
		facts.Pass("Namespace", p.namespace)
	}

	pod, err := p.templateFor(ctx)
	if err != nil {
		facts.Fail("Workload", fmt.Sprintf("%s — %v", p.env.Workload, err),
			"Name the Deployment or StatefulSet running Keycloak, as deployment/<name> or statefulset/<name>.")
		return facts, nil
	}
	container := p.keycloakContainer(pod)
	if container == nil {
		facts.Fail("Container", "no container found in "+p.env.Workload,
			"For a multi-container pod, name the Keycloak container on the environment.")
		return facts, nil
	}
	facts.Pass("Workload", fmt.Sprintf("%s · %s", p.env.Workload, container.Image))

	// Exactly which verbs are missing, rather than a generic refusal.
	var missing []string
	for _, v := range requiredVerbs {
		allowed, err := p.can(ctx, v.verb, v.resource, v.sub)
		if err != nil {
			facts.Skipped("RBAC", "could not be checked: "+err.Error())
			missing = nil
			break
		}
		if !allowed {
			name := v.verb + " " + v.resource
			if v.sub != "" {
				name = v.verb + " " + v.resource + "/" + v.sub
			}
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		facts.Fail("RBAC", "missing "+strings.Join(missing, ", "),
			fmt.Sprintf("An ephemeral clone needs these verbs in %s. Grant them, or capture instead from a host with database access.", p.namespace))
		return facts, nil
	}
	if len(missing) == 0 {
		facts.Pass("RBAC", "create, get, list and delete on pods, and create on pods/exec")
	}

	// A tight ResourceQuota refuses the clone, and it is far better to say so
	// now than to fail at step four of a capture the operator already started.
	headroom, quotaNote := p.quotaHeadroom(ctx, container)
	switch {
	case quotaNote == "":
		facts.CloneCapable = true
		facts.CloneDetail = "can be created"
		facts.Pass("Ephemeral clone", facts.CloneDetail)
	case headroom:
		facts.CloneCapable = true
		facts.CloneDetail = "can be created · " + quotaNote
		facts.Pass("Ephemeral clone", facts.CloneDetail)
	default:
		facts.Fail("Ephemeral clone", quotaNote,
			"Raise the namespace quota, or override the clone's resources on the environment.")
		return facts, nil
	}

	facts.KcPath = kcPathIn(p.env.KcPath, container)
	if p.env.KcPath != "" {
		facts.Pass("kc.sh", facts.KcPath+" (set on this environment)")
	} else {
		facts.Pass("kc.sh", facts.KcPath)
	}

	if v := versionFromImage(container.Image); v != "" {
		facts.KeycloakVersion = v
		facts.Warn("Keycloak version", v+" (read from the image tag)",
			"PortCloak reads the version from the image rather than execing into the serving pod.")
	} else {
		facts.Warn("Keycloak version", "could not be determined from the image tag", "")
	}

	facts.TempDir = "/tmp"
	// Not assumed any more. The Keycloak image is built on ubi-micro and has no
	// tar; PortCloak streams the export out over the exec channel itself, which
	// is why an image without one is not a problem to report.
	facts.HasTar = false
	facts.Pass("File transfer", "streamed over exec — the clone needs no tar")
	facts.Ports = target.PortSet{HTTP: 8080, HTTPS: 8443, Management: 9000}
	facts.Pass("Free ports", "the clone has its own network namespace, so nothing can collide")

	return facts, nil
}

func (p *Platform) can(ctx context.Context, verb, resourceName, sub string) (bool, error) {
	review := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace:   p.namespace,
				Verb:        verb,
				Resource:    resourceName,
				Subresource: sub,
			},
		},
	}
	res, err := p.clientset.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return false, err
	}
	return res.Status.Allowed, nil
}

// quotaHeadroom reports whether the namespace has room for one more pod's worth
// of requests.
func (p *Platform) quotaHeadroom(ctx context.Context, c *corev1.Container) (ok bool, note string) {
	quotas, err := p.clientset.CoreV1().ResourceQuotas(p.namespace).List(ctx, metav1.ListOptions{})
	if err != nil || len(quotas.Items) == 0 {
		return true, ""
	}

	wantCPU := c.Resources.Requests[corev1.ResourceCPU]
	wantMem := c.Resources.Requests[corev1.ResourceMemory]

	for _, q := range quotas.Items {
		for _, res := range []corev1.ResourceName{"requests.cpu", corev1.ResourceCPU} {
			hard, has := q.Status.Hard[res]
			if !has {
				continue
			}
			used := q.Status.Used[res]
			free := hard.DeepCopy()
			free.Sub(used)
			if free.Cmp(wantCPU) < 0 {
				return false, fmt.Sprintf("the namespace quota %s has %s CPU free and the clone needs %s", q.Name, free.String(), wantCPU.String())
			}
			note = "quota headroom " + free.String() + " CPU"
		}
		for _, res := range []corev1.ResourceName{"requests.memory", corev1.ResourceMemory} {
			hard, has := q.Status.Hard[res]
			if !has {
				continue
			}
			used := q.Status.Used[res]
			free := hard.DeepCopy()
			free.Sub(used)
			if free.Cmp(wantMem) < 0 {
				return false, fmt.Sprintf("the namespace quota %s has %s memory free and the clone needs %s", q.Name, free.String(), wantMem.String())
			}
			note += " / " + free.String()
		}
		if pods, has := q.Status.Hard[corev1.ResourcePods]; has {
			used := q.Status.Used[corev1.ResourcePods]
			free := pods.DeepCopy()
			free.Sub(used)
			if free.CmpInt64(1) < 0 {
				return false, fmt.Sprintf("the namespace quota %s has no pod slots free", q.Name)
			}
		}
	}
	return true, strings.TrimSpace(note)
}

// templateFor reads the serving workload's pod template, read-only.
func (p *Platform) templateFor(ctx context.Context) (*corev1.PodSpec, error) {
	kind, name, ok := strings.Cut(p.env.Workload, "/")
	if !ok {
		return nil, fmt.Errorf("use the form deployment/<name> or statefulset/<name>")
	}
	switch strings.ToLower(strings.TrimSuffix(kind, "s")) {
	case "deployment":
		d, err := p.clientset.AppsV1().Deployments(p.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return d.Spec.Template.Spec.DeepCopy(), nil
	case "statefulset":
		s, err := p.clientset.AppsV1().StatefulSets(p.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return s.Spec.Template.Spec.DeepCopy(), nil
	default:
		return nil, fmt.Errorf("%q is not a workload kind PortCloak reads", kind)
	}
}

func (p *Platform) keycloakContainer(spec *corev1.PodSpec) *corev1.Container {
	if spec == nil || len(spec.Containers) == 0 {
		return nil
	}
	if p.env.ContainerName != "" {
		for i := range spec.Containers {
			if spec.Containers[i].Name == p.env.ContainerName {
				return &spec.Containers[i]
			}
		}
		return nil
	}
	for i := range spec.Containers {
		if strings.Contains(strings.ToLower(spec.Containers[i].Image), "keycloak") {
			return &spec.Containers[i]
		}
	}
	return &spec.Containers[0]
}

// kcPathIn decides which kc.sh the export will run.
//
// A path set on the environment wins outright: it is the operator saying where
// their own image puts it, and a custom build is exactly the case the two
// guesses below get wrong.
func kcPathIn(configured string, c *corev1.Container) string {
	if configured != "" {
		return configured
	}
	for _, e := range c.Env {
		if e.Name == "KEYCLOAK_HOME" && e.Value != "" {
			return path.Join(e.Value, "bin", "kc.sh")
		}
	}
	return "/opt/keycloak/bin/kc.sh"
}

func versionFromImage(ref string) string {
	_, tag, ok := strings.Cut(ref, ":")
	if !ok {
		return ""
	}
	return kc.ParseVersion(tag)
}

// Inspect derives the clone spec from the serving workload.
func (p *Platform) Inspect(ctx context.Context, jobID string, realms []string) (clone.Spec, error) {
	spec, err := p.templateFor(ctx)
	if err != nil {
		return clone.Spec{}, resil.Fatal("read the serving workload",
			fmt.Sprintf("PortCloak could not read %s in %s: %v", p.env.Workload, p.namespace, err), err)
	}
	c := p.keycloakContainer(spec)
	if c == nil {
		return clone.Spec{}, resil.Fatal("read the serving workload",
			"PortCloak could not identify the Keycloak container in that workload.", nil).
			WithAdvice("For a multi-container pod, name the container on the environment.")
	}

	env := map[string]string{}
	for _, e := range c.Env {
		if e.Value != "" {
			env[e.Name] = e.Value
		}
	}
	return clone.Spec{
		JobID:   jobID,
		Image:   c.Image,
		Env:     env,
		WorkDir: target.WorkDirFor(jobID),
	}, nil
}

// Create materialises the clone as a bare pod.
//
// A pod rather than a Job: a Job's pod is owned by a controller that will
// recreate it, which is precisely the behaviour a throwaway execution context
// must not have. The TTL a Job would give as a backstop is replaced by the
// activeDeadlineSeconds below, plus the label sweep on launch.
func (p *Platform) Create(ctx context.Context, spec clone.Spec) (string, error) {
	source, err := p.templateFor(ctx)
	if err != nil {
		return "", resil.Fatal("read the serving workload", err.Error(), err)
	}
	sourceContainer := p.keycloakContainer(source)
	if sourceContainer == nil {
		return "", resil.Fatal("read the serving workload", "no Keycloak container was found", nil)
	}

	c := sourceContainer.DeepCopy()
	c.Name = "keycloak"
	// The command is replaced by a hang. Probes go because a hung container
	// answers none of them, and ports go because a clone must never be
	// addressable.
	c.Command = spec.Command
	c.Args = nil
	c.LivenessProbe = nil
	c.ReadinessProbe = nil
	c.StartupProbe = nil
	c.Lifecycle = nil
	c.Ports = nil

	if p.env.ResourcePreset != nil {
		c.Resources = corev1.ResourceRequirements{Requests: corev1.ResourceList{}, Limits: corev1.ResourceList{}}
		if p.env.ResourcePreset.CPU != "" {
			if q, err := resource.ParseQuantity(p.env.ResourcePreset.CPU); err == nil {
				c.Resources.Requests[corev1.ResourceCPU] = q
				c.Resources.Limits[corev1.ResourceCPU] = q
			}
		}
		if p.env.ResourcePreset.Memory != "" {
			if q, err := resource.ParseQuantity(p.env.ResourcePreset.Memory); err == nil {
				c.Resources.Requests[corev1.ResourceMemory] = q
				c.Resources.Limits[corev1.ResourceMemory] = q
			}
		}
	}

	deadline := int64(6 * 3600)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clone.Name(spec.JobID),
			Namespace: p.namespace,
			// This is the label trap, and the reason the strip list is total.
			// A pod inheriting app=keycloak is picked up by the production
			// Service and receives real user traffic into a container that
			// serves nothing. Only PortCloak's own labels are applied, and no
			// annotation, ownerReference or controller reference travels.
			Labels: spec.Labels,
		},
		Spec: corev1.PodSpec{
			Containers:    []corev1.Container{*c},
			RestartPolicy: corev1.RestartPolicyNever,
			// Kept, because a clone that will not schedule or that an SCC
			// rejects is useless.
			ServiceAccountName:            source.ServiceAccountName,
			ImagePullSecrets:              source.ImagePullSecrets,
			Volumes:                       source.Volumes,
			SecurityContext:               source.SecurityContext,
			NodeSelector:                  source.NodeSelector,
			Tolerations:                   source.Tolerations,
			Affinity:                      source.Affinity,
			DNSPolicy:                     source.DNSPolicy,
			DNSConfig:                     source.DNSConfig,
			TerminationGracePeriodSeconds: source.TerminationGracePeriodSeconds,
			// A backstop for the case where PortCloak's own deletion never runs
			// because the machine it was running on died.
			ActiveDeadlineSeconds: &deadline,
		},
	}
	// Explicitly dropped: nodeName pins the clone to where the serving pod
	// already is, and hostname/subdomain make it addressable.
	pod.Spec.NodeName = ""
	pod.Spec.Hostname = ""
	pod.Spec.Subdomain = ""

	created, err := p.clientset.CoreV1().Pods(p.namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsForbidden(err) {
			return "", resil.Fatal("create the ephemeral clone",
				fmt.Sprintf("The cluster refused to create the clone in %s: %v", p.namespace, err), err).
				WithAdvice("Nothing was left behind. The probe lists the verbs a clone needs.")
		}
		return "", resil.Fatal("create the ephemeral clone",
			fmt.Sprintf("The cluster refused to create the clone: %v", err), err).
			WithAdvice("A PodSecurity or SCC policy may be rejecting it. The message above is the cluster's own.")
	}
	return "pod/" + created.Name, nil
}

// WaitRunning waits for the clone pod to be running, and reports why it will
// not be when that is the answer.
func (p *Platform) WaitRunning(ctx context.Context, ref string) error {
	name := podName(ref)
	deadline := time.Now().Add(5 * time.Minute)

	for {
		pod, err := p.clientset.CoreV1().Pods(p.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return resil.Retry("wait for the ephemeral clone",
				"PortCloak lost sight of the clone while waiting for it to start.", err)
		}
		switch pod.Status.Phase {
		case corev1.PodRunning:
			return nil
		case corev1.PodFailed, corev1.PodSucceeded:
			return resil.Fatal("wait for the ephemeral clone",
				fmt.Sprintf("The clone ended before it could be used: %s", podReason(pod)), nil)
		}
		// A pull failure or an unschedulable pod is reported with the cluster's
		// own words, which are more useful than anything PortCloak could invent.
		if reason := blockingWaitReason(pod); reason != "" {
			return resil.Fatal("wait for the ephemeral clone", reason, nil)
		}
		if time.Now().After(deadline) {
			return resil.Fatal("wait for the ephemeral clone",
				fmt.Sprintf("The clone did not become ready within five minutes: %s", podReason(pod)), nil)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func blockingWaitReason(pod *corev1.Pod) string {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting == nil {
			continue
		}
		switch cs.State.Waiting.Reason {
		case "ErrImagePull", "ImagePullBackOff", "InvalidImageName", "CreateContainerConfigError", "CrashLoopBackOff":
			return fmt.Sprintf("%s: %s", cs.State.Waiting.Reason, cs.State.Waiting.Message)
		}
	}
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse && c.Reason == corev1.PodReasonUnschedulable {
			return "the clone cannot be scheduled: " + c.Message
		}
	}
	return ""
}

func podReason(pod *corev1.Pod) string {
	if pod.Status.Reason != "" {
		return pod.Status.Reason + " " + pod.Status.Message
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			return cs.State.Waiting.Reason + " " + cs.State.Waiting.Message
		}
		if cs.State.Terminated != nil {
			return fmt.Sprintf("exited with %d: %s", cs.State.Terminated.ExitCode, cs.State.Terminated.Message)
		}
	}
	return string(pod.Status.Phase)
}

// Exec runs a command in the clone over the same SPDY mechanism kubectl exec
// uses.
// execArgv renders a command as the argv the exec subresource takes.
//
// That subresource has no environment field — unlike Docker's exec, which takes
// one, and unlike local and SSH, which set it on the process and in the command
// prefix. Left unhandled this adapter would accept a Command carrying Env and
// run it without: three adapters honouring a field and the fourth dropping it
// quietly, which is the shape of note 1. env(1) applies it instead, and the
// working directory wraps whatever came out of that.
func execArgv(cmd target.Command) []string {
	argv := append([]string{cmd.Path}, cmd.Args...)
	if len(cmd.Env) > 0 {
		keys := make([]string, 0, len(cmd.Env))
		for k := range cmd.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		prefix := []string{"env"}
		for _, k := range keys {
			prefix = append(prefix, k+"="+cmd.Env[k])
		}
		argv = append(prefix, argv...)
	}
	if cmd.Dir != "" {
		argv = []string{"/bin/sh", "-c", "cd " + shellQuote(cmd.Dir) + " && exec " + shellJoin(argv)}
	}
	return argv
}

func (p *Platform) Exec(ctx context.Context, ref string, cmd target.Command) (target.ExecResult, error) {
	argv := execArgv(cmd)

	out := newLineWriter(cmd.OnStdout)
	errW := newLineWriter(cmd.OnStderr)
	started := time.Now()

	err := p.stream(ctx, ref, argv, nil, out, errW)
	out.flush()
	errW.flush()

	res := target.ExecResult{Stdout: out.String(), Stderr: errW.String(), Duration: time.Since(started)}
	if err != nil {
		var codeErr exitCoder
		if errors.As(err, &codeErr) {
			res.ExitCode = codeErr.ExitStatus()
			return res, nil
		}
		return res, resil.Retry("run the command",
			"The exec channel to the clone dropped while the command was running.", err)
	}
	return res, nil
}

type exitCoder interface{ ExitStatus() int }

func (p *Platform) stream(ctx context.Context, ref string, argv []string, stdin io.Reader, stdout, stderr io.Writer) error {
	req := p.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName(ref)).
		Namespace(p.namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Container: "keycloak",
		Command:   argv,
		Stdin:     stdin != nil,
		Stdout:    stdout != nil,
		Stderr:    stderr != nil,
	}, runtime.NewParameterCodec(scheme.Scheme))

	// The WebSocket executor is preferred with a fallback to SPDY, because SPDY
	// exec can deadlock on large stdout while stdin goes unread — which is
	// exactly the shape of streaming a large realm's export out.
	exec, err := remotecommand.NewWebSocketExecutor(p.restCfg, "GET", req.URL().String())
	if err != nil {
		exec, err = remotecommand.NewSPDYExecutor(p.restCfg, "POST", req.URL())
		if err != nil {
			return resil.Fatal("open an exec channel",
				"PortCloak could not open an exec channel to the clone.", err).
				WithAdvice("Exec may be disabled by policy on this cluster. Capturing from a host with database access is the alternative.")
		}
	}
	return exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin: stdin, Stdout: stdout, Stderr: stderr,
	})
}

// copyOutScript streams a directory as a length-framed sequence of files.
//
// `kubectl cp`, and everything built the way it is built, runs tar inside the
// container. The official Keycloak image has no tar — it is assembled on
// ubi-micro, which ships neither tar nor gzip — so the export finished, the pod
// was healthy, and the capture failed at "the stream from the clone ended
// unexpectedly", which was the exec channel closing on a binary that does not
// exist. Nothing said so, because tar's stderr was going to io.Discard.
//
// This needs sh, find, wc, cat and printf, which that image does have. Each
// file arrives as
//
//	PCF <size> <name relative to dir>\n
//	<exactly size bytes>
//
// which is a far smaller contract than tar's, and one no image can be missing.
// A file whose name contains a newline would break the framing; Keycloak names
// its export after the realm, and a realm name cannot contain one.
func copyOutScript(dir string) string {
	return "cd " + shellQuote(dir) + ` && find . -type f -print | while IFS= read -r f; do ` +
		`printf 'PCF %s %s\n' "$(wc -c < "$f")" "${f#./}"; cat "$f"; done`
}

// CopyOut streams a directory out of the clone over the exec channel.
func (p *Platform) CopyOut(ctx context.Context, ref, dir string, sink target.ArtifactSink) error {
	pr, pw := io.Pipe()

	// The command's stderr is kept rather than discarded. It is the half that
	// says which binary was missing or which path did not exist, and throwing
	// it away is what turned a one-line diagnosis into a mystery.
	var stderr boundedBuffer

	errCh := make(chan error, 1)
	go func() {
		// Bounded buffering: the stream is consumed as it arrives rather than
		// accumulated, which is what keeps a very large realm from deadlocking.
		err := p.stream(ctx, ref, []string{"/bin/sh", "-c", copyOutScript(dir)}, nil, pw, &stderr)
		_ = pw.CloseWithError(err)
		errCh <- err
	}()

	fail := func(err error) error {
		_ = pr.CloseWithError(err)
		<-errCh
		return collectFailure(err, &stderr)
	}

	br := bufio.NewReaderSize(pr, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			_ = pr.CloseWithError(err)
			return err
		}

		header, err := br.ReadString('\n')
		if errors.Is(err, io.EOF) && strings.TrimSpace(header) == "" {
			break
		}
		if err != nil {
			return fail(err)
		}

		name, size, err := parseFrame(header)
		if err != nil {
			return fail(err)
		}
		if name == "" {
			// Nothing to hand on, but the bytes still have to be drawn off or
			// the next header is read out of the middle of a file.
			if _, err := io.CopyN(io.Discard, br, size); err != nil {
				return fail(err)
			}
			continue
		}

		body := io.LimitReader(br, size)
		if err := sink.Artifact(ctx, target.Artifact{Name: name, Size: size, Mode: 0o600}, body); err != nil {
			_ = pr.CloseWithError(err)
			<-errCh
			return err
		}
		// A sink that read less than it was offered would otherwise leave the
		// reader inside the file it just took.
		if _, err := io.Copy(io.Discard, body); err != nil {
			return fail(err)
		}
	}

	_ = pr.Close()
	if err := <-errCh; err != nil {
		return collectFailure(err, &stderr)
	}
	return nil
}

// collectFailure separates the two ways collecting can go wrong.
//
// A non-zero exit is a decision by the shell inside the clone — a directory
// that is not there, a permission it does not have — and trying it three more
// times only delays the same answer. A stream that ends without one is the exec
// channel dropping, which is exactly what retrying is for. Exec already draws
// this line; CopyOut used to call everything retryable.
func collectFailure(err error, stderr *boundedBuffer) error {
	var codeErr exitCoder
	if errors.As(err, &codeErr) {
		return resil.Fatal("collect the exported files",
			"The clone could not hand the exported files back."+stderr.suffix(), err).
			WithAdvice("The export's own output is in the job log above; it says whether it wrote anything.")
	}
	return resil.Retry("collect the exported files",
		"The stream from the clone ended unexpectedly."+stderr.suffix(), err)
}

// parseFrame reads one `PCF <size> <name>` header.
func parseFrame(line string) (name string, size int64, err error) {
	fields := strings.SplitN(strings.TrimRight(line, "\r\n"), " ", 3)
	if len(fields) != 3 || fields[0] != "PCF" {
		return "", 0, fmt.Errorf("the clone sent something that is not a file header: %q", truncateLine(line))
	}
	size, err = strconv.ParseInt(fields[1], 10, 64)
	if err != nil || size < 0 {
		return "", 0, fmt.Errorf("the clone sent a file with an unreadable length: %q", truncateLine(line))
	}
	return strings.TrimPrefix(fields[2], "./"), size, nil
}

func truncateLine(s string) string {
	s = strings.TrimRight(s, "\r\n")
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

// boundedBuffer keeps the first few KiB of a command's stderr. A failing
// command says why in its first line; the cap is there so a command that fails
// by producing output forever cannot be the thing that runs the app out of
// memory.
type boundedBuffer struct {
	buf bytes.Buffer
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if room := 8 << 10; b.buf.Len() < room {
		if len(p) > room-b.buf.Len() {
			b.buf.Write(p[:room-b.buf.Len()])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

// suffix renders the captured stderr for the end of a failure sentence.
func (b *boundedBuffer) suffix() string {
	s := strings.TrimSpace(b.buf.String())
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return " The clone said: " + truncateLine(s)
}

// CopyIn writes a file into the clone, for the restore path.
//
// The bytes go in over stdin rather than as a tar, for the same reason CopyOut
// does not read one out: the Keycloak image has no tar. Ownership is not set
// and never was — the shell runs as the pod's own user and cannot chown, which
// is the same result the tar header used to produce.
func (p *Platform) CopyIn(ctx context.Context, ref, dest string, _ int64, _ clone.FileOwner, r io.Reader) error {
	var stderr boundedBuffer
	err := p.stream(ctx, ref, []string{"/bin/sh", "-c", copyInScript(dest)}, r, io.Discard, &stderr)
	if err != nil {
		return resil.Retry("send the snapshot",
			fmt.Sprintf("PortCloak could not write %s into the clone.", dest)+stderr.suffix(), err)
	}
	return nil
}

// copyInScript writes stdin to one file, for the same reason CopyOut does not
// use tar: the image has none. The umask is what makes the file 0600 from the
// moment it exists rather than briefly after.
func copyInScript(dest string) string {
	return "umask 077 && cat > " + shellQuote(dest)
}

// Destroy deletes the clone pod. A pod that is already gone is success.
func (p *Platform) Destroy(ctx context.Context, ref string) error {
	grace := int64(0)
	err := p.clientset.CoreV1().Pods(p.namespace).Delete(ctx, podName(ref), metav1.DeleteOptions{
		GracePeriodSeconds: &grace,
	})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// FindOrphans lists pods carrying PortCloak's own label.
func (p *Platform) FindOrphans(ctx context.Context) ([]target.Orphan, error) {
	pods, err := p.clientset.CoreV1().Pods(p.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: target.LabelEphemeral,
	})
	if err != nil {
		// An environment that could not be checked is reported as unchecked,
		// never as clean.
		return nil, resil.Retry("check for orphaned clones",
			fmt.Sprintf("PortCloak could not list pods in %s, so it cannot say whether any were left behind.", p.namespace), err)
	}

	out := make([]target.Orphan, 0, len(pods.Items))
	for _, pod := range pods.Items {
		out = append(out, target.Orphan{
			Environment: p.env.Name,
			Kind:        string(config.EnvKubernetes),
			Ref:         "pod/" + pod.Name,
			JobID:       pod.Labels[target.LabelJob],
			CreatedAt:   pod.CreationTimestamp.Time,
			State:       string(pod.Status.Phase),
		})
	}
	return out, nil
}

func podName(ref string) string { return strings.TrimPrefix(ref, "pod/") }

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func shellJoin(parts []string) string {
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = shellQuote(p)
	}
	return strings.Join(quoted, " ")
}

// lineWriter turns a byte stream into whole lines for the UI, and keeps the
// full text for the driver to parse afterwards.
type lineWriter struct {
	onLine  func(string)
	pending bytes.Buffer
	all     strings.Builder
}

func newLineWriter(onLine func(string)) *lineWriter { return &lineWriter{onLine: onLine} }

func (w *lineWriter) Write(p []byte) (int, error) {
	w.all.Write(p)
	if w.onLine == nil {
		return len(p), nil
	}
	w.pending.Write(p)
	for {
		idx := bytes.IndexByte(w.pending.Bytes(), '\n')
		if idx < 0 {
			break
		}
		line := make([]byte, idx+1)
		_, _ = w.pending.Read(line)
		w.onLine(strings.TrimRight(string(line), "\r\n"))
	}
	return len(p), nil
}

func (w *lineWriter) flush() {
	if w.onLine == nil || w.pending.Len() == 0 {
		return
	}
	w.onLine(strings.TrimRight(w.pending.String(), "\r\n"))
	w.pending.Reset()
}

func (w *lineWriter) String() string { return w.all.String() }
