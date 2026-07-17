// Package web embeds the built frontend (web/dist) into the binary so the app
// ships as a single file (design D2). The dist directory is produced by the
// frontend build; a placeholder index.html keeps this compilable before then.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// FS returns the embedded dist directory as a filesystem rooted at dist/.
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err) // dist is always embedded at build time
	}
	return sub
}
