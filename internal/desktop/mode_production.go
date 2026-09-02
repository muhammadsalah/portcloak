// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

//go:build production

package desktop

// productionBuild reports whether this binary was compiled for release.
// See mode_dev.go for why this is a build tag rather than a runtime setting.
const productionBuild = true
