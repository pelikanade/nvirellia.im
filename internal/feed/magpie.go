package feed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// maxBody caps upstream response bodies.
const maxBody = 8 << 20

// Magpie reads token usage from magpie's web API. Its routes need a session:
// GET <base>/?k=<key> answers 303 with the session cookie, then
//
//	GET <base>/api/usage?period=today  -> hourly buckets for today (local)
//	GET <base>/api/usage?period=all    -> daily buckets from magpie's first usage record
//
// An unknown period silently falls back to 30 days, so every series is checked
// against the period and bucket it was asked for.
type Magpie struct {
	BaseURL string
	Key     string
	Client  *http.Client
}

// NewMagpie returns a client that keeps magpie's session cookie in its own jar.
func NewMagpie(baseURL, key string) (*Magpie, error) {
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("MAGPIE_URL: %w", err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &Magpie{BaseURL: baseURL, Key: key, Client: &http.Client{Jar: jar}}, nil
}

// Usage implements TokenSource.
func (m *Magpie) Usage(ctx context.Context) (Usage, error) {
	if err := m.login(ctx); err != nil {
		return Usage{}, err
	}
	hours, err := m.usage(ctx, "today", "hour")
	if err != nil {
		return Usage{}, err
	}
	days, err := m.usage(ctx, "all", "day")
	if err != nil {
		return Usage{}, err
	}
	return Usage{Hours: hours, Days: days}, nil
}

// login exchanges the key for the session cookie. The key travels only in this
// request's query, and errors never carry the URL.
func (m *Magpie) login(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.endpoint("/", url.Values{"k": {m.Key}}), nil)
	if err != nil {
		return fmt.Errorf("magpie: %w", errorWithoutURL(err))
	}
	noFollow := *m.Client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noFollow.Do(req)
	if err != nil {
		return fmt.Errorf("magpie: login request failed: %w", errorWithoutURL(err))
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
	if resp.StatusCode != http.StatusSeeOther {
		return fmt.Errorf("magpie: login HTTP %d", resp.StatusCode)
	}
	return nil
}

// wireUsage is the part of magpie's /api/usage body the feed reads.
type wireUsage struct {
	Period string `json:"period"`
	Bucket string `json:"bucket"`
	Series []struct {
		Time   time.Time `json:"time"`
		Input  int64     `json:"input"`
		Output int64     `json:"output"`
	} `json:"series"`
}

// usage fetches one period and rejects it unless magpie served that period at that grain.
func (m *Magpie) usage(ctx context.Context, period, bucket string) ([]Bucket, error) {
	var body wireUsage
	if err := getJSON(ctx, m.Client, m.endpoint("/api/usage", url.Values{"period": {period}}), "", "magpie", &body); err != nil {
		return nil, err
	}
	if body.Period != period || body.Bucket != bucket {
		return nil, fmt.Errorf("magpie: period %s/%s came back as %s/%s", period, bucket, body.Period, body.Bucket)
	}
	out := make([]Bucket, len(body.Series))
	for i, s := range body.Series {
		out[i] = Bucket{Start: s.Time, Input: s.Input, Output: s.Output}
	}
	return out, nil
}

// endpoint joins a path and query onto the base URL.
func (m *Magpie) endpoint(path string, q url.Values) string {
	u := strings.TrimRight(m.BaseURL, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// getJSON GETs a URL with an optional bearer token and decodes the JSON body.
// Errors name the upstream and status only; the token and body never appear in them.
func getJSON(ctx context.Context, client *http.Client, target, token, name string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: request failed: %w", name, errorWithoutURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		return fmt.Errorf("%s: HTTP %d", name, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
		return fmt.Errorf("%s: bad JSON: %w", name, err)
	}
	return nil
}

// errorWithoutURL strips the *url.Error wrapper so request errors carry no target URL.
func errorWithoutURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
