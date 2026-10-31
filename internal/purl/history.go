// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.astrophena.name/base/filelock"
)

const (
	historyLimit = 500
	historyBytes = 1 << 20
)

type history struct {
	f *os.File
}

func openHistory(getenv func(string) string) (*history, error) {
	state := getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(state) {
		home := getenv("HOME")
		if !filepath.IsAbs(home) {
			return nil, fmt.Errorf("history requires XDG_STATE_HOME or HOME")
		}
		state = filepath.Join(home, ".local", "state")
	}

	dir := filepath.Join(state, "purl")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(filepath.Join(dir, "history"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}
	return &history{
		f: f,
	}, nil
}

func (h *history) entries() ([]string, error) {
	if _, err := h.f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	sc := bufio.NewScanner(io.LimitReader(h.f, historyBytes+1))
	sc.Buffer(make([]byte, 4096), historyBytes+1)
	var entries []string
	for sc.Scan() {
		var entry string
		// Recover older entries after a partial write.
		if json.Unmarshal(sc.Bytes(), &entry) == nil && entry != "" {
			entries = append(entries, entry)
		}
	}
	return entries, sc.Err()
}

func (h *history) load(ctx context.Context) (entries []string, err error) {
	lock, err := h.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()

	return h.entries()
}

func (h *history) save(ctx context.Context, entry string) (err error) {
	if strings.TrimSpace(entry) == "" || strings.HasPrefix(entry, " ") || len(entry) > 16<<10 {
		return nil
	}
	lock, err := h.lock(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()

	entries, err := h.entries()
	if err != nil {
		return err
	}
	if len(entries) == 0 || entries[len(entries)-1] != entry {
		entries = append(entries, entry)
	}
	if len(entries) > historyLimit {
		entries = entries[len(entries)-historyLimit:]
	}

	var (
		encoded = make([][]byte, len(entries))
		size    int
	)
	for i, entry := range entries {
		data, _ := json.Marshal(entry)
		encoded[i] = append(data, '\n')
		size += len(encoded[i])
	}
	for size > historyBytes && len(encoded) > 0 {
		size -= len(encoded[0])
		encoded = encoded[1:]
	}

	if _, err := h.f.WriteAt(bytes.Join(encoded, nil), 0); err != nil {
		return err
	}
	return h.f.Truncate(int64(size))
}

func (h *history) lock(ctx context.Context) (*os.File, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f, err := filelock.Acquire(h.f.Name())
		if !errors.Is(err, filelock.ErrLocked) {
			return f, err
		}

		// Acquire does not wait. Let other sessions finish without spinning.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
