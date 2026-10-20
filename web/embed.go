//go:build ui

package web

import (
	"embed"
	"io/fs"
)

// The all: prefix keeps SvelteKit's _app directory, which a plain pattern skips.
//
//go:embed all:build
var build embed.FS

// FS returns the built web app.
func FS() fs.FS {
	sub, err := fs.Sub(build, "build")
	if err != nil {
		panic(err) // "build" is a valid path, so this can't happen.
	}
	return sub
}
