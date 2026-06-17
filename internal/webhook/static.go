package webhook

import (
	"embed"
	"io/fs"
)

//go:embed static/*
var staticFiles embed.FS

// staticFS returns the embedded dashboard assets rooted at "static/", so
// they're served at "/", "/style.css", "/app.js" etc — the binary ships
// the UI with no separate static/ directory needed on disk, the same way
// the Prometheus binary embeds its web assets.
func staticFS() fs.FS {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err) // static/ is embedded at build time; this can't fail at runtime
	}
	return sub
}
