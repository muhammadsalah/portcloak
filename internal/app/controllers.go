package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// controllers is the registry the Wails application binds to the frontend.
//
// It is a single list rather than wiring scattered through Run so that what the
// UI can reach is readable in one place.
//
// None of these implement Wails' ServiceName interface, deliberately. That
// method names a service in log lines only; a bound method is addressed by its
// fully-qualified Go name, so Call.ByName("ConfigController.Load") does not
// resolve and Call.ByName("portcloak/internal/app.ConfigController.Load")
// does. A ServiceName returning "ConfigController" reads like it sets the
// address and does not, which is how the frontend came to use the short form
// for every call on every screen. TestBindings_EveryFrontendCallResolves is
// what actually holds the two sides together now.
func controllers(eng *Engine) []application.Service {
	return []application.Service{
		application.NewService(NewConfigController(eng)),
		application.NewService(NewCaptureController(eng)),
		application.NewService(NewSnapshotController(eng)),
		application.NewService(NewInspectController(eng)),
		application.NewService(NewRestoreController(eng)),
		application.NewService(NewJobsController(eng)),
		application.NewService(NewKeysController(eng)),
		application.NewService(NewMaintenanceController(eng)),
	}
}
