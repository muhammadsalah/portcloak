//go:build production

package app

// productionBuild reports whether this binary was compiled for release.
// See mode_dev.go for why this is a build tag rather than a runtime setting.
const productionBuild = true
