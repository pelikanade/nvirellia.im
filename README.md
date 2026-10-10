# nvirellia.im

Profile sheet for nvirellia.im: a Go program that serves the Clearlooks profile
(Profile | Resources | Agents) and a live feed at `/api/stats.json`.

Stdlib only. Templates and static assets are embedded.

## Run

    go run ./cmd/nvirellia

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | listen address |
| `MAGPIE_URL` | unset | magpie token-usage endpoint (unset → sample data) |
| `MAGPIE_TOKEN` | unset | bearer token for magpie; sent only from the server |
| `PASEO_URL` | unset | paseo agent-list endpoint (unset → sample data) |
| `PASEO_TOKEN` | unset | bearer token for paseo; sent only from the server |
| `FEED_TTL` | `30s` | in-memory cache lifetime for upstream fetches |

Each upstream falls back on its own. Last good value is served if under 10 minutes
old; otherwise the sample blob is served with `"sample": true`.

## Routes

- `GET /`: the sheet, server-rendered from the current feed
- `GET /api/stats.json`: the feed, `Cache-Control: max-age=15`
- `GET /healthz`: `ok`, never calls upstream
- `GET /static/*`: embedded assets

## Test

    go vet ./...
    go test ./...

## Upstream items to confirm with Noa

- magpie: endpoint path, response shape, auth scheme (currently assumed; see `internal/feed/magpie.go`)
- paseo: endpoint path, response shape, auth scheme (currently assumed; see `internal/feed/paseo.go`)
- Hosting target and domain wiring
