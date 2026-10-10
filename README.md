# nvirellia.im

Profile sheet for nvirellia.im: a Go program that serves the Clearlooks profile
(Profile | Resources) and a live feed at `/api/stats.json`.

Stdlib only. Templates and static assets are embedded.

## Run

    go run ./cmd/nvirellia

## Dev server

    go run ./cmd/nvirellia dev

Run from the repo root. `dev` rebuilds and restarts the server when a `.go` file or
`go.mod` changes. Templates and static files are read from disk on every request, and
open pages reload when they change. Production (`go run ./cmd/nvirellia`) serves the
embedded copies and sends no reload script.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | listen address |
| `MAGPIE_URL` | unset | magpie web base URL, e.g. `http://<host>:3430` (unset → no Resources panel) |
| `MAGPIE_WEB_KEY` | unset | magpie web key, required with `MAGPIE_URL` |
| `FEED_TTL` | `30s` | in-memory cache lifetime for upstream fetches |

Variables are read from the environment. A `.env` file in the working directory fills in
any that are not already set (git-ignored; keep the key there, not in the repo).
Magpie falls back when unreachable: the last good value is served if under 10 minutes
old; otherwise no tokens are served and the Resources panel is hidden.

## Routes

- `GET /`: the sheet, server-rendered from the current feed
- `GET /api/stats.json`: the feed, `Cache-Control: max-age=15`
- `GET /healthz`: `ok`, never calls upstream
- `GET /static/*`: embedded assets

## Test

Tests are end-to-end only. `e2e/` builds the binary, runs it against a stub magpie, and
checks the HTTP responses. There are no unit tests.

    go vet ./...
    go test ./e2e/

## Upstream items to confirm with Noa

- magpie: web API (`/api/usage`, login with the web key) is assumed, not yet checked against a live magpie; confirm the endpoint and response shape before relying on live numbers
- Hosting target and domain wiring
