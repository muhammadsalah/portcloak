// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

//go:build production

package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// addDeveloperMenu is a no-op in a production build: there is no Developer
// menu, and the inspector it would open is itself compiled out of Wails by the
// same build tag.
func addDeveloperMenu(*application.Menu) {}
