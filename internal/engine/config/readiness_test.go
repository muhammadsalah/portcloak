package config

import "testing"

// Readiness is a banner over an entry that is otherwise fine, so a rule that is
// wrong is worse than no rule: it tells an operator their working configuration
// is broken, and the only way to find out otherwise is to ignore the app.
//
// The rule these pin is that a field belongs in EnvironmentReadiness only when
// Validate lets it be empty *and* the adapter has no usable default for it.

// A Docker environment with no dockerEndpoint uses DOCKER_HOST or the local
// socket — docker.NewPlatform falls through to client.FromEnv and the error
// path calls it "the default endpoint". Blank is how a working configuration
// says that, and it is what most of them say.
func TestEnvironmentReadiness_DockerWithoutAnEndpointUsesTheLocalSocket(t *testing.T) {
	r := EnvironmentReadiness(Environment{
		Name: "local-docker", Kind: EnvDocker, Container: "keycloak",
	})
	if !r.Ready {
		t.Errorf("a Docker environment naming its container was reported as not usable: %s", r.Reason)
	}
}

// The same for Kubernetes: k8s.NewPlatform hands an empty context and kubeconfig
// straight to client-go's default loading rules, which is KUBECONFIG, the
// default file's current context, or in-cluster.
func TestEnvironmentReadiness_KubernetesWithoutAContextUsesTheDefaultRules(t *testing.T) {
	r := EnvironmentReadiness(Environment{
		Name: "prod", Kind: EnvKubernetes,
		Namespace: "sso", Workload: "deployment/keycloak",
	})
	if !r.Ready {
		t.Errorf("a Kubernetes environment naming its namespace and workload was reported as not usable: %s", r.Reason)
	}
}

// And the case readiness does exist for: Validate permits an SSH entry with no
// auth method, and there is no default that could stand in for one.
func TestEnvironmentReadiness_SSHWithoutAnAuthMethodIsNotUsableYet(t *testing.T) {
	r := EnvironmentReadiness(Environment{
		Name: "prod", Kind: EnvSSH, Host: "sso.example",
		User: "keycloak", ServerFolder: "/opt/keycloak",
	})
	if r.Ready {
		t.Error("an SSH environment with no auth method was reported as usable")
	}

	r = EnvironmentReadiness(Environment{
		Name: "prod", Kind: EnvSSH, Host: "sso.example",
		User: "keycloak", ServerFolder: "/opt/keycloak", Auth: SSHKey,
	})
	if r.Ready {
		t.Error("an SSH environment whose private key is not in the keychain was reported as usable")
	}

	// The agent needs nothing from the keychain, so it is ready as it stands.
	r = EnvironmentReadiness(Environment{
		Name: "prod", Kind: EnvSSH, Host: "sso.example",
		User: "keycloak", ServerFolder: "/opt/keycloak", Auth: SSHAgent,
	})
	if !r.Ready {
		t.Errorf("an SSH environment using the agent was reported as not usable: %s", r.Reason)
	}
}

// Nothing readiness reports may contradict Validate. An entry Validate accepts
// and readiness calls unusable has to name a field with no default — which is
// the invariant the Docker and Kubernetes rules broke.
func TestEnvironmentReadiness_NeverContradictsAValidatedEntry(t *testing.T) {
	for _, e := range []Environment{
		{Name: "docker", Kind: EnvDocker, Container: "keycloak"},
		{Name: "docker-endpoint", Kind: EnvDocker, Container: "keycloak", DockerEndpoint: "unix:///var/run/docker.sock"},
		{Name: "k8s", Kind: EnvKubernetes, Namespace: "sso", Workload: "deployment/keycloak"},
		{Name: "local", Kind: EnvLocal, ServerFolder: "/opt/keycloak"},
	} {
		t.Run(e.Name, func(t *testing.T) {
			cfg := Config{Version: 1, Environments: []Environment{e}}
			if problems := Validate(&cfg, nil); len(problems) > 0 {
				t.Fatalf("the fixture is not a valid environment: %v", problems)
			}
			if r := EnvironmentReadiness(e); !r.Ready {
				t.Errorf("Validate accepted this environment and readiness calls it unusable: %s", r.Reason)
			}
		})
	}
}
