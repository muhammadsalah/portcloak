// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"portcloak/internal/app"
)

// The frontend addresses a bound method by the string Wails registers it
// under, and Wails registers the fully-qualified Go name: package path, type,
// method. Nothing checks that at compile time in either language, so a wrong
// string is a runtime "unknown bound method name" on a screen nobody opened
// during development — and a rename of this package breaks all eight screens
// at once, silently.
//
// This test is the missing compile step. It reads the calls the frontend
// actually makes and resolves each one against the real controllers.

// callRe matches `call<T>("ConfigController", "Load", …)` in api.ts.
var callRe = regexp.MustCompile(`call<[^>]*>\(\s*"([A-Za-z]+)"\s*,\s*"([A-Za-z0-9_]+)"`)

// packageRe matches the package-path constant the bridge prefixes calls with.
var packageRe = regexp.MustCompile(`const goPackage = "([^"]+)"`)

// byNameRe matches the single place the bridge builds an address for Wails.
// Declaring the package path is not enough — the address that goes over the
// wire has to be built from it, and the first version of this test checked the
// constant existed while the call site quietly ignored it.
var byNameRe = regexp.MustCompile(`Call\.ByName\(\s*` + "`" + `([^` + "`" + `]*)` + "`")

// boundFQNs mirrors what Wails does at bind time: for every controller the
// application registers, the name of every exported method it will accept a
// call for.
//
// internalServiceMethods in Wails excludes the lifecycle hooks, so ServiceName,
// ServiceStartup, ServiceShutdown and ServeHTTP are excluded here too.
func boundFQNs(t *testing.T) map[string]bool {
	t.Helper()

	internal := map[string]bool{
		"ServiceName": true, "ServiceStartup": true,
		"ServiceShutdown": true, "ServeHTTP": true,
	}

	// Read back off the same registry Run binds, against a zero engine: only
	// the method set is read, never called.
	//
	// Derived rather than restated. A second hand-written list of controllers
	// is a list that drifts, and it did: a controller added to the registry and
	// called from the frontend was reported by this test as eight unbound
	// methods, which is the exact failure it exists to catch and the exact
	// wrong place to look for the cause.
	out := map[string]bool{}
	for _, svc := range controllers(&app.Engine{}) {
		ptr := reflect.TypeOf(svc.Instance())
		named := ptr.Elem()
		for i := range ptr.NumMethod() {
			name := ptr.Method(i).Name
			if internal[name] {
				continue
			}
			out[fmt.Sprintf("%s.%s.%s", named.PkgPath(), named.Name(), name)] = true
		}
	}
	return out
}

func readAPI(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "api.ts"))
	if err != nil {
		t.Fatalf("the frontend bridge could not be read: %v", err)
	}
	return string(b)
}

// Every call the screens make must land on a method that exists.
func TestBindings_EveryFrontendCallResolves(t *testing.T) {
	src := readAPI(t)
	bound := boundFQNs(t)

	pkg := packageRe.FindStringSubmatch(src)
	if pkg == nil {
		t.Fatal("api.ts does not define goPackage, so calls cannot carry the package path Wails registers methods under")
	}
	// The prefix has to be the controllers' real import path — internal/app,
	// not this package. A rename there, or a controller moved up here into the
	// Wails shell, without a matching edit in api.ts is the failure this test
	// exists to catch.
	if want := reflect.TypeOf(&app.ConfigController{}).Elem().PkgPath(); pkg[1] != want {
		t.Fatalf("api.ts addresses %q, but the controllers live in %q", pkg[1], want)
	}

	// The address actually sent must be built from the package path, the
	// service and the method — in that order.
	tmpl := byNameRe.FindStringSubmatch(src)
	if tmpl == nil {
		t.Fatal("no Call.ByName template was found in api.ts, so what the bridge sends cannot be checked")
	}
	if want := "${goPackage}.${service}.${method}"; tmpl[1] != want {
		t.Fatalf("the bridge addresses methods as %q, but Wails registers them as %q.\n"+
			"Without the package path every call fails with \"unknown bound method name\".", tmpl[1], want)
	}

	calls := callRe.FindAllStringSubmatch(src, -1)
	if len(calls) == 0 {
		t.Fatal("no calls were found in api.ts; the extraction pattern no longer matches the bridge")
	}

	var missing []string
	seen := map[string]bool{}
	for _, c := range calls {
		fqn := fmt.Sprintf("%s.%s.%s", pkg[1], c[1], c[2])
		if seen[fqn] {
			continue
		}
		seen[fqn] = true
		if !bound[fqn] {
			missing = append(missing, fmt.Sprintf("%s.%s", c[1], c[2]))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("the frontend calls %d method(s) that are not bound:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
	t.Logf("%d distinct calls across the screens, all resolved", len(seen))
}

// A controller method nothing calls is either dead code or a screen that was
// never wired up. Both are worth seeing rather than discovering later.
func TestBindings_EveryBoundMethodIsReachable(t *testing.T) {
	src := readAPI(t)

	called := map[string]bool{}
	for _, c := range callRe.FindAllStringSubmatch(src, -1) {
		called[c[1]+"."+c[2]] = true
	}

	var unused []string
	for fqn := range boundFQNs(t) {
		parts := strings.Split(fqn, ".")
		short := parts[len(parts)-2] + "." + parts[len(parts)-1]
		if !called[short] {
			unused = append(unused, short)
		}
	}
	sort.Strings(unused)
	if len(unused) > 0 {
		t.Errorf("%d bound method(s) no screen calls:\n  %s",
			len(unused), strings.Join(unused, "\n  "))
	}
}
