// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The window options and the menu are the two places a development affordance
// can reach a user, and both are decided in code that no other test touches.
// These tests are cheap and they fail loudly, which is the point: the failure
// mode they guard against is a release that looks correct and ships an
// inspector.

// TestProductionMode_MatchesBuildTag pins the flag to the tag. Both files
// declare the same constant under opposite build constraints, so the only way
// this can be wrong is if one of them is edited without the other.
func TestProductionMode_MatchesBuildTag(t *testing.T) {
	// A test binary is compiled without -tags production unless the caller asks
	// for it, so this asserts the development default and the production build
	// asserts its own in CI via `go test -tags production`.
	if productionBuild {
		t.Log("compiled with -tags production")
	} else {
		t.Log("compiled without -tags production")
	}
}

// TestMenu_OmitsTheDefaultViewAndHelpMenus is the regression test for the two
// entries that made Wails' default menu unshippable: a View menu that can
// reload the webview out from under a running job, and a Help menu whose one
// item navigates the main window to https://wails.io with no way back.
//
// It reads the source rather than building a *Menu, because constructing one
// needs a running application and the thing worth pinning is the decision, not
// the widget.
func TestMenu_OmitsTheDefaultViewAndHelpMenus(t *testing.T) {
	src := readSource(t, "menu.go")

	for _, banned := range []string{"application.ViewMenu", "application.HelpMenu", "application.FileMenu"} {
		if strings.Contains(src, banned) {
			t.Errorf("menu.go adds %s to the application menu.\n"+
				"ViewMenu carries Reload, ForceReload and Toggle DevTools; HelpMenu's only\n"+
				"entry navigates the main window to https://wails.io. Neither belongs in a\n"+
				"shipped build. See the comment on applicationMenu.", banned)
		}
	}

	// Edit is not optional on macOS: a WKWebView takes Cmd+C, Cmd+V and Cmd+A
	// from the first responder chain via these menu items, so removing the Edit
	// menu silently breaks copy and paste in every text field in the app.
	if !strings.Contains(src, "application.EditMenu") {
		t.Error("menu.go no longer adds application.EditMenu; that removes Cmd+C/V/X/A " +
			"from every text field on macOS")
	}
}

// TestDeveloperMenu_IsCompiledOutOfProductionBuilds checks that the Developer
// menu lives behind the build tag rather than behind a runtime condition. A
// runtime condition would still carry the inspector in the binary.
func TestDeveloperMenu_IsCompiledOutOfProductionBuilds(t *testing.T) {
	prod := readSource(t, "menu_production.go")
	if !constrainedBy(prod, "//go:build production") {
		t.Fatal("menu_production.go must be constrained to the production build tag")
	}
	if strings.Contains(prod, "OpenDevTools") {
		t.Error("the production menu references OpenDevTools")
	}

	dev := readSource(t, "menu_dev.go")
	if !constrainedBy(dev, "//go:build !production") {
		t.Fatal("menu_dev.go must be constrained to !production")
	}
}

// constrainedBy reports whether a file carries a build constraint, which means
// it appears above the package clause.
//
// Not "starts with": every file in this repository opens with a licence header,
// and Go's rule is that a constraint may be preceded by blank lines and other
// line comments — so byte zero was never what made the tag effective, and
// checking for it there only pinned the header's absence.
func constrainedBy(src, constraint string) bool {
	head, _, found := strings.Cut(src, "\npackage ")
	if !found {
		return false
	}
	for _, line := range strings.Split(head, "\n") {
		if strings.TrimSpace(line) == constraint {
			return true
		}
	}
	return false
}

// TestWindow_GatesDevToolsOnTheBuildTag pins the window options to the flag
// rather than to a literal, which is how the inspector would come back.
func TestWindow_GatesDevToolsOnTheBuildTag(t *testing.T) {
	src := readSource(t, "run.go")

	if !strings.Contains(src, "DevToolsEnabled:            !productionBuild") {
		t.Error("run.go must set DevToolsEnabled from !productionBuild")
	}
	if !strings.Contains(src, "DefaultContextMenuDisabled: productionBuild") {
		t.Error("run.go must set DefaultContextMenuDisabled from productionBuild; " +
			"without it a release build still offers a right-click context menu")
	}
	if !strings.Contains(src, "DisableResize: false") {
		t.Error("run.go must state DisableResize explicitly; the window being resizable " +
			"is a requirement, not a default worth inheriting silently")
	}
}

// TestMasthead_UsesTheWailsDragProperty guards the fix for a window that could
// not be dragged at all. Wails' runtime compares the computed value of
// --wails-draggable; -webkit-app-region is an Electron convention and is inert
// in WKWebView, so the Electron spelling looks right and does nothing.
func TestMasthead_UsesTheWailsDragProperty(t *testing.T) {
	css := readRepoFile(t, filepath.Join("frontend", "src", "app", "Masthead.tsx"))

	if !strings.Contains(css, "--wails-draggable: drag") {
		t.Error("the masthead must set --wails-draggable: drag, or the window cannot be " +
			"dragged by its title bar")
	}
	// Comments are stripped first: the declaration is what would break the
	// window, and the comment above it explains why the Electron spelling was
	// wrong — naming it there must not fail the test that forbids using it.
	if strings.Contains(stripCSSComments(css), "-webkit-app-region") {
		t.Error("Masthead.tsx still declares -webkit-app-region, which Wails does not read")
	}
}

// stripCSSComments removes /* … */ blocks so a check can look at declarations
// alone. The masthead's styling is a styled-components template rather than a
// stylesheet now, and both spell a comment the same way.
func stripCSSComments(css string) string {
	var b strings.Builder
	for {
		start := strings.Index(css, "/*")
		if start < 0 {
			b.WriteString(css)
			return b.String()
		}
		b.WriteString(css[:start])
		end := strings.Index(css[start:], "*/")
		if end < 0 {
			return b.String() // unterminated comment: nothing after it counts
		}
		css = css[start+end+2:]
	}
}

// readSource reads a file from this package's directory.
func readSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(b)
}

// readRepoFile reads a file relative to the repository root. Tests run in the
// package directory, so the root is two levels up from internal/app.
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return string(b)
}
