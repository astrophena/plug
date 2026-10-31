// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package purl implements the purl RPC shell and PlugScript evaluator.
package purl

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"go.astrophena.name/base/cli"

	"golang.org/x/term"
)

// Command runs the purl RPC shell. Its zero value is ready to use.
type Command struct {
	expr      string
	hasExpr   bool
	file      string
	json      bool
	noHistory bool
	timeout   time.Duration
	headers   http.Header
}

// Flags registers the command's flags.
func (c *Command) Flags(fs *flag.FlagSet) {
	fs.Func("e", "evaluate `expression` without a REPL", func(text string) error {
		c.expr, c.hasExpr = text, true
		return nil
	})
	fs.StringVar(&c.file, "f", "", "read PlugScript from `file` (- means stdin)")
	fs.BoolVar(&c.json, "json", false, "write complete results as JSON")
	fs.BoolVar(&c.noHistory, "no-history", false, "do not load or save REPL history")
	fs.DurationVar(&c.timeout, "timeout", 30*time.Second, "connection and per-expression `timeout` (0 disables)")
	fs.Func("H", "add an HTTP upgrade request `header` (repeatable)", func(text string) error {
		name, value, ok := strings.Cut(text, ":")
		if !ok || strings.TrimSpace(name) == "" || strings.ContainsAny(text, "\r\n") {
			return fmt.Errorf("expected Name: value")
		}
		if c.headers == nil {
			c.headers = make(http.Header)
		}
		c.headers.Add(strings.TrimSpace(name), strings.TrimSpace(value))
		return nil
	})
}

// Run evaluates input against the endpoint. Script errors stop execution;
// REPL errors leave the session open.
func (c *Command) Run(ctx context.Context) (err error) {
	defer func() {
		if err != nil {
			err = &safeError{
				err: err,
			}
		}
	}()

	env := cli.GetEnv(ctx)
	if len(env.Args) != 1 || c.hasExpr && c.file != "" || c.timeout < 0 {
		return fmt.Errorf("%w: usage: purl [-e EXPRESSION | -f FILE] [FLAGS] ENDPOINT", cli.ErrInvalidArgs)
	}

	setup, cancel := c.context(ctx)
	s, err := connect(setup, env.Args[0], c.headers)
	cancel()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, s.client.Close()) }()

	input := env.Stdin
	if c.hasExpr {
		input = strings.NewReader(c.expr)
	}
	if c.file != "" && c.file != "-" {
		file, err := os.Open(c.file)
		if err != nil {
			return err
		}
		defer file.Close()
		input = file
	}

	in, inOK := env.Stdin.(*os.File)
	out, outOK := env.Stdout.(*os.File)
	if !c.hasExpr && c.file == "" && inOK && outOK && term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd())) {
		return c.repl(ctx, s, in, out, env)
	}
	return c.script(ctx, s, input, env.Stdout)
}

func (c *Command) context(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.timeout == 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, c.timeout)
}

func (c *Command) script(ctx context.Context, s *session, r io.Reader, w io.Writer) error {
	const maxSource = 4 << 20
	src, err := io.ReadAll(io.LimitReader(r, maxSource+1))
	if err != nil {
		return err
	}
	if len(src) > maxSource {
		return fmt.Errorf("script exceeds %d bytes", maxSource)
	}

	stmts, err := parse(string(src))
	if err != nil {
		return err
	}
	for _, st := range stmts {
		if err := ctx.Err(); err != nil {
			return err
		}
		call, cancel := c.context(ctx)
		v, display, err := s.execute(call, st)
		cancel()
		if err != nil {
			return err
		}
		if !display {
			continue
		}
		if err := c.print(w, v); err != nil {
			return err
		}
	}
	return nil
}

func (c *Command) print(w io.Writer, v value) error {
	var text string
	if c.json {
		var err error
		text, err = marshalJSON(v)
		if err != nil {
			return err
		}
	} else {
		text = format(v, 0)
	}

	_, err := fmt.Fprintln(w, text)
	return err
}
