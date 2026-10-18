//go:build embedui

package server

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
)

// Templates and static assets are embedded at build time.
//go:embed all:web/templates
var webTemplates embed.FS

//go:embed all:web/static
var webStatic embed.FS

// indexTemplate is the parsed index.html template with version injection.
var indexTemplate *template.Template

func init() {
	tmpl, err := template.New("index.html").ParseFS(webTemplates, "web/templates/index.html")
	if err != nil {
		panic(fmt.Errorf("parse index.html template: %w", err))
	}
	indexTemplate = tmpl
}

// webuiHandler serves the embedded SPA using Go templates for the main page
// and static files from /static/*.
// Public (no JWT) — auth is enforced client-side and on /api.
func (s *Server) webuiHandler() http.Handler {
	staticRoot, err := fs.Sub(webStatic, "web/static")
	if err != nil {
		panic(err)
	}

	staticHandler := http.StripPrefix("/static/", http.FileServerFS(staticRoot))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve static files from /static/*
		if strings.HasPrefix(r.URL.Path, "/static/") {
			staticHandler.ServeHTTP(w, r)
			return
		}

		// Serve favicon.ico
		if r.URL.Path == "/favicon.ico" {
			if serveStaticFile(w, r, staticRoot, "favicon.svg") {
				return
			}
			http.NotFound(w, r)
			return
		}

		// Serve index.html template for all other routes (SPA)
		// Inject version and config
		data := struct {
			Version    string
			BuildTime  string
			CommitHash string
			Features   map[string]bool
		}{
			Version:    BuildVersion,
			BuildTime:  BuildTime,
			CommitHash: CommitHash,
			Features: map[string]bool{
				"trafficCapture":  s.m.TrafficEnabled(),
				"webSearch":       s.m.WebSearchEnabled(),
				"vulnintel":       s.m.VulnintelEnabled(),
				"proxypool":       s.m.ProxyPoolEnabled(),
				"c2":              s.m.C2Enabled(),
				"webShell":        s.m.WebShellEnabled(),
				"noaCompaction":   s.m.NoaCompactionEnabled(),
				"llmPool":         s.m.LLMPoolEnabled(),
			},
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := indexTemplate.Execute(w, data); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})
}

// serveStaticFile serves a file from the embedded static FS if it exists.
func serveStaticFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) bool {
	f, err := fsys.Open(name)
	if err != nil {
		return false
	}
	st, statErr := f.Stat()
	_ = f.Close()
	if statErr != nil || st.IsDir() {
		return false
	}
	http.ServeFileFS(w, r, fsys, name)
	return true
}

// Template variables injected at build time via -ldflags
var (
	BuildTime  string = ""
	CommitHash string = ""
)