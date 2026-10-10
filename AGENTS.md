# AGENTS.md

## Dev server

- While iterating, run the dev server in the background, from the repo root, as a named service (`nvdev`):

      LISTEN_ADDR=127.0.0.1:8080 go run ./cmd/nvirellia dev

- Before starting, check `curl -s http://127.0.0.1:8080/healthz`. If it prints `ok`, reuse that server. NEVER start a second one on the same port.
- Template and static edits need no restart: the running server reads them from disk and reloads open pages. Go edits rebuild and restart automatically. A broken Go edit keeps the old server running; read the `dev:` log lines for the build error.
- Restart it only if the supervisor has exited (its log says so) or after changing `LISTEN_ADDR` or other env.
- Stop it when the work is done.

## Tern browser

- When the dev server starts from a Tern terminal, open `http://127.0.0.1:8080/` in a Tern browser side pane (`browser.open`, name `nvirellia-dev`, viewport 1280x800), so the sheet is visible next to the editor.
- If a tab named `nvirellia-dev` is already open, reuse it. NEVER open a second tab for the same URL.
