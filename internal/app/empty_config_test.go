package app

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"unicode"

	"portcloak/internal/engine/config"
)

// An empty configuration is the state every operator is in exactly once, on the
// first launch, and it is the one state no amount of manual testing reaches
// again afterwards. These tests drive the screen-load methods against a home
// directory that has never been written to, and assert the two things the
// frontend cannot survive being wrong about:
//
//   - an empty list arrives as [], never as null;
//   - a field arrives under the name the frontend reads it by.
//
// Both were broken. A nil slice marshals as null, and the first thing every
// screen does with a list is read its length, so the whole app sat on its
// loading spinner with a TypeError behind it.

// emptyEngine is a PortCloak that has never been configured.
func emptyEngine(t *testing.T) *Engine {
	t.Helper()
	t.Setenv("PORTCLOAK_HOME", t.TempDir())
	eng, err := NewEngine("test")
	if err != nil {
		t.Fatalf("an engine could not be built on an empty home: %v", err)
	}
	return eng
}

// screenLoads is what the frontend calls when a screen opens, which is the set
// that has to survive an empty configuration.
func screenLoads(eng *Engine) map[string]func() any {
	cfg := NewConfigController(eng)
	capture := NewCaptureController(eng)
	snaps := NewSnapshotController(eng)
	restore := NewRestoreController(eng)
	jobs := NewJobsController(eng)
	keys := NewKeysController(eng)
	audit := NewAuditController(eng)
	settings := NewSettingsController(eng)

	return map[string]func() any{
		"ConfigController.Load":             func() any { return cfg.Load() },
		"ConfigController.Reload":           func() any { return cfg.Reload() },
		"ConfigController.EnvironmentKinds": func() any { return cfg.EnvironmentKinds() },
		"ConfigController.StorageKinds":     func() any { return cfg.StorageKinds() },
		"CaptureController.Defaults":        func() any { return capture.Defaults() },
		"SnapshotController.Library":        func() any { return snaps.Library() },
		"RestoreController.Strategies":      func() any { return restore.Strategies() },
		"RestoreController.Destinations":    func() any { return restore.Destinations() },
		"RestoreController.OutOfScopeNote":  func() any { return restore.OutOfScopeNote() },
		"JobsController.List":               func() any { return jobs.List() },
		"KeysController.List":               func() any { return keys.List() },
		"KeysController.Recipients":         func() any { return keys.Recipients() },
		"AuditController.Audit":             func() any { return audit.Audit("", 30) },
		"SettingsController.Location":       func() any { return settings.Location() },
		"SettingsController.Orphans":        func() any { return settings.Orphans() },
		"SettingsController.WorkingData":    func() any { return settings.WorkingData() },
	}
}

// TestControllers_NeverHandTheFrontendNull is the regression test for a first
// launch that never got past "Loading configuration…".
func TestControllers_NeverHandTheFrontendNull(t *testing.T) {
	eng := emptyEngine(t)

	for name, load := range screenLoads(eng) {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(load())
			if err != nil {
				t.Fatalf("%s could not be marshalled: %v", name, err)
			}
			var decoded any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("%s produced JSON that will not parse: %v", name, err)
			}
			for _, path := range nullsIn("", decoded) {
				t.Errorf("%s returned null at %s — the frontend reads that as a list and throws before it can clear the spinner", name, path)
			}
		})
	}
}

// nullsIn walks decoded JSON and reports the path of every null it finds.
//
// A null anywhere in a response is worth failing on: the fields that are
// legitimately absent are tagged omitempty and are simply not there, so a null
// that survived is a slice or a map that was never made.
func nullsIn(path string, v any) []string {
	switch t := v.(type) {
	case nil:
		if path == "" {
			return []string{"the whole response"}
		}
		return []string{path}
	case map[string]any:
		var out []string
		for k, child := range t {
			out = append(out, nullsIn(path+"."+k, child)...)
		}
		return out
	case []any:
		var out []string
		for i, child := range t {
			out = append(out, nullsIn(path+"["+itoa(i)+"]", child)...)
		}
		return out
	default:
		return nil
	}
}

// TestConfigModel_CrossesTheBridgeUnderTheNamesTheFrontendReads guards the
// second half of the same failure.
//
// config.Environment, config.Storage and config.Preferences are read from
// config.yaml and were tagged for YAML only. Over the Wails bridge they are
// marshalled by encoding/json, which falls back to the Go field name, so the
// environments list arrived as {"Name":…,"Kind":…} while every screen read
// env.name and env.kind. Nothing failed loudly: the rows rendered blank.
func TestConfigModel_CrossesTheBridgeUnderTheNamesTheFrontendReads(t *testing.T) {
	eng := emptyEngine(t)

	env := config.Environment{
		Name: "prod", Kind: config.EnvSSH,
		Host: "sso.example", User: "keycloak", ServerFolder: "/opt/keycloak",
		AdminBaseURL: "https://sso.example",
	}
	if err := eng.Config.AddEnvironment(env); err != nil {
		t.Fatalf("the environment could not be added: %v", err)
	}
	st := config.Storage{Name: "archive", Kind: config.StoreDisk, Folder: t.TempDir()}
	if err := eng.Config.AddStorage(st); err != nil {
		t.Fatalf("the storage could not be added: %v", err)
	}

	raw, err := json.Marshal(NewConfigController(eng).Load())
	if err != nil {
		t.Fatalf("the configuration could not be marshalled: %v", err)
	}

	var snapshot struct {
		Environments []map[string]any `json:"environments"`
		Storage      []map[string]any `json:"storage"`
		Preferences  map[string]any   `json:"preferences"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("the configuration could not be parsed back: %v", err)
	}

	if len(snapshot.Environments) != 1 || len(snapshot.Storage) != 1 {
		t.Fatalf("expected one environment and one storage, got %d and %d",
			len(snapshot.Environments), len(snapshot.Storage))
	}

	// The names the screens actually read, per frontend/src/api.ts.
	assertKeys(t, "environment", snapshot.Environments[0],
		"name", "kind", "host", "user", "serverFolder", "adminBaseUrl", "target", "readiness")
	assertKeys(t, "storage", snapshot.Storage[0], "name", "kind", "folder", "root", "readiness")
	assertKeys(t, "preferences", snapshot.Preferences,
		"usersMode", "usersPerFile", "verifyByDefault", "encryptByDefault", "allowSecretReveal")

	// And nothing may arrive under a Go field name, which is what a forgotten
	// json tag looks like from the frontend.
	for _, m := range []map[string]any{snapshot.Environments[0], snapshot.Storage[0], snapshot.Preferences} {
		for key := range m {
			if unicode.IsUpper(rune(key[0])) {
				t.Errorf("%q crosses the bridge under its Go field name; the frontend reads %q",
					key, strings.ToLower(key[:1])+key[1:])
			}
		}
	}
}

func assertKeys(t *testing.T, what string, got map[string]any, want ...string) {
	t.Helper()
	for _, key := range want {
		if _, ok := got[key]; !ok {
			t.Errorf("the %s reached the frontend without %q; it has %v", what, key, sortedKeys(got))
		}
	}
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
