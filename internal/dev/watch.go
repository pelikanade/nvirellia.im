// Package dev is the development harness: a supervisor that rebuilds and
// restarts the server when Go sources change, and a hub that tells open pages
// to reload when templates or static files change.
package dev

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

type stamp struct {
	mod  time.Time
	size int64
}

// Watch polls the files under root/dirs whose root-relative path satisfies keep,
// and calls onChange after every poll that finds a change. Changes include
// additions, deletions, and edits.
func Watch(ctx context.Context, every time.Duration, root string, dirs []string, keep func(rel string) bool, onChange func()) {
	prev := snapshot(root, dirs, keep)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cur := snapshot(root, dirs, keep)
			if changed(prev, cur) {
				prev = cur
				onChange()
			}
		}
	}
}

// snapshot records mtime and size for each matching file. Hidden directories
// (.git, .devenv) are skipped.
func snapshot(root string, dirs []string, keep func(rel string) bool) map[string]stamp {
	out := map[string]stamp{}
	for _, d := range dirs {
		start := filepath.Join(root, d)
		filepath.WalkDir(start, func(p string, de fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if de.IsDir() {
				if p != start && strings.HasPrefix(de.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil || !keep(rel) {
				return nil
			}
			info, err := de.Info()
			if err != nil {
				return nil
			}
			out[rel] = stamp{mod: info.ModTime(), size: info.Size()}
			return nil
		})
	}
	return out
}

func changed(a, b map[string]stamp) bool {
	if len(a) != len(b) {
		return true
	}
	for k, v := range a {
		w, ok := b[k]
		if !ok || !w.mod.Equal(v.mod) || w.size != v.size {
			return true
		}
	}
	return false
}
