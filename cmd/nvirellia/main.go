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
	"github.com/pelikanade/nvirellia.im/internal/dev"
	"github.com/pelikanade/nvirellia.im/internal/feed"
	"github.com/pelikanade/nvirellia.im/internal/web"
)

type config struct {
	ListenAddr  string
	FeedTTL     time.Duration
	MagpieURL   string
	MagpieToken string
}

// loadConfig reads the environment. An unset MAGPIE_URL leaves magpie unconfigured,
// which serves sample data.
func loadConfig(getenv func(string) string) (config, error) {
	c := config{
		ListenAddr:  orDefault(getenv("LISTEN_ADDR"), ":8080"),
		MagpieURL:   getenv("MAGPIE_URL"),
		MagpieToken: getenv("MAGPIE_TOKEN"),
	}
	ttl, err := time.ParseDuration(orDefault(getenv("FEED_TTL"), "30s"))
	if err != nil {
		return config{}, fmt.Errorf("FEED_TTL: %w", err)
	}
	c.FeedTTL = ttl
	return c, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func main() {
	var err error
	if len(os.Args) > 1 && os.Args[1] == "dev" {
		err = runDev()
	} else {
		err = run()
	}
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
		tokens = feed.NewMagpie(cfg.MagpieURL, cfg.MagpieToken)
	}
	svc := feed.New(feed.Config{Location: loc, TTL: cfg.FeedTTL}, tokens)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	handler, err := siteHandler(ctx, svc)
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

// devRootEnv names the checkout a dev-mode server serves from. `nvirellia dev` sets it.
const devRootEnv = "NVIRELLIA_DEV_ROOT"

// siteHandler serves the embedded assets. With devRootEnv set it serves the checkout
// on disk and reloads open pages when templates or static files change.
func siteHandler(ctx context.Context, feeds web.Feeds) (http.Handler, error) {
	root := os.Getenv(devRootEnv)
	if root == "" {
		return web.NewHandler(feeds, site.Assets)
	}
	hub := dev.NewHub()
	go dev.Watch(ctx, 300*time.Millisecond, root, []string{"templates", "static"}, func(string) bool { return true }, hub.Notify)
	log.Printf("dev mode: serving %s with live reload", root)
	return web.NewDevHandler(feeds, os.DirFS(root), hub)
}
