// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

//go:build !production

package desktop

// productionBuild reports whether this binary was compiled for release.
//
// It tracks the `production` build tag, which is the same switch Wails uses to
// compile out openDevTools and the inspector. Keeping our own flag on that tag
// rather than on an environment variable or a config setting means the two can
// never disagree: a binary either contains the inspector and admits it, or
// contains neither the inspector nor the code that would have offered it.
const productionBuild = false
