package feed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// maxBody caps upstream response bodies.
const maxBody = 8 << 20

// Magpie reads token usage from magpie. Wire format is assumed (to confirm with Noa):
//
//	GET <MAGPIE_URL>?since=<RFC 3339 UTC>
//	Authorization: Bearer <MAGPIE_TOKEN>
//	{"buckets": [{"start": "2026-10-09T15:00:00Z", "input": 123, "output": 45}]}
type Magpie struct {
	URL    string
	Token  string
	Client *http.Client
}

// NewMagpie returns a magpie client with a default HTTP client.
func NewMagpie(rawURL, token string) *Magpie {
	return &Magpie{URL: rawURL, Token: token, Client: http.DefaultClient}
}

// Usage implements TokenSource.
func (m *Magpie) Usage(ctx context.Context, since time.Time) ([]Bucket, error) {
	u, err := url.Parse(m.URL)
	if err != nil {
		return nil, fmt.Errorf("magpie: bad URL: %w", err)
	}
	q := u.Query()
	q.Set("since", since.UTC().Format(time.RFC3339))
	u.RawQuery = q.Encode()

	var body struct {
		Buckets []Bucket `json:"buckets"`
	}
	if err := getJSON(ctx, m.Client, u.String(), m.Token, "magpie", &body); err != nil {
		return nil, err
	}
	return body.Buckets, nil
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
