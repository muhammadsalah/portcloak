//go:build integration

// The remote adapters run the same Executor table as local. A divergence is a
// bug in the newest implementation, not a reason to fork the table.
//
// These are behind a build tag rather than a runtime probe on purpose: a
// missing Docker daemon produces "not run", never a silent pass. A green board
// that quietly skipped every remote target is worse than a red one.
package target_test

import (
	"context"
	"os"
	"strconv"
	"testing"

	"portcloak/internal/engine/config"
	"portcloak/internal/engine/target"
	"portcloak/internal/engine/target/docker"
	"portcloak/internal/engine/target/k8s"
	"portcloak/internal/engine/target/ssh"
	"portcloak/internal/engine/target/targettest"
)

// env reads a service-container setting, failing rather than skipping: under
// the integration tag a missing setting is a broken CI configuration.
func env(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Fatalf("%s is not set. The integration suite expects the service containers from CI; see spec/rollout/00 §0.7.", key)
	}
	return v
}

// sshPort lets CI point the suite at an sshd on a non-privileged port without
// the tests caring which.
func sshPort(t *testing.T) int {
	t.Helper()
	v := os.Getenv("PORTCLOAK_TEST_SSH_PORT")
	if v == "" {
		return 22
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("PORTCLOAK_TEST_SSH_PORT is %q, which is not a port number", v)
	}
	return n
}

func TestSSH_ExecutorContract(t *testing.T) {
	host := env(t, "PORTCLOAK_TEST_SSH_HOST")
	user := env(t, "PORTCLOAK_TEST_SSH_USER")
	credential := env(t, "PORTCLOAK_TEST_SSH_CREDENTIAL")

	targettest.RunContract(t, func(t *testing.T) target.Executor {
		creds := config.NewMemoryCredentials()
		handle := config.Handle("ssh", "contract")
		if err := creds.Set(handle, credential); err != nil {
			t.Fatal(err)
		}
		e, err := ssh.New(config.Environment{
			Name: "contract", Kind: config.EnvSSH,
			Host: host, Port: sshPort(t), User: user, Auth: config.SSHPassword,
			CredentialRef: handle,
		}, creds)
		if err != nil {
			t.Fatal(err)
		}
		// A first connection to a throwaway test host is accepted
		// deliberately; in the application this is always an operator's
		// decision.
		e.AcceptHostKey()
		return e
	})
}

// Docker runs the table through an ephemeral clone, so every row is also a
// statement about the clone: it is created, it accepts commands, and teardown
// destroys it.
func TestDocker_ExecutorContract(t *testing.T) {
	container := env(t, "PORTCLOAK_TEST_DOCKER_CONTAINER")

	targettest.RunContract(t, func(t *testing.T) target.Executor {
		e, err := docker.NewExecutor(config.Environment{
			Name: "contract", Kind: config.EnvDocker,
			DockerEndpoint: os.Getenv("PORTCLOAK_TEST_DOCKER_ENDPOINT"),
			Container:      container,
		})
		if err != nil {
			t.Fatal(err)
		}
		return e
	})
}

func TestKubernetes_ExecutorContract(t *testing.T) {
	namespace := env(t, "PORTCLOAK_TEST_K8S_NAMESPACE")
	workload := env(t, "PORTCLOAK_TEST_K8S_WORKLOAD")

	targettest.RunContract(t, func(t *testing.T) target.Executor {
		e, err := k8s.NewExecutor(config.Environment{
			Name: "contract", Kind: config.EnvKubernetes,
			Kubeconfig: os.Getenv("PORTCLOAK_TEST_KUBECONFIG"),
			Context:    os.Getenv("PORTCLOAK_TEST_K8S_CONTEXT"),
			Namespace:  namespace, Workload: workload,
			ContainerName: os.Getenv("PORTCLOAK_TEST_K8S_CONTAINER"),
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = e.Teardown(context.Background()) })
		return e
	})
}
