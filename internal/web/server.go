// Package web serves the nvirellia.im profile sheet, its live feed, and static assets.
package web

import (
	"bytes"
	"encoding/json"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"

	"github.com/pelikanade/nvirellia.im/internal/dev"
	"github.com/pelikanade/nvirellia.im/internal/feed"
)

// Feeds supplies the current feed snapshot.
type Feeds interface {
	Feed() feed.Feed
}

type server struct {
	feeds  Feeds
	pages  *template.Template // production: parsed once at startup
	assets fs.FS
	hub    *dev.Hub // non-nil in dev: templates parsed per request
}

// NewHandler routes the site from assets, which must contain templates/ and static/.
func NewHandler(feeds Feeds, assets fs.FS) (http.Handler, error) {
	return buildHandler(feeds, assets, nil)
}

// NewDevHandler serves assets from disk for development. Templates are parsed on
// every request, static files are never cached, and /dev/events streams reload
// signals from hub to open pages.
func NewDevHandler(feeds Feeds, assets fs.FS, hub *dev.Hub) (http.Handler, error) {
	return buildHandler(feeds, assets, hub)
}

func buildHandler(feeds Feeds, assets fs.FS, hub *dev.Hub) (http.Handler, error) {
	s := &server{feeds: feeds, assets: assets, hub: hub}
	if hub == nil {
		pages, err := parseTemplates(assets)
		if err != nil {
			return nil, err
		}
		s.pages = pages
	}
	static, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /api/stats.json", s.stats)
	mux.HandleFunc("GET /fragments/readouts", s.readouts)
	mux.HandleFunc("GET /healthz", healthz)
	if hub != nil {
		mux.Handle("GET /dev/events", hub)
		mux.Handle("GET /static/", http.StripPrefix("/static/", noStore(http.FileServerFS(static))))
	} else {
		mux.Handle("GET /static/", http.StripPrefix("/static/", longCache(http.FileServerFS(static))))
	}
	return mux, nil
}

func parseTemplates(assets fs.FS) (*template.Template, error) {
	return template.New("").ParseFS(assets, "templates/*.html")
}

func noStore(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

// templates returns the page templates. Dev mode re-parses them so edits show on reload.
func (s *server) templates() (*template.Template, error) {
	if s.hub == nil {
		return s.pages, nil
	}
	return parseTemplates(s.assets)
}

func (s *server) index(w http.ResponseWriter, _ *http.Request) {
	s.render(w, "layout.html")
}

// readouts serves the values the page refreshes every 60 s. It shares partials with the page.
func (s *server) readouts(w http.ResponseWriter, _ *http.Request) {
	s.render(w, "live-fragment")
}

func (s *server) render(w http.ResponseWriter, name string) {
	pages, err := s.templates()
	if err != nil {
		log.Printf("web: parse templates: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	p, err := newPage(s.feeds.Feed())
	if err != nil {
		log.Printf("web: build page: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	p.Dev = s.hub != nil
	var buf bytes.Buffer
	if err := pages.ExecuteTemplate(&buf, name, p); err != nil {
		log.Printf("web: render %s: %v", name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}

func (s *server) stats(w http.ResponseWriter, _ *http.Request) {
	body, err := json.Marshal(s.feeds.Feed())
	if err != nil {
		log.Printf("web: encode feed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "max-age=15")
	w.Write(body)
}

// healthz reports the process only; it never calls upstream.
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, "ok")
}

func longCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=604800")
		h.ServeHTTP(w, r)
	})
}
