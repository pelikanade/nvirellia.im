// Command nvirellia serves nvirellia.im.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	site "github.com/pelikanade/nvirellia.im"
	"github.com/pelikanade/nvirellia.im/internal/feed"
	"github.com/pelikanade/nvirellia.im/internal/web"
)

type config struct {
	ListenAddr string
	FeedTTL    time.Duration
	MagpieURL  string
	MagpieKey  string
}

// loadConfig reads the environment. An unset MAGPIE_URL leaves magpie unconfigured,
// so the sheet renders without the Resources panel.
func loadConfig(getenv func(string) string) (config, error) {
	c := config{
		ListenAddr: orDefault(getenv("LISTEN_ADDR"), ":8080"),
		MagpieURL:  getenv("MAGPIE_URL"),
		MagpieKey:  getenv("MAGPIE_WEB_KEY"),
	}
	ttl, err := time.ParseDuration(orDefault(getenv("FEED_TTL"), "30s"))
	if err != nil {
		return config{}, fmt.Errorf("FEED_TTL: %w", err)
	}
	c.FeedTTL = ttl
	if c.MagpieURL != "" && c.MagpieKey == "" {
		return config{}, errors.New("MAGPIE_WEB_KEY is required when MAGPIE_URL is set")
	}
	return c, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func main() {
	if err := loadDotEnv(".env"); err != nil {
		log.Fatal(err)
	}
	err := run()
	if err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	loc, err := feed.Location()
	if err != nil {
		return fmt.Errorf("time zone: %w", err)
	}

	var tokens feed.TokenSource
	if cfg.MagpieURL != "" {
		magpie, err := feed.NewMagpie(cfg.MagpieURL, cfg.MagpieKey)
		if err != nil {
			return err
		}
		tokens = magpie
	}
	svc := feed.New(feed.Config{Location: loc, TTL: cfg.FeedTTL}, tokens)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	handler, err := web.NewHandler(svc, site.Assets)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", cfg.ListenAddr)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	log.Print("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
