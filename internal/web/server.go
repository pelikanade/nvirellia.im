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

	"github.com/pelikanade/nvirellia.im/internal/feed"
)

// Feeds supplies the current feed snapshot.
type Feeds interface {
	Feed() feed.Feed
}

type server struct {
	feeds Feeds
	pages *template.Template
}

// NewHandler routes the site. assets must contain templates/ and static/.
func NewHandler(feeds Feeds, assets fs.FS) (http.Handler, error) {
	pages, err := template.New("").ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	static, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}

	s := &server{feeds: feeds, pages: pages}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /api/stats.json", s.stats)
	mux.HandleFunc("GET /healthz", healthz)
	mux.Handle("GET /static/", http.StripPrefix("/static/", longCache(http.FileServerFS(static))))
	return mux, nil
}

func (s *server) index(w http.ResponseWriter, _ *http.Request) {
	p, err := newPage(s.feeds.Feed())
	if err != nil {
		log.Printf("web: build page: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := s.pages.ExecuteTemplate(&buf, "layout.html", p); err != nil {
		log.Printf("web: render page: %v", err)
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
