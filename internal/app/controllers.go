package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// controllers is the registry the Wails application binds to the frontend.
//
// It is a single list rather than wiring scattered through Run so that what the
// UI can reach is readable in one place.
func controllers(eng *Engine) []application.Service {
	return []application.Service{
		application.NewService(NewConfigController(eng)),
		application.NewService(NewCaptureController(eng)),
		application.NewService(NewSnapshotController(eng)),
		application.NewService(NewInspectController(eng)),
		application.NewService(NewRestoreController(eng)),
		application.NewService(NewJobsController(eng)),
		application.NewService(NewMaintenanceController(eng)),
	}
}
