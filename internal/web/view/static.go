package view

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
)

// staticETags maps each /static/ URL path to a strong ETag over the asset's
// bytes, computed once at startup from the embedded FS.
//
// The assets are baked into the binary, so their FS modification times are the
// zero value and http.FileServer sends no Last-Modified. Without an ETag either,
// a response carries no validator at all and the browser is free to guess how
// long to keep it — which is how a replaced favicon keeps showing the old mark
// long after a deploy. Hashing the content gives every asset a validator that
// changes exactly when the asset does.
var staticETags = buildStaticETags()

func buildStaticETags() map[string]string {
	etags := make(map[string]string)
	// An error here would mean the embed directive and this walk disagree, which
	// is a build-time defect rather than a runtime condition; the assets still
	// serve correctly without a validator, so skip what can't be read.
	_ = fs.WalkDir(assetsFS, "static", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(assetsFS, name)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(b)
		etags["/"+name] = `"` + hex.EncodeToString(sum[:16]) + `"`
		return nil
	})
	return etags
}

// StaticHandler serves the embedded assets under /static/ with a content ETag.
//
// Cache-Control is no-cache rather than a max-age: it lets the browser keep the
// copy but requires it to revalidate, so an asset that changed is picked up on
// the next page load while an unchanged one costs a 304 with no body. For a
// handful of small files on a single-operator panel that trade is the right way
// round — correctness after a deploy matters more than saving the round trip.
// http.ServeContent answers the conditional request from the ETag we set here.
func StaticHandler() http.Handler {
	files := http.FileServer(http.FS(assetsFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if etag, ok := staticETags[path.Clean(r.URL.Path)]; ok {
			w.Header().Set("ETag", etag)
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}
