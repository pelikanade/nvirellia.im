package feed

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func mustLocation(t *testing.T) *time.Location {
	t.Helper()
	loc, err := Location()
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

type fakeTokens struct {
	buckets []Bucket
	err     error
	calls   int
}

func (f *fakeTokens) Usage(context.Context, time.Time) ([]Bucket, error) {
	f.calls++
	return f.buckets, f.err
}

func ptrValue(p *int64) int64 {
	if p == nil {
		return -1
	}
	return *p
}

func TestTodayAndHourlyFollowUTC8Midnight(t *testing.T) {
	loc := mustLocation(t)
	src := &fakeTokens{buckets: []Bucket{
		{Start: mustTime(t, "2026-10-09T15:00:00Z"), Input: 100, Output: 10}, // 23:00 Oct 9, UTC+8
		{Start: mustTime(t, "2026-10-09T16:00:00Z"), Input: 200, Output: 20}, // 00:00 Oct 10, UTC+8
	}}
	feedAt := func(ts string) Feed {
		now := mustTime(t, ts)
		return New(Config{Location: loc, TTL: time.Minute, Now: func() time.Time { return now }}, src).Feed()
	}

	before := feedAt("2026-10-09T15:59:59Z")
	if before.Tokens.Today != (Pair{Input: 100, Output: 10}) {
		t.Errorf("15:59:59Z today = %+v, want Oct 9 total", before.Tokens.Today)
	}
	if before.Tokens.Hourly.Start != "2026-10-09T00:00:00+08:00" {
		t.Errorf("15:59:59Z hourly start = %s", before.Tokens.Hourly.Start)
	}
	if got := before.Tokens.Hourly.Input[23]; got != 100 {
		t.Errorf("15:59:59Z last hour (23:00) input = %d, want 100", got)
	}
	if got := ptrValue(before.Tokens.Daily.Input[110]); got != -1 {
		t.Errorf("15:59:59Z Oct 10 daily cell should be future (null), got %d", got)
	}

	after := feedAt("2026-10-09T16:00:00Z")
	if after.Tokens.Today != (Pair{Input: 200, Output: 20}) {
		t.Errorf("16:00:00Z today = %+v, want Oct 10 total", after.Tokens.Today)
	}
	if after.Tokens.Hourly.Start != "2026-10-09T01:00:00+08:00" {
		t.Errorf("16:00:00Z hourly start = %s", after.Tokens.Hourly.Start)
	}
	if got := after.Tokens.Hourly.Input[22]; got != 100 {
		t.Errorf("16:00:00Z hour 23:00 Oct 9 input = %d, want 100", got)
	}
	if got := after.Tokens.Hourly.Input[23]; got != 200 {
		t.Errorf("16:00:00Z current hour (00:00 Oct 10) input = %d, want 200", got)
	}
	if after.Tokens.Daily.Start != "2026-06-22" {
		t.Errorf("daily start = %s, want the Monday 15 weeks before Oct 5", after.Tokens.Daily.Start)
	}
	if got := ptrValue(after.Tokens.Daily.Input[110]); got != 200 {
		t.Errorf("Oct 10 daily input = %d, want 200", got)
	}
	if got := ptrValue(after.Tokens.Daily.Input[111]); got != -1 {
		t.Errorf("Oct 11 daily cell should be future (null), got %d", got)
	}
}

func TestUnconfiguredUpstreamsServeSample(t *testing.T) {
	loc := mustLocation(t)
	now := mustTime(t, "2026-10-10T17:00:00+08:00")
	f := New(Config{Location: loc, TTL: time.Minute, Now: func() time.Time { return now }}, nil).Feed()

	if !f.Sample {
		t.Error("unconfigured upstreams must report sample: true")
	}
	if len(f.Tokens.Daily.Input) != dailyDays || len(f.Tokens.Hourly.Input) != hourlyWindow {
		t.Errorf("window sizes daily=%d hourly=%d", len(f.Tokens.Daily.Input), len(f.Tokens.Hourly.Input))
	}
	if f.Tokens.Daily.Input[110] == nil || f.Tokens.Daily.Input[111] != nil {
		t.Error("Saturday is index 110: it must be today (set) and Sunday must be future (null)")
	}
	if f.TZ != "Asia/Shanghai" || f.GeneratedAt != "2026-10-10T17:00:00+08:00" {
		t.Errorf("tz=%s generatedAt=%s", f.TZ, f.GeneratedAt)
	}
}

func TestFallbackServesLastGoodThenSample(t *testing.T) {
	loc := mustLocation(t)
	clock := mustTime(t, "2026-10-10T12:00:00+08:00")
	src := &fakeTokens{err: errors.New("down")}
	svc := New(Config{Location: loc, TTL: 30 * time.Second, Now: func() time.Time { return clock }}, src)

	// Magpie is down from the start: the feed falls back to sample.
	if f := svc.Feed(); !f.Sample || f.Tokens.Today != (Pair{Input: sampleTodayInput, Output: sampleTodayOutput}) {
		t.Fatalf("no prior value: want sample tokens, got sample=%v today=%+v", f.Sample, f.Tokens.Today)
	}
	if src.calls != 1 {
		t.Fatalf("first failure should make one call, got %d", src.calls)
	}

	// Inside the retry backoff the upstream is not called again.
	clock = clock.Add(10 * time.Second)
	svc.Feed()
	if src.calls != 1 {
		t.Fatalf("retry before TTL: calls = %d, want 1", src.calls)
	}

	// Magpie recovers.
	clock = clock.Add(30 * time.Second)
	src.err = nil
	src.buckets = []Bucket{{Start: mustTime(t, "2026-10-10T11:00:00+08:00"), Input: 5000, Output: 500}}
	f := svc.Feed()
	if f.Sample {
		t.Error("recovered magpie: sample must be false")
	}
	if f.Tokens.Today != (Pair{Input: 5000, Output: 500}) {
		t.Errorf("recovered today = %+v", f.Tokens.Today)
	}

	// Magpie fails again after TTL: last good (under 10 min) is served, and the feed is marked sample.
	clock = clock.Add(time.Minute)
	src.err = errors.New("down again")
	f = svc.Feed()
	if !f.Sample {
		t.Error("serving stale data during an outage must be marked sample")
	}
	if f.Tokens.Today != (Pair{Input: 5000, Output: 500}) {
		t.Errorf("stale window = %+v, want last good values", f.Tokens.Today)
	}

	// Past the 10 min staleness bound the sample blob takes over.
	clock = clock.Add(11 * time.Minute)
	if f = svc.Feed(); f.Tokens.Today != (Pair{Input: sampleTodayInput, Output: sampleTodayOutput}) {
		t.Errorf("stale past 10 min: today = %+v, want sample", f.Tokens.Today)
	}
}

func TestHTTPUpstreamsFallbackAndRecover(t *testing.T) {
	loc := mustLocation(t)
	now := mustTime(t, "2026-10-10T12:00:00+08:00")
	const token = "s3cret"

	magpie := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token || r.URL.Query().Get("since") == "" {
			http.Error(w, "unauthorised", http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"buckets": []Bucket{
			{Start: time.Date(2026, 10, 10, 3, 0, 0, 0, time.UTC), Input: 7000, Output: 700}, // 11:00 UTC+8
		}})
	}))
	defer magpie.Close()

	cfg := Config{Location: loc, TTL: time.Minute, Now: func() time.Time { return now }}
	up := New(cfg, NewMagpie(magpie.URL, token)).Feed()
	if up.Sample {
		t.Error("stub upstreams up: sample must be false")
	}
	if up.Tokens.Today != (Pair{Input: 7000, Output: 700}) {
		t.Errorf("live feed = today %+v", up.Tokens.Today)
	}

	// Magpie unreachable: sample, no panic, same shape.
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()
	down := New(cfg, NewMagpie(goneURL, token)).Feed()
	if !down.Sample {
		t.Errorf("unreachable magpie: sample=%v, want true", down.Sample)
	}
}

func TestClientErrorsDoNotLeakToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()
	_, err := NewMagpie(srv.URL, "s3cret").Usage(context.Background(), time.Now())
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("err = %v, want HTTP 403", err)
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Errorf("error leaks token: %v", err)
	}
}
