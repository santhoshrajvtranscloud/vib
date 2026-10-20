package api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/santhosh/vib"
)

// getSPAFileSystem determines whether to serve from the local filesystem (dev mode)
// or from embedded assets compiled into the single binary.
func getSPAFileSystem() (http.FileSystem, bool) {
	// 1. Check if local web/dist directory exists on disk (development mode priority)
	if localDir := findWebDistDir(); localDir != "" {
		return http.Dir(localDir), false
	}

	// 2. Check embedded assets in vib.DistFS()
	if embeddedFS, err := vib.DistFS(); err == nil {
		if f, err := embeddedFS.Open("index.html"); err == nil {
			_ = f.Close()
			return http.FS(embeddedFS), true
		}
	}

	return nil, false
}

// handleStatic serves static assets (from local disk or embedded FS) with SPA fallback.
func (s *Server) handleStatic(hfs http.FileSystem, isEmbedded bool) http.HandlerFunc {
	fileServer := http.FileServer(hfs)

	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") {
			http.NotFound(w, r)
			return
		}

		cleanPath := strings.TrimPrefix(filepath.Clean(r.URL.Path), "/")
		if cleanPath == "" || cleanPath == "." {
			cleanPath = "index.html"
		}

		// Try to open the requested file
		f, err := hfs.Open(cleanPath)
		if err == nil {
			defer f.Close()
			fi, err := f.Stat()
			if err == nil && !fi.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// Fallback to index.html for SPA routing
		indexFile, err := hfs.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer indexFile.Close()

		indexInfo, err := indexFile.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}

		if seeker, ok := indexFile.(io.ReadSeeker); ok {
			http.ServeContent(w, r, "index.html", indexInfo.ModTime(), seeker)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.Copy(w, indexFile)
	}
}

// findWebDistDir locates the compiled web/dist directory relative to current or parent directories.
func findWebDistDir() string {
	candidates := []string{
		"web/dist",
		"../web/dist",
		"../../web/dist",
	}
	for _, dir := range candidates {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			if _, err := os.Stat(filepath.Join(dir, "index.html")); err == nil {
				return dir
			}
		}
	}
	return ""
}
