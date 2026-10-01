package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func staticDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"index.html":                     "<html>app</html>",
		"index.html.gz":                  "GZ-index",
		"robots.txt":                     "User-agent: *",
		"_app/immutable/chunks/a1.js":    "console.log(1)",
		"_app/immutable/chunks/a1.js.br": "BR-js",
		"_app/immutable/chunks/a1.js.gz": "GZ-js",
		"_app/version.json":              `{"version":"1"}`,
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func get(h http.Handler, target, acceptEncoding string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", target, nil)
	if acceptEncoding != "" {
		r.Header.Set("Accept-Encoding", acceptEncoding)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestStaticServesFilesWithCachePolicy(t *testing.T) {
	h := Static(staticDir(t))

	w := get(h, "/_app/immutable/chunks/a1.js", "")
	if w.Code != 200 || w.Body.String() != "console.log(1)" {
		t.Fatalf("plain asset: %d %q", w.Code, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("hashed asset Cache-Control = %q", cc)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("Content-Type = %q", ct)
	}

	w = get(h, "/robots.txt", "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("robots.txt: %d, Cache-Control %q", w.Code, w.Header().Get("Cache-Control"))
	}
}

func TestStaticPrefersPrecompressedVariants(t *testing.T) {
	h := Static(staticDir(t))
	cases := []struct{ accept, wantBody, wantEnc string }{
		{"gzip, deflate, br, zstd", "BR-js", "br"},
		{"gzip", "GZ-js", "gzip"},
		{"br;q=0, gzip", "GZ-js", "gzip"},
		{"identity", "console.log(1)", ""},
	}
	for _, tc := range cases {
		w := get(h, "/_app/immutable/chunks/a1.js", tc.accept)
		if w.Body.String() != tc.wantBody || w.Header().Get("Content-Encoding") != tc.wantEnc {
			t.Errorf("Accept-Encoding %q: body %q enc %q, want %q %q",
				tc.accept, w.Body.String(), w.Header().Get("Content-Encoding"), tc.wantBody, tc.wantEnc)
		}
		if w.Header().Get("Vary") != "Accept-Encoding" {
			t.Errorf("Accept-Encoding %q: missing Vary", tc.accept)
		}
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
			t.Errorf("Accept-Encoding %q: Content-Type %q (must describe the decoded file)", tc.accept, ct)
		}
	}
}

func TestStaticSPAFallback(t *testing.T) {
	h := Static(staticDir(t))
	for _, target := range []string{"/", "/k7x2p9", "/some/client/route"} {
		w := get(h, target, "")
		if w.Code != 200 || w.Body.String() != "<html>app</html>" {
			t.Errorf("%s: %d %q, want index.html", target, w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: index.html must not be cached long-term", target)
		}
	}
	// Precompressed index.html is used for routes too.
	if w := get(h, "/k7x2p9", "gzip"); w.Body.String() != "GZ-index" {
		t.Errorf("route with gzip: %q", w.Body.String())
	}
}

func TestStaticMissingAssetsAre404(t *testing.T) {
	h := Static(staticDir(t))
	for _, target := range []string{"/favicon.ico", "/_app/immutable/chunks/gone.js"} {
		if w := get(h, target, ""); w.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", target, w.Code)
		}
	}
}

func TestStaticRejectsTraversal(t *testing.T) {
	dir := staticDir(t)
	secret := filepath.Join(filepath.Dir(dir), "secret.txt")
	if err := os.WriteFile(secret, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := Static(dir)
	r := httptest.NewRequest("GET", "/", nil)
	r.URL.Path = "/../secret.txt"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if strings.Contains(w.Body.String(), "nope") {
		t.Fatal("served a file outside the static dir")
	}
}
