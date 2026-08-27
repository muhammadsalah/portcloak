package app

import (
	"strings"
	"testing"

	"portcloak/internal/engine/config"
)

// An edit the engine refuses has to come back as the reason it was refused, in
// a form the editor can put over the field the operator is looking at.
//
// This drives the entry that could not be saved and gave no clue why: a
// Kubernetes workload typed as a bare name. Validate had the sentence naming
// the value and the fix all along.
func TestSaveEnvironment_ReportsTheProblemRatherThanTheFile(t *testing.T) {
	eng := emptyEngine(t)
	c := NewConfigController(eng)

	failure := c.SaveEnvironment("", config.Environment{
		Name: "Keycloak Corp A", Kind: config.EnvKubernetes,
		Context: "crc-admin", Namespace: "kc-a", Workload: "kc-a",
	}, "")
	if failure == nil {
		t.Fatal("a workload given as a bare name was accepted")
	}

	// Fail() renders a *config.ValidationError as "<path> has 1 problem:" and
	// then indents it, which is right for the banner about a hand-edited file
	// and wrong over a form: the operator is looking at the field, not the file.
	if strings.Contains(failure.Message, "has 1 problem") {
		t.Errorf("the form is being told about the file: %q", failure.Message)
	}
	for _, want := range []string{`"kc-a"`, "deployment/<name>"} {
		if !strings.Contains(failure.Message, want) {
			t.Errorf("the message does not mention %q: %q", want, failure.Message)
		}
	}

	if failure := c.SaveEnvironment("", config.Environment{
		Name: "Keycloak Corp A", Kind: config.EnvKubernetes,
		Context: "crc-admin", Namespace: "kc-a", Workload: "deployment/kc-a",
	}, ""); failure != nil {
		t.Fatalf("the corrected environment was still refused: %s", failure.Message)
	}
}

// An entry can be wrong in more than one place at once, and reporting one
// problem per attempt makes an operator fix a form one save at a time — the
// exact thing Validate collects every problem to avoid. They cross as one
// message with a line each, which the notice renders as separate lines.
func TestSaveEnvironment_ReportsEveryProblemAtOnce(t *testing.T) {
	eng := emptyEngine(t)

	failure := NewConfigController(eng).SaveEnvironment("", config.Environment{
		Name: "half-done", Kind: config.EnvKubernetes,
	}, "")
	if failure == nil {
		t.Fatal("an environment with no namespace and no workload was accepted")
	}
	lines := strings.Split(failure.Message, "\n")
	if len(lines) < 2 {
		t.Fatalf("both problems should be reported together, got %q", failure.Message)
	}
	for _, want := range []string{"namespace", "Deployment or StatefulSet"} {
		if !strings.Contains(failure.Message, want) {
			t.Errorf("the message does not mention %q: %q", want, failure.Message)
		}
	}
}

// The same applies to storage, which shares the save path.
func TestSaveStorage_ReportsTheProblemRatherThanTheFile(t *testing.T) {
	eng := emptyEngine(t)

	failure := NewConfigController(eng).SaveStorage("", config.Storage{
		Name: "archive", Kind: config.StoreS3,
	}, "")
	if failure == nil {
		t.Fatal("an S3 storage with no bucket was accepted")
	}
	if strings.Contains(failure.Message, "has 1 problem") {
		t.Errorf("the form is being told about the file: %q", failure.Message)
	}
}
