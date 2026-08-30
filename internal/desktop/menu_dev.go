// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

//go:build !production

package desktop

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// addDeveloperMenu appends the reload and inspector entries to a development
// build's menu bar.
//
// These are the items Wails' default View menu carries. They are useful while
// building the UI and wrong to ship, so they live behind the same `production`
// build tag Wails itself uses to compile out openDevTools — which means a
// release binary does not merely hide them, it does not contain them.
func addDeveloperMenu(menu *application.Menu) {
	dev := menu.AddSubmenu("Developer")
	dev.AddRole(application.Reload)
	dev.AddRole(application.ForceReload)
	dev.AddRole(application.OpenDevTools)
	dev.AddSeparator()
	dev.AddRole(application.ResetZoom)
	dev.AddRole(application.ZoomIn)
	dev.AddRole(application.ZoomOut)
	dev.AddSeparator()
	dev.AddRole(application.ToggleFullscreen)
}
