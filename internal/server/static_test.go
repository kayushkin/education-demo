package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUnknownApiRouteIs404NotTheSinglePageApp pins that an unmatched API path
// fails loudly.
//
// It used to fall through to the SPA and answer 200 with index.html, so a
// mistyped route -- or a route that exists in the source but not in the
// deployed binary -- looked like a success to anything reading the status code.
// That is precisely how a stale deployment stays hidden, and it fooled a check
// of whether this very server had been redeployed.
func TestUnknownApiRouteIs404NotTheSinglePageApp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"),
		[]byte("<!doctype html><html><body>app</body></html>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	srv := New(Config{BasePath: "/education-demo", WebDir: dir})
	handler := srv.Handler()

	t.Run("unknown api route", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
			"/education-demo/api/no-such-route", nil))

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("content-type = %q, want application/json", ct)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body is not JSON: %v (%q)", err, rec.Body.String())
		}
		if body["reason"] != "unknown_route" {
			t.Errorf("reason = %q, want unknown_route", body["reason"])
		}
	})

	// A real page route must still reach the app, or fixing the above would
	// break every deep link.
	t.Run("spa deep link still served", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/education-demo/join", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "<html") {
			t.Errorf("deep link did not return the app: %q", rec.Body.String())
		}
	})

	// And a route that DOES exist must be unaffected.
	t.Run("real api route unaffected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
			"/education-demo/api/vocabularies", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})
}
