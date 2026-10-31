// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/chzyer/readline"
	"github.com/google/shlex"
	"golang.org/x/term"
)

// ttyInput can stop a pending editor read before giving the terminal to a pager.
// The mutex stays locked until the pager exits.
type ttyInput struct {
	f  *os.File
	mu sync.Mutex
}

func (t *ttyInput) Read(p []byte) (int, error) {
	for {
		t.mu.Lock()
		n, err := t.f.Read(p)
		t.mu.Unlock()
		if n != 0 || !errors.Is(err, os.ErrDeadlineExceeded) {
			return n, err
		}
	}
}

func (t *ttyInput) pause() error {
	if err := t.f.SetReadDeadline(time.Now()); err != nil {
		return err
	}
	t.mu.Lock()
	if err := t.f.SetReadDeadline(time.Time{}); err != nil {
		t.mu.Unlock()
		return err
	}
	return nil
}

func (t *ttyInput) resume() { t.mu.Unlock() }

type display struct {
	in     *os.File
	out    *os.File
	stderr io.Writer
	input  *ttyInput
	state  *readline.State
	pager  []string
	color  bool
}

func pagerArgs(getenv func(string) string) []string {
	if getenv("NO_PAGER") != "" || getenv("TERM") == "dumb" {
		return nil
	}
	if text := getenv("PAGER"); text != "" {
		args, err := shlex.Split(text)
		if err != nil || len(args) == 0 {
			return nil
		}
		if _, err := exec.LookPath(args[0]); err != nil {
			return nil
		}
		return args
	}
	if path, err := exec.LookPath("less"); err == nil {
		return []string{path, "-FRX"}
	}
	return nil
}

func (d *display) value(ctx context.Context, v value) error {
	now := time.Now()
	width, _, err := term.GetSize(int(d.out.Fd()))
	if err != nil {
		width = 80
	}
	short, tabular := recordTable(v, width, 16<<10, now)
	if !tabular {
		short = formatAt(v, 16<<10, now) + "\n"
	}
	text := short
	if len(d.pager) != 0 {
		if tabular {
			text, _ = recordTable(v, width, 0, now)
		} else {
			text = formatAt(v, 0, now) + "\n"
		}
	}
	paint := func(text string) string { return highlightValue(v, text) }
	if tabular {
		paint = highlightTable
	}
	return d.show(ctx, text, short, paint)
}

// show takes plain text so screen sizing does not count color escapes.
func (d *display) show(ctx context.Context, text, short string, paint func(string) string) error {
	if len(d.pager) != 0 {
		width, height, err := term.GetSize(int(d.out.Fd()))
		if err == nil && overflows(text, width, height) {
			if d.color {
				text = paint(text)
			}
			used, err := d.page(ctx, text)
			if used || err != nil {
				return err
			}
		}
	}
	if d.color {
		short = paint(short)
	}
	_, err := fmt.Fprint(d.out, short)
	return err
}

func (d *display) page(ctx context.Context, text string) (used bool, err error) {
	if err := d.input.pause(); err != nil {
		return false, nil
	}
	defer d.input.resume()

	if err := readline.Restore(int(d.in.Fd()), d.state); err != nil {
		return false, err
	}
	defer func() {
		_, rawErr := readline.MakeRaw(int(d.in.Fd()))
		err = errors.Join(err, rawErr)
	}()

	cmd := exec.CommandContext(ctx, d.pager[0], d.pager[1:]...)
	cmd.Stdin = strings.NewReader(text)
	cmd.Stdout = d.out
	cmd.Stderr = d.stderr
	if err := cmd.Start(); err != nil {
		// An unavailable pager must not hide the result.
		return false, ctx.Err()
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return true, ctx.Err()
		}
		return true, fmt.Errorf("pager: %w", err)
	}
	return true, nil
}

func overflows(text string, width, height int) bool {
	if width <= 0 || height <= 1 {
		return false
	}
	var col int
	rows := 1
	for _, r := range text {
		if r == '\n' {
			rows++
			col = 0
		} else {
			n := (readline.Runes{}).Width(r)
			if r == '\t' {
				n = 8 - col%8
			}
			if col+n > width {
				rows++
				col = 0
			}
			col += n
		}
		if rows >= height {
			return true
		}
	}
	return false
}
