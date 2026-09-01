package websharerelay

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed webroot/* webroot/assets/*
var embeddedWebRoot embed.FS

var webStaticFS = mustWebStaticFS()

func mustWebStaticFS() http.FileSystem {
	sub, err := fs.Sub(embeddedWebRoot, "webroot")
	if err != nil {
		panic(err)
	}
	return http.FS(sub)
}

func serveStaticFallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "." || name == "" {
		serveStaticIndex(w, r)
		return
	}
	if f, err := webStaticFS.Open(name); err == nil {
		_ = f.Close()
		w.Header().Set("Cache-Control", "no-store")
		http.FileServer(webStaticFS).ServeHTTP(w, r)
		return
	}
	serveStaticIndex(w, r)
}

func serveStaticIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	b, err := embeddedWebRoot.ReadFile("webroot/index.html")
	if err != nil {
		http.Error(w, "web root unavailable", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(b)
}
