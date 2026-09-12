package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// mountStatic serves the built front end under basePath.
//
// The app is a single-page router, so any path that is not a real file falls
// back to index.html and lets the client route it. Requests under the API
// prefix are already matched by more specific patterns and never reach here.
func (s *Server) mountStatic(mux *http.ServeMux, basePath string) {
	dir := s.cfg.WebDir
	fileServer := http.FileServer(http.Dir(dir))
	prefix := basePath
	if prefix == "" {
		prefix = "/"
	}

	apiPrefix := basePath + "/api/"

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// An unmatched API path must NOT fall through to the single-page app.
		//
		// It did, and the result was that every mistyped or not-yet-deployed
		// API route answered 200 with index.html. A caller checking the status
		// code sees success and has to notice the content-type to find out it
		// got a web page — which is exactly how a stale deployment hides. Any
		// client, and any check of whether a route exists, is misled by it.
		if strings.HasPrefix(r.URL.Path, apiPrefix) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"reason":"unknown_route",` +
				`"error":"no such API route on this server"}` + "\n"))
			return
		}

		rel := strings.TrimPrefix(r.URL.Path, basePath)
		if rel == "" || rel == "/" {
			serveIndex(w, r, dir)
			return
		}
		clean := filepath.Clean(rel)
		// filepath.Join below already contains traversal, but rejecting it
		// here makes the intent explicit rather than relying on a side effect.
		if strings.Contains(clean, "..") {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		full := filepath.Join(dir, clean)
		info, err := os.Stat(full)
		if err != nil || info.IsDir() {
			serveIndex(w, r, dir)
			return
		}
		// Hashed asset filenames are immutable; index.html must never be
		// cached or a deploy leaves browsers on the old bundle.
		if strings.HasPrefix(clean, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.StripPrefix(basePath, fileServer).ServeHTTP(w, r)
	})

	if basePath == "" {
		mux.Handle("/", handler)
		return
	}
	mux.Handle(prefix+"/", handler)
	// Without this the bare prefix with no trailing slash 301s to the prefix
	// with one, which is a wasted round trip on the demo's own front door.
	mux.Handle(prefix, handler)
}

func serveIndex(w http.ResponseWriter, r *http.Request, dir string) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	http.ServeFile(w, r, filepath.Join(dir, "index.html"))
}
