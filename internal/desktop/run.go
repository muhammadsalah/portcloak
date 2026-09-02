// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

// Package desktop is the Wails shell: the window, the menu, the event bridge,
// and the service registry that exposes internal/app's controllers to the
// frontend.
//
// It is a package of its own so that internal/app — the composition root and
// every controller — imports no Wails. Wails on Linux is cgo over GTK, and a
// command-line binary that reached the controllers through this package would
// need a webview toolkit installed in order to capture a realm in a terminal.
//
// Nothing below this package may import github.com/wailsapp/wails.
// TestHeadless_NoWailsImportBelowTheDesktopPackage is what enforces it.
package desktop

import (
	_ "embed"
	"fmt"
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"

	"portcloak/frontend"
	"portcloak/internal/app"
	"portcloak/internal/engine/config"
)

// appIcon is the 512px rendering of the mark, generated from
// build/icons/appicon-square.svg by build/icons/generate.sh.
//
// Wails uses it for the About dialog on every platform and, on Linux, for the
// window manager's own title bar and task list. The bundled icons under
// build/darwin and build/windows are what a packaged application shows in the
// Dock and in Explorer; this one is what the running process can hand out.
//
//go:embed appicon.png
var appIcon []byte

// Run starts the desktop application.
func Run(build app.Build) error {
	eng, err := app.NewEngine(build.Version)
	if err != nil {
		return err
	}

	// Housekeeping first, and only if nothing else is here: adopt jobs that were
	// running when the process last died, and sweep index files and decrypted
	// working directories a crash left behind.
	//
	// Before the shared claim below, not after. A process holding the folder
	// shared cannot then take it exclusively — advisory locks conflict between
	// open file descriptions, so it would be waiting for itself.
	eng.SweepIfSolelyHere()

	// The window says it is here, and keeps saying so until shutdown.
	//
	// Shared, not exclusive: a window open all afternoon that held the folder to
	// itself would refuse every terminal command for the whole afternoon, and
	// watching a capture from a terminal while the window runs it is the point
	// of having both. What needs the folder to itself is narrower — see
	// config.LockMode — and is taken for as long as it takes and no longer.
	//
	// SingleInstance below stops a second *window*; it knows nothing about a
	// second binary, which is why the guarantee lives in the lock and not there.
	held, err := config.Acquire(eng.Home(), config.LockShared,
		config.Holder{Program: "PortCloak"})
	if err != nil {
		_ = eng.Close()
		return err
	}
	eng.Hold(held)
	// NewEngine derives a build from the version alone, which is all a test
	// binary can know. Replace it with the one main was linked with, so the
	// About panel reports the stamped commit rather than a recovered one.
	eng.Build = build

	// Wails' own system messages go through PortCloak's logger, so a support
	// request needs one file rather than one file plus whatever the terminal
	// happened to still have scrolled back. A release build only wants the
	// warnings; a development build wants the startup detail too.
	logLevel := slog.LevelInfo
	if productionBuild {
		logLevel = slog.LevelWarn
	}

	wapp := application.New(application.Options{
		Name:        "PortCloak",
		Description: "Move Keycloak realms between environments with full fidelity.",
		Icon:        appIcon,
		Logger:      eng.Log.Logger,
		LogLevel:    logLevel,
		Services:    controllers(eng),
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(frontend.Assets()),
			// The asset server logs a line per request. In development that is
			// a useful trace; in a release build it is thousands of lines of
			// noise between the entries someone actually needs to read.
			DisableLogging: productionBuild,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Linux: application.LinuxOptions{
			// Sets g_set_prgname, which is how a window manager matches a
			// running window to the launcher that started it. It has to equal
			// StartupWMClass in build/linux/portcloak.desktop; when the two
			// disagree the taskbar shows a second, iconless entry beside the
			// real one.
			ProgramName: "portcloak",
		},
		Windows: application.WindowsOptions{
			// The default is "WailsWebviewWindow", which is what automation
			// tools, window managers and crash reports would call PortCloak.
			WndClass: "PortCloakWindow",
		},

		// A second launch raises the window that already exists instead of
		// opening a rival.
		//
		// This is the courtesy, not the guarantee. What actually stops two
		// PortCloaks sharing one ~/.portcloak — the same job store, the same
		// inspection index, the same staging area — is the advisory lock taken
		// above, because SingleInstance only sees other copies of this binary
		// and pcloak is a different one. Raising the window is still worth
		// doing: it is what somebody double-clicking the icon actually wants,
		// and a lock refusal is not.
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "io.portcloak.app",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if w := wappWindow(); w != nil {
					w.Restore()
					w.Focus()
				}
			},
		},

		// A panic in a bound method would otherwise take the process down with
		// nothing written anywhere. Capture is long-running and destructive to
		// redo, so the stack has to reach the log file before the window goes.
		PanicHandler: func(d *application.PanicDetails) {
			eng.Log.Error("the application panicked",
				"error", d.Error,
				"time", d.Time,
				"stack", d.FullStackTrace)
		},
		ErrorHandler: func(err error) {
			eng.Log.Error("the application reported an error", "error", err)
		},
		WarningHandler: func(msg string) {
			eng.Log.Warn("the application reported a warning", "warning", msg)
		},

		// Closing the window must not strand a running capture mid-flight in
		// somebody else's cluster: the engine's Close tears down clones and
		// releases leases, and the shutdown blocks until it has.
		OnShutdown: func() {
			if err := eng.Close(); err != nil {
				eng.Log.Error("shutting the engine down", "error", err)
			}
		},
	})

	// Replace Wails' default menu before the application starts. Left nil it
	// installs a View menu carrying Reload and Toggle DevTools, and a Help menu
	// whose one entry navigates the main window to https://wails.io. See
	// applicationMenu.
	wapp.Menu.Set(applicationMenu(wapp))

	// The engine emits progress into the Wails event bus. Building this bridge
	// here, rather than handing the engine a Wails dependency, is what keeps
	// the dependency rule intact.
	eng.AttachSink(newEventBridge(wapp))

	eng.Log.Info("PortCloak starting", "build", build.String())

	if !frontend.Built() {
		wapp.Logger.Warn("the frontend has not been built; run `npm --prefix frontend ci && npm --prefix frontend run build`")
	}

	wapp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "PortCloak",
		Width:     1440,
		Height:    900,
		MinWidth:  1100,
		MinHeight: 700,

		// Stated rather than left to the default, because "the window resizes"
		// is a requirement here and not an accident of the zero value. The
		// minimums above are the point below which the inspector's two-pane
		// layout starts overlapping itself.
		DisableResize: false,

		// The inspector, and the right-click menu that offers it. Wails already
		// compiles openDevTools out under the production tag; setting these
		// closes the other two doors — the keyboard shortcut and the context
		// menu — and keeps development builds fully equipped.
		DevToolsEnabled:            !productionBuild,
		DefaultContextMenuDisabled: productionBuild,

		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(240, 240, 240),
	})

	if err := wapp.Run(); err != nil {
		return fmt.Errorf("running the application: %w", err)
	}
	// Shutdown is handled by OnShutdown above, which Wails calls on every exit
	// path — including the ones where Run does not return, as it does not on
	// macOS when the Dock quits the app.
	return nil
}

// wappWindow returns the application's current window, or nil before one
// exists. A second launch can arrive before the first has finished starting.
func wappWindow() application.Window {
	app := application.Get()
	if app == nil {
		return nil
	}
	return app.Window.Current()
}
