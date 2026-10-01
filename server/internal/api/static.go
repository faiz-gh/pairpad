package api

import (
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
)

// encodings are the precompressed variants the web build ships
// (adapter-static `precompress`), in order of preference.
var encodings = []struct{ token, ext string }{
	{"br", ".br"},
	{"gzip", ".gz"},
}

// Static serves the built single-page app from dir.
//
//   - Files are served as-is; when the client accepts it, a precompressed
//     .br or .gz sibling is sent instead, so nothing is compressed per request.
//   - Hashed assets under _app/immutable/ are cached for a year; everything
//     else (notably index.html) must revalidate so deploys take effect.
//   - Paths without a file extension that don't exist are client-side routes
//     (e.g. /k7x2p9) and get index.html. Missing files with an extension get
//     404 rather than a confusing HTML page.
func Static(dir string) http.Handler {
	fsys := os.DirFS(dir)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if info, err := fs.Stat(fsys, name); err != nil || info.IsDir() {
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			name = "index.html"
		}
		serveFile(w, r, fsys, name)
	})
}

func serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	h := w.Header()
	if strings.HasPrefix(name, "_app/immutable/") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		h.Set("Content-Type", ct)
	}

	file := name
	for _, enc := range encodings {
		if _, err := fs.Stat(fsys, name+enc.ext); err != nil {
			continue
		}
		h.Add("Vary", "Accept-Encoding")
		if acceptsEncoding(r.Header.Get("Accept-Encoding"), enc.token) {
			file = name + enc.ext
			h.Set("Content-Encoding", enc.token)
			break
		}
	}

	f, err := fsys.Open(file)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// ServeContent handles HEAD, Range and If-Modified-Since.
	http.ServeContent(w, r, name, info.ModTime(), rs)
}

// acceptsEncoding reports whether an Accept-Encoding header allows token
// (ignoring q-values other than an explicit q=0).
func acceptsEncoding(header, token string) bool {
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(fields[0]), token) {
			continue
		}
		for _, param := range fields[1:] {
			if q := strings.TrimSpace(param); q == "q=0" || q == "q=0.0" || q == "q=0.00" || q == "q=0.000" {
				return false
			}
		}
		return true
	}
	return false
}
