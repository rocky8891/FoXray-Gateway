package gateway

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStaticEntryIgnoresOldCacheValidators(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "index.html")
	if err := os.WriteFile(entry, []byte("<html>new-release</html>"), 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(entry, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	app := &App{ui: dir}
	for _, path := range []string{"/", "/monitoring"} {
		r := httptest.NewRequest("GET", "http://192.168.1.8:18880"+path, nil)
		r.RemoteAddr = "192.168.1.10:5500"
		r.Header.Set("If-Modified-Since", "Fri, 13 Nov 2026 10:06:12 GMT")
		r.Header.Set("If-None-Match", "old-build")
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), "new-release") {
			t.Fatal("browser was not sent current entry", path, w.Code, w.Header())
		}
	}
	r := httptest.NewRequest("GET", "http://192.168.1.8:18880/assets/deleted.js", nil)
	r.RemoteAddr = "192.168.1.10:5500"
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	if w.Code != 404 || strings.Contains(w.Body.String(), "<html>") {
		t.Fatal("missing script returned HTML")
	}
}

func TestPastedChineseFullStopRedirectsToRoot(t *testing.T) {
	app := &App{ui: t.TempDir()}
	r := httptest.NewRequest("GET", "http://192.168.1.8:18880/%E3%80%82", nil)
	r.RemoteAddr = "192.168.1.10:5500"
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
		t.Fatal("pasted punctuation not normalized", w.Code)
	}
}
