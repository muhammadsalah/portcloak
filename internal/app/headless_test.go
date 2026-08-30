// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Wails on Linux is cgo over GTK. A single import of it anywhere below
// internal/desktop would mean the command line could not be built — or even
// vetted — on a machine with no webview toolkit installed, which is most of the
// machines a realm gets captured from: CI runners, jump boxes, headless
// servers.
//
// The failure would not look like this, either. It would look like a linker
// error about a C header, several packages away from the import that caused it,
// on somebody else's build. So the rule is checked directly against the source.
//
// This reads every file regardless of its build constraints, which is stricter
// than asking the toolchain: a Wails import behind `//go:build linux` compiles
// fine here and breaks exactly the build this rule protects.
func TestHeadless_NoWailsImportBelowTheDesktopPackage(t *testing.T) {
	const wails = "github.com/wailsapp/wails"

	root := repoRoot(t)
	// internal/desktop is the one place allowed to import it, and cmd/portcloak
	// is allowed to import internal/desktop. Everything else listed here has to
	// compile with CGO_ENABLED=0 and no toolkit present.
	for _, dir := range []string{
		filepath.Join("cmd", "pcloak"),
		filepath.Join("internal", "app"),
		filepath.Join("internal", "cli"),
		filepath.Join("internal", "engine"),
	} {
		abs := filepath.Join(root, dir)
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			// internal/cli and cmd/pcloak arrive later in the rollout. A rule
			// that fails because the thing it protects does not exist yet is a
			// rule nobody keeps.
			continue
		}

		err := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range file.Imports {
				p, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					continue
				}
				if strings.HasPrefix(p, wails) {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s imports %s; only internal/desktop may. "+
						"This is what stops pcloak needing GTK to capture a realm in a terminal.", rel, p)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
}

// repoRoot is the directory holding go.mod. Tests run in their own package
// directory, and this package is two levels down.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("the repository root is not where this test expects it: %v", err)
	}
	return root
}
