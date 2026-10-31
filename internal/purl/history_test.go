// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.astrophena.name/base/filelock"
)

func TestHistory(t *testing.T) {
	state := t.TempDir()
	getenv := func(name string) string {
		if name == "XDG_STATE_HOME" {
			return state
		}
		return ""
	}
	h, err := openHistory(getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer h.f.Close()
	for _, entry := range []string{"one", " secret", "", "[\n1, 2\n]", "last"} {
		if err := h.save(t.Context(), entry); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := h.load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"one", "[\n1, 2\n]", "last"}; !slices.Equal(entries, want) {
		t.Fatalf("got %q, want %q", entries, want)
	}
	for path, mode := range map[string]os.FileMode{
		filepath.Join(state, "purl"):            0700,
		filepath.Join(state, "purl", "history"): 0600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != mode {
			t.Fatalf("%s: got %o, want %o", path, got, mode)
		}
	}
}

func TestConcurrentHistory(t *testing.T) {
	state := t.TempDir()
	getenv := func(name string) string {
		if name == "XDG_STATE_HOME" {
			return state
		}
		return ""
	}
	var wg sync.WaitGroup
	for i := range 4 {
		h, err := openHistory(getenv)
		if err != nil {
			t.Fatal(err)
		}
		wg.Go(func() {
			defer h.f.Close()
			for j := range 10 {
				if err := h.save(t.Context(), fmt.Sprintf("%d:%d", i, j)); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
	h, err := openHistory(getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer h.f.Close()
	entries, err := h.load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 40 {
		t.Fatalf("lost concurrent entries: got %d, want 40", len(entries))
	}
}

func TestHistoryLock(t *testing.T) {
	state := t.TempDir()
	h, err := openHistory(func(name string) string {
		if name == "XDG_STATE_HOME" {
			return state
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.f.Close()

	lock, err := filelock.Acquire(h.f.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lock.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := h.load(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("load while locked: %v", err)
	}
	if err := h.save(ctx, "not saved"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("save while locked: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}

	if err := h.save(t.Context(), "saved"); err != nil {
		t.Fatal(err)
	}
	entries, err := h.load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(entries, []string{"saved"}) {
		t.Fatalf("got %q, want [saved]", entries)
	}
}

func TestHistoryBoundsAndFallback(t *testing.T) {
	home := t.TempDir()
	getenv := func(name string) string {
		switch name {
		case "HOME":
			return home
		case "XDG_STATE_HOME":
			return "relative/path"
		}
		return ""
	}
	h, err := openHistory(getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer h.f.Close()
	if want := filepath.Join(home, ".local", "state", "purl", "history"); h.f.Name() != want {
		t.Fatalf("got %q, want %q", h.f.Name(), want)
	}
	if err := h.save(t.Context(), strings.Repeat("x", 17<<10)); err != nil {
		t.Fatal(err)
	}
	for i := range historyLimit + 5 {
		if err := h.save(t.Context(), fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := h.load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != historyLimit || entries[0] != "5" {
		t.Fatalf("bad history bounds: %d entries, first %q", len(entries), entries[0])
	}
}
