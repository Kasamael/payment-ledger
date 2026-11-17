// Package web serves the demo page. The files are built into the binary with embed,
// so the service is still a single file to deploy.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var static embed.FS

// Handler serves index.html, app.js and the rest of the static folder.
func Handler() http.Handler {
	files, err := fs.Sub(static, "static")
	if err != nil {
		// Happens only if the embed path above is wrong: a programming error, not a runtime one.
		panic(err)
	}
	return http.FileServerFS(files)
}
