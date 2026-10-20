//go:build !ui

package web

import "io/fs"

// FS returns nil: this binary was built without the "ui" tag, so it has no web app.
// Plain go build, go test and go vet work this way without a web build.
func FS() fs.FS {
	return nil
}
