// Package app is the Wails binding layer: it turns engine capabilities into
// methods the frontend can call, and engine events into messages the frontend
// can subscribe to.
//
// No business logic lives here. Every method in this package resolves
// configuration, calls one engine entry point, and shapes the result for a
// screen. If something in here starts making decisions, it belongs in the
// engine instead — which is the rule that keeps the engine testable headlessly.
package app

import (
	"fmt"
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"

	"portcloak/frontend"
)

// Run starts the desktop application.
func Run(version string) error {
	eng, err := NewEngine(version)
	if err != nil {
		return err
	}

	wapp := application.New(application.Options{
		Name:        "PortCloak",
		Description: "Move Keycloak realms between environments with full fidelity.",
		LogLevel:    slog.LevelInfo,
		Services: controllers(eng),
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(frontend.Assets()),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	// The engine emits progress into the Wails event bus. Building this bridge
	// here, rather than handing the engine a Wails dependency, is what keeps
	// the dependency rule intact.
	eng.AttachSink(newEventBridge(wapp))

	if !frontend.Built() {
		wapp.Logger.Warn("the frontend has not been built; run `npm --prefix frontend ci && npm --prefix frontend run build`")
	}

	wapp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "PortCloak",
		Width:     1440,
		Height:    900,
		MinWidth:  1100,
		MinHeight: 700,
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(240, 240, 240),
	})

	// Startup housekeeping an operator should never have to ask for: adopt
	// jobs that were running when the process last died, sweep index files a
	// crash left behind, and look for orphaned ephemeral clones.
	go eng.StartupSweep()

	if err := wapp.Run(); err != nil {
		return fmt.Errorf("running the application: %w", err)
	}
	return eng.Close()
}
