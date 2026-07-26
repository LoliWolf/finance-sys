package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed frontend_dist
var frontendAssets embed.FS

func newFrontendHandler() http.Handler {
	root, err := fs.Sub(frontendAssets, "frontend_dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
			http.NotFound(w, r)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, statErr := fs.Stat(root, path); statErr != nil {
				clone := r.Clone(r.Context())
				clone.URL.Path = "/"
				fileServer.ServeHTTP(w, clone)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}
