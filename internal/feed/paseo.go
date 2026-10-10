package feed

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// Paseo reads the agent list from paseo. Wire format is assumed (to confirm with Noa):
//
//	GET <PASEO_URL>
//	Authorization: Bearer <PASEO_TOKEN>
//	[{"name": "Designy", "status": "running", "uptimeSec": 8040, "task": "..."}]
type Paseo struct {
	URL    string
	Token  string
	Client *http.Client
}

// NewPaseo returns a paseo client with a default HTTP client.
func NewPaseo(rawURL, token string) *Paseo {
	return &Paseo{URL: rawURL, Token: token, Client: http.DefaultClient}
}

// Agents implements AgentSource.
func (p *Paseo) Agents(ctx context.Context) ([]Agent, error) {
	var agents []Agent
	if err := getJSON(ctx, p.Client, p.URL, p.Token, "paseo", &agents); err != nil {
		return nil, err
	}
	return agents, nil
}

// errorWithoutURL strips the *url.Error wrapper so request errors carry no target URL.
func errorWithoutURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
