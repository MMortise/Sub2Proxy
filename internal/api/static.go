package api

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// serveStatic serves the embedded web UI. Unknown non-asset paths fall back to
// index.html so client-side routing works (web-console spec: SPA fallback). When
// no staticFS is configured (dev), it returns a helpful placeholder.
func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if s.staticFS == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("sub2proxy API is running. Web UI is not embedded in this build.\n"))
		return
	}

	upath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if upath == "" {
		upath = "index.html"
	}
	if serveFile(w, r, s.staticFS, upath) {
		return
	}
	// Fall back to index.html for SPA routes (path with no file extension).
	if path.Ext(upath) == "" {
		if serveFile(w, r, s.staticFS, "index.html") {
			return
		}
	}
	http.NotFound(w, r)
}

func serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) bool {
	f, err := fsys.Open(name)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			http.Error(w, "read error", http.StatusInternalServerError)
			return true
		}
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		return false
	}
	http.ServeContent(w, r, name, info.ModTime(), rs)
	return true
}
