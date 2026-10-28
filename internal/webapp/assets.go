package webapp

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robinjoseph08/web-app-template/pkg/errorstack"
)

// embedded contains the Vite production build. The tracked placeholder keeps
// normal Go builds valid before the frontend has been built.
//
//go:embed all:dist
var embedded embed.FS

const indexFile = "index.html"

// hashedAssets is where Vite puts the files whose names contain a hash of
// their content, like assets/index-B80ZTyZ2.js. A new build gets new names, so
// they can be cached forever. Files from app/public keep their names and go at
// the top level, so app/public must never have an assets/ folder: its files
// would be cached forever too.
const hashedAssets = "assets/"

// compressible are the text types worth gzipping. Images and fonts are
// already compressed.
var compressible = map[string]bool{
	".css": true, ".html": true, ".js": true, ".json": true, ".map": true, ".svg": true, ".txt": true, ".webmanifest": true, ".xml": true,
}

// minimumGzipSize skips files too small to be worth compressing.
const minimumGzipSize = 1024

type file struct {
	content []byte
	etag    string

	// The gzipped copy is made once per process: in the background at startup,
	// or by the first request that wants it, whichever comes first.
	gzipOnce sync.Once
	gzipped  []byte
}

// Handler returns the production frontend handler when a Vite build is
// embedded. Development uses Vite directly, so a source-only build has no
// frontend handler.
func Handler() (http.Handler, bool, error) {
	root, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil, false, errorstack.Capture(err)
	}
	files, err := loadFiles(root)
	if err != nil {
		return nil, false, errorstack.Capture(err)
	}
	if _, ok := files[indexFile]; !ok {
		return nil, false, nil
	}
	go func() {
		for name, f := range files {
			if worthCompressing(name, f) {
				f.gzip()
			}
		}
	}()
	return &spaHandler{files: files}, true, nil
}

func loadFiles(root fs.FS) (map[string]*file, error) {
	files := map[string]*file{}
	err := fs.WalkDir(root, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || path.Base(name) == ".gitkeep" {
			return err
		}
		content, err := fs.ReadFile(root, name)
		if err != nil {
			return err
		}
		// Embedded files have no modification time, so caches revalidate with
		// a fingerprint of the content instead.
		sum := sha256.Sum256(content)
		files[name] = &file{content: content, etag: hex.EncodeToString(sum[:8])}
		return nil
	})
	return files, err
}

type spaHandler struct {
	files map[string]*file
}

func newHandler(root fs.FS) (http.Handler, error) {
	files, err := loadFiles(root)
	if err != nil {
		return nil, err
	}
	return &spaHandler{files: files}, nil
}

func (h *spaHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	requested := strings.TrimPrefix(path.Clean("/"+request.URL.Path), "/")
	if requested == "" {
		requested = indexFile
	}
	if !fs.ValidPath(requested) {
		http.NotFound(response, request)
		return
	}

	name := requested
	if _, ok := h.files[name]; !ok {
		name = path.Join(requested, indexFile)
	}
	if _, ok := h.files[name]; !ok {
		// A missing asset is a 404. Anything else is a frontend route, which
		// the app renders from index.html, including its own not found page.
		if requested == "assets" || strings.HasPrefix(requested, hashedAssets) {
			http.NotFound(response, request)
			return
		}
		if path.Ext(requested) != "" && !strings.Contains(request.Header.Get("Accept"), "text/html") {
			http.NotFound(response, request)
			return
		}
		name = indexFile
	}
	h.serveFile(response, request, name)
}

func (h *spaHandler) serveFile(response http.ResponseWriter, request *http.Request, name string) {
	f := h.files[name]
	header := response.Header()
	header.Set("Cache-Control", cacheControl(name))
	header.Set("X-Content-Type-Options", "nosniff")
	if path.Ext(name) == ".html" {
		// The app shell can't be framed by other sites, and links out of it
		// don't tell the destination which page they came from.
		header.Set("Content-Security-Policy", "frame-ancestors 'none'")
		header.Set("Referrer-Policy", "no-referrer")
	}
	// Set the type from the name, so it's the same gzipped or not.
	if contentType := typeByName(name); contentType != "" {
		header.Set("Content-Type", contentType)
	}

	content, etag := f.content, f.etag
	if worthCompressing(name, f) {
		header.Add("Vary", "Accept-Encoding")
		if acceptsGzip(request) {
			content, etag = f.gzip(), f.etag+"-gzip"
			header.Set("Content-Encoding", "gzip")
			// Compressed responses are always whole. Browsers don't ask for
			// ranges of these files, and an error for a bad range would
			// otherwise go out labeled as gzip.
			request.Header.Del("Range")
			request.Header.Del("If-Range")
		}
	}
	// Each encoding is a different set of bytes, so each gets its own ETag.
	header.Set("ETag", `"`+etag+`"`)
	http.ServeContent(response, request, name, time.Time{}, bytes.NewReader(content))
}

func worthCompressing(name string, f *file) bool {
	return compressible[strings.ToLower(path.Ext(name))] && len(f.content) >= minimumGzipSize
}

func (f *file) gzip() []byte {
	f.gzipOnce.Do(func() {
		var buffer bytes.Buffer
		writer := gzip.NewWriter(&buffer)
		_, _ = writer.Write(f.content)
		_ = writer.Close()
		f.gzipped = buffer.Bytes()
	})
	return f.gzipped
}

// acceptsGzip reports whether the request's Accept-Encoding lists gzip with a
// nonzero quality (gzip;q=0 means "not gzip").
func acceptsGzip(request *http.Request) bool {
	for part := range strings.SplitSeq(request.Header.Get("Accept-Encoding"), ",") {
		encoding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(encoding), "gzip") {
			continue
		}
		for param := range strings.SplitSeq(params, ";") {
			key, value, _ := strings.Cut(strings.TrimSpace(param), "=")
			if strings.EqualFold(strings.TrimSpace(key), "q") {
				quality, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				return err == nil && quality > 0
			}
		}
		return true
	}
	return false
}

// typeByName names the content types of the files a Vite build contains, so a
// gzipped body isn't sniffed as application/x-gzip. Anything else is left to
// http.ServeContent.
func typeByName(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json", ".map":
		return "application/json"
	case ".webmanifest":
		return "application/manifest+json"
	case ".svg":
		return "image/svg+xml"
	case ".xml":
		return "text/xml; charset=utf-8"
	case ".txt":
		return "text/plain; charset=utf-8"
	}
	return ""
}

// cacheControl decides how long browsers keep a file. HTML says which build is
// current, so it's always revalidated. Hashed assets never change. Everything
// else (icons, the manifest) is kept for an hour, then revalidated with its
// ETag.
func cacheControl(name string) string {
	switch {
	case path.Ext(name) == ".html":
		return "no-cache"
	case strings.HasPrefix(name, hashedAssets):
		return "public, max-age=31536000, immutable"
	default:
		return "public, max-age=3600"
	}
}
