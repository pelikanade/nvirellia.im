package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	site "github.com/pelikanade/nvirellia.im"
	"github.com/pelikanade/nvirellia.im/internal/feed"
)

type countingTokens struct {
	buckets []feed.Bucket
	err     error
	calls   int
}

func (c *countingTokens) Usage(context.Context, time.Time) ([]feed.Bucket, error) {
	c.calls++
	return c.buckets, c.err
}

func newHandler(t *testing.T, tokens feed.TokenSource, now time.Time) http.Handler {
	t.Helper()
	loc, err := feed.Location()
	if err != nil {
		t.Fatal(err)
	}
	svc := feed.New(feed.Config{Location: loc, TTL: time.Minute, Now: func() time.Time { return now }}, tokens)
	h, err := NewHandler(svc, site.Assets)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func get(t *testing.T, h http.Handler, path string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	body, _ := io.ReadAll(rec.Body)
	return rec, string(body)
}

var noon = time.Date(2026, 10, 10, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))

func TestSheetRendersProfileResourcesAndQuitDialog(t *testing.T) {
	rec, body := get(t, newHandler(t, nil, noon), "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, want := range []string{
		`id="profile"`, `id="resources"`, // both panels
		`id="quit-dlg"`,     // Quit joke dialog
		"i@nvirellia.im",    // email in Contact
		"sample data",       // sample badge while magpie is unconfigured
		`href="#resources"`, // no-JS tab strip
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(body, "is-live") {
		t.Error("sample feed must not carry the is-live state")
	}
}

func TestLiveFeedSetsIsLive(t *testing.T) {
	tokens := &countingTokens{buckets: []feed.Bucket{{Start: noon.Add(-time.Hour), Input: 900, Output: 90}}}
	_, body := get(t, newHandler(t, tokens, noon), "/")
	if !strings.Contains(body, "is-live") {
		t.Error("live feed must set is-live on <html>")
	}
}

var embeddedFeed = regexp.MustCompile(`(?s)<script type="application/json" id="cl-stats">(.*?)</script>`)

// The monitor reads the embedded feed; it must decode to the same feed the API serves.
func TestEmbeddedFeedMatchesAPI(t *testing.T) {
	h := newHandler(t, nil, noon)
	_, page := get(t, h, "/")
	m := embeddedFeed.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("no cl-stats block in page")
	}
	var fromPage feed.Feed
	if err := json.Unmarshal([]byte(m[1]), &fromPage); err != nil {
		t.Fatalf("embedded feed is not valid JSON: %v", err)
	}
	_, api := get(t, h, "/api/stats.json")
	var fromAPI feed.Feed
	if err := json.Unmarshal([]byte(api), &fromAPI); err != nil {
		t.Fatal(err)
	}
	if fromPage.Sample != fromAPI.Sample || fromPage.Tokens.Today != fromAPI.Tokens.Today {
		t.Errorf("embedded feed diverges from API:\n page=%+v\n api=%+v", fromPage, fromAPI)
	}
}

func TestStatsJSONMatchesSchema(t *testing.T) {
	rec, body := get(t, newHandler(t, nil, noon), "/api/stats.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "max-age=15" {
		t.Errorf("Cache-Control = %q", got)
	}
	var f map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &f); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"sample", "generatedAt", "tz", "tokens"} {
		if _, ok := f[k]; !ok {
			t.Errorf("missing top-level key %q", k)
		}
	}
	if _, ok := f["agents"]; ok {
		t.Error("agents key must be gone")
	}
	var typed feed.Feed
	json.Unmarshal([]byte(body), &typed)
	if !strings.HasSuffix(typed.GeneratedAt, "+08:00") || typed.TZ != "Asia/Shanghai" {
		t.Errorf("generatedAt=%s tz=%s", typed.GeneratedAt, typed.TZ)
	}
	if len(typed.Tokens.Hourly.Input) != 24 || len(typed.Tokens.Daily.Input) != 112 {
		t.Errorf("window sizes hourly=%d daily=%d", len(typed.Tokens.Hourly.Input), len(typed.Tokens.Daily.Input))
	}
}

func TestHealthzNeverCallsUpstream(t *testing.T) {
	tokens := &countingTokens{err: io.ErrUnexpectedEOF}
	rec, body := get(t, newHandler(t, tokens, noon), "/healthz")
	if rec.Code != http.StatusOK || body != "ok" {
		t.Errorf("healthz = %d %q", rec.Code, body)
	}
	if tokens.calls != 0 {
		t.Errorf("healthz called magpie %d times", tokens.calls)
	}
}
