package feed

import (
	"context"
	"time"
)

// TokenSource fetches magpie's token usage.
type TokenSource interface {
	Usage(ctx context.Context) (Usage, error)
}

// Config holds the feed's timing. Zero values take the defaults noted below.
type Config struct {
	Location *time.Location // default: UTC+8
	TTL      time.Duration  // how long a fetched value is fresh (FEED_TTL); required
	Timeout  time.Duration  // per-upstream request timeout; default 3s
	MaxStale time.Duration  // oldest last-good value served on error; default 10m
	Now      func() time.Time
}

// Service builds Feed snapshots. It is safe for concurrent use.
type Service struct {
	loc    *time.Location
	now    func() time.Time
	tokens *cached[Usage]
}

// New wires the service. A nil source means magpie is unconfigured, so Tokens is nil.
func New(cfg Config, tokens TokenSource) *Service {
	if cfg.Location == nil {
		cfg.Location = time.UTC
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 3 * time.Second
	}
	if cfg.MaxStale == 0 {
		cfg.MaxStale = 10 * time.Minute
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	s := &Service{loc: cfg.Location, now: cfg.Now}

	var fetchTokens func(context.Context) (Usage, error)
	if tokens != nil {
		fetchTokens = tokens.Usage
	}
	s.tokens = &cached[Usage]{name: "magpie", fetch: fetchTokens, now: cfg.Now,
		ttl: cfg.TTL, timeout: cfg.Timeout, maxStale: cfg.MaxStale}
	return s
}

// Feed returns the current snapshot. Tokens is nil while magpie is unconfigured,
// unreachable with no last-good value within MaxStale, or has returned no buckets.
func (s *Service) Feed() Feed {
	now := s.now()
	f := Feed{
		GeneratedAt: now.In(s.loc).Format(time.RFC3339),
		TZ:          s.loc.String(),
	}
	usage, have, _ := s.tokens.get()
	if have && (len(usage.Hours) > 0 || len(usage.Days) > 0) {
		t := aggregate(usage, now, s.loc)
		f.Tokens = &t
	}
	return f
}
