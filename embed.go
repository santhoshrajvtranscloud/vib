package vib

import (
	"embed"
	"io/fs"
)

//go:embed all:web/dist
var DistEmbedFS embed.FS

// DistFS returns an fs.FS sub-rooted at "web/dist".
func DistFS() (fs.FS, error) {
	return fs.Sub(DistEmbedFS, "web/dist")
}
