package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pelikanade/nvirellia.im/internal/dev"
)

// runDev is `nvirellia dev`. It rebuilds and restarts the server when Go sources
// change. Templates and static files need no restart: the running server reads
// them from this checkout and reloads open pages.
func runDev() error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "cmd", "nvirellia")); err != nil {
		return fmt.Errorf("run `nvirellia dev` from the repo root: %w", err)
	}
	bin := filepath.Join(os.TempDir(), "nvirellia-dev")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	restart := make(chan struct{}, 1)
	go dev.Watch(ctx, 500*time.Millisecond, root, []string{"."}, isGoSource, func() {
		select {
		case restart <- struct{}{}:
		default:
		}
	})

	if err := build(ctx, root, bin); err != nil {
		return fmt.Errorf("initial build: %w", err)
	}
	child, err := startServer(root, bin)
	if err != nil {
		return err
	}
	exited := child.done
	for {
		select {
		case <-ctx.Done():
			child.stop()
			return nil
		case <-exited:
			log.Print("dev: server exited; it restarts on the next Go change")
			exited = nil
		case <-restart:
			if err := build(ctx, root, bin); err != nil {
				log.Printf("dev: build failed; keeping the running server: %v", err)
				continue
			}
			child.stop()
			if child, err = startServer(root, bin); err != nil {
				return err
			}
			exited = child.done
			log.Print("dev: restarted server")
		}
	}
}

func isGoSource(rel string) bool {
	return strings.HasSuffix(rel, ".go") || rel == "go.mod"
}

// build writes to a sibling file and renames it into place, so a running server
// keeps its inode while the new binary is produced.
func build(ctx context.Context, root, bin string) error {
	next := bin + ".new"
	cmd := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", next, "./cmd/nvirellia")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	return os.Rename(next, bin)
}

type proc struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func startServer(root, bin string) (*proc, error) {
	cmd := exec.Command(bin)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), devRootEnv+"="+root)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &proc{cmd: cmd, done: make(chan struct{})}
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("dev: server: %v", err)
		}
		close(p.done)
	}()
	return p, nil
}

// stop asks the server to shut down and waits for the port to be released.
func (p *proc) stop() {
	_ = p.cmd.Process.Signal(os.Interrupt)
	select {
	case <-p.done:
	case <-time.After(15 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
}
