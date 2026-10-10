package feed

import (
	"context"
	"time"
)

// TokenSource fetches magpie's token usage.
type TokenSource interface {
	Usage(ctx context.Context) (Usage, error)
}

// Config holds the feed's day zone and how long a fetched value stays fresh.
type Config struct {
	Location *time.Location // day zone for the feed; required
	TTL      time.Duration  // how long a fetched value is fresh (FEED_TTL); required
}

const (
	upstreamTimeout = 3 * time.Second  // per-upstream request timeout
	maxStale        = 10 * time.Minute // oldest last-good value served on error
)

// Service builds Feed snapshots. It is safe for concurrent use.
type Service struct {
	loc    *time.Location
	tokens *cached[Usage]
}

// New wires the service. cfg.Location must be set. A nil source means magpie is unconfigured, so Tokens is nil.
func New(cfg Config, tokens TokenSource) *Service {
	s := &Service{loc: cfg.Location}

	var fetchTokens func(context.Context) (Usage, error)
	if tokens != nil {
		fetchTokens = tokens.Usage
	}
	s.tokens = &cached[Usage]{name: "magpie", fetch: fetchTokens, now: time.Now,
		ttl: cfg.TTL, timeout: upstreamTimeout, maxStale: maxStale}
	return s
}

// Feed returns the current snapshot. Tokens is nil while magpie is unconfigured,
// unreachable with no last-good value within maxStale, or has returned no buckets.
func (s *Service) Feed() Feed {
	now := time.Now()
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
