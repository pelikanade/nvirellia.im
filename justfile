# List recipes
default:
    @just --list

# Run the server (reads .env; LISTEN_ADDR defaults to :8080)
run:
    go run ./cmd/nvirellia

# Restart the server on any change in the repo (embedded templates and static need a restart too)
dev:
    watchexec --restart --clear --ignore '.devenv/**' -- go run ./cmd/nvirellia
