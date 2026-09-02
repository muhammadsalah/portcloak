// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

package desktop

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"portcloak/internal/app"
)

// controllers is the registry the Wails application binds to the frontend.
//
// It is a single list rather than wiring scattered through Run so that what the
// UI can reach is readable in one place. The controllers themselves live in
// internal/app, which imports no Wails: this file is the whole of the coupling
// between them and the window.
//
// Wails' ServiceName interface does not set the address, and reading it as
// though it does is how the frontend once came to call every method on every
// screen by a name that could not resolve. Wails registers a bound method under
// its fully-qualified Go name, so Call.ByName("ConfigController.Load") fails
// and Call.ByName("portcloak/internal/app.ConfigController.Load") works.
// ServiceName reaches only getServiceName, which Wails uses for log lines and
// for the message it wraps a service startup error in.
//
// Seven of the nine implement it and two do not, which is untidy rather than
// wrong: the two without are logged under their type name instead. Adding it to
// the remaining two, or dropping it from the seven, are both fine; what must not
// happen is anyone concluding from its presence that it names the call address.
// TestBindings_EveryFrontendCallResolves is what actually holds the two sides
// together, and it resolves against internal/app rather than this package —
// so moving a controller up here would fail it.
func controllers(eng *app.Engine) []application.Service {
	return []application.Service{
		application.NewService(app.NewConfigController(eng)),
		application.NewService(app.NewCaptureController(eng)),
		application.NewService(app.NewSnapshotController(eng)),
		application.NewService(app.NewInspectController(eng)),
		application.NewService(app.NewRestoreController(eng)),
		application.NewService(app.NewJobsController(eng)),
		application.NewService(app.NewKeysController(eng)),
		application.NewService(app.NewAuditController(eng)),
		application.NewService(app.NewSettingsController(eng)),
	}
}
