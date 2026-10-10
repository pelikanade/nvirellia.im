package feed

import (
	"context"
	"time"
)

// TokenSource fetches hourly magpie usage covering at least since..now.
type TokenSource interface {
	Usage(ctx context.Context, since time.Time) ([]Bucket, error)
}

// AgentSource fetches the current paseo agent list.
type AgentSource interface {
	Agents(ctx context.Context) ([]Agent, error)
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
	tokens *cached[[]Bucket]
	agents *cached[[]Agent]
}

// New wires the service. A nil source means that upstream is unconfigured: its
// half of the feed is always sample data.
func New(cfg Config, tokens TokenSource, agents AgentSource) *Service {
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

	var fetchTokens func(context.Context) ([]Bucket, error)
	if tokens != nil {
		fetchTokens = func(ctx context.Context) ([]Bucket, error) {
			return tokens.Usage(ctx, windowStart(s.now(), s.loc))
		}
	}
	var fetchAgents func(context.Context) ([]Agent, error)
	if agents != nil {
		fetchAgents = agents.Agents
	}
	s.tokens = &cached[[]Bucket]{name: "magpie", fetch: fetchTokens, now: cfg.Now,
		ttl: cfg.TTL, timeout: cfg.Timeout, maxStale: cfg.MaxStale}
	s.agents = &cached[[]Agent]{name: "paseo", fetch: fetchAgents, now: cfg.Now,
		ttl: cfg.TTL, timeout: cfg.Timeout, maxStale: cfg.MaxStale}
	return s
}

// Feed returns the current snapshot. Sample is true while either upstream is
// unconfigured or unreachable (including when stale data is being served).
func (s *Service) Feed() Feed {
	now := s.now()
	buckets, haveTokens, freshTokens := s.tokens.get()
	tokens := sampleTokens(now, s.loc)
	if haveTokens {
		tokens = aggregate(buckets, now, s.loc)
	}

	agents, haveAgents, freshAgents := s.agents.get()
	if !haveAgents {
		agents = sampleAgents()
	}
	if agents == nil {
		agents = []Agent{}
	}

	return Feed{
		Sample:      !freshTokens || !freshAgents,
		GeneratedAt: now.In(s.loc).Format(time.RFC3339),
		TZ:          s.loc.String(),
		Tokens:      tokens,
		Agents:      agents,
	}
}
