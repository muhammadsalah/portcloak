// Package frontend embeds the built web assets into the binary, so PortCloak
// ships as a single file with no server component (NFR-4).
//
// dist/ is produced by `npm run build`. A checked-in placeholder keeps
// `go build ./...` working on a machine that has never run npm — the binary
// then serves an empty asset tree, which is a broken UI but a correct compile.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Assets is the built frontend, rooted so that index.html is at the top.
func Assets() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// The embed directive guarantees dist exists; a failure here is a
		// build-time impossibility rather than a runtime condition.
		panic(err)
	}
	return sub
}

// Built reports whether the embedded assets contain a real build. The app uses
// it to say something useful instead of opening a blank window.
func Built() bool {
	_, err := fs.Stat(dist, "dist/index.html")
	return err == nil
}
