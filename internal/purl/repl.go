// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"
	"unicode/utf8"

	"go.astrophena.name/base/cli"
	syntax "go.astrophena.name/plug/syntax/chroma"

	"github.com/chzyer/readline"
	"golang.org/x/term"
)

// mu separates editor callbacks from evaluation, which runs serially.
type repl struct {
	mu      sync.Mutex
	session *session
	prefix  string
	cancel  context.CancelFunc
	idle    chan struct{}
	op      *readline.Operation
}

type replResult struct {
	quit bool
	err  error
}

func (r *repl) Do(line []rune, pos int) ([][]rune, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		return nil, 0
	}

	candidates, prefix := r.session.complete(r.prefix + string(line[:pos]))
	out := make([][]rune, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, []rune(strings.TrimPrefix(candidate, prefix)))
	}
	return out, utf8.RuneCountInString(prefix)
}

func (r *repl) filter(key rune) (rune, bool) {
	r.mu.Lock()
	if r.cancel != nil {
		idle := r.idle
		if key == readline.CharInterrupt {
			r.cancel()
		}
		r.mu.Unlock()
		// Wait so an editor redraw cannot overwrite the result.
		<-idle
		return key, key != readline.CharInterrupt
	}
	// Editor changes can call Paint, which also reads REPL state.
	op := r.op
	r.mu.Unlock()
	if key == readline.CharInterrupt && op != nil {
		// Ctrl-C cancels input even when a menu is open.
		op.ExitCompleteMode(false)
		if op.IsSearchMode() {
			op.ExitSearchMode(false)
		}
	}
	return key, true
}

func (r *repl) finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
		close(r.idle)
	}
}

func (r *repl) Paint(line []rune, pos int) []rune {
	r.mu.Lock()
	prefix := clean(r.prefix)
	r.mu.Unlock()
	return []rune(colorize(prefix+clean(string(line)), syntax.Script, len(prefix)))
}

func (c *Command) repl(ctx context.Context, s *session, in, out *os.File, env *cli.Env) (err error) {
	// Reopen stdin so stopping the editor leaves the caller's file open.
	input, err := os.Open(in.Name())
	if err != nil {
		return err
	}
	defer input.Close()

	tty := &ttyInput{
		f: input,
	}
	stdin := readline.NewCancelableStdin(tty)
	defer stdin.Close()

	// Unlike term.MakeRaw, this preserves LF-to-CRLF output translation.
	state, err := readline.MakeRaw(int(in.Fd()))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, readline.Restore(int(in.Fd()), state)) }()

	r := &repl{
		session: s,
	}
	color := !c.json && env.Getenv("NO_COLOR") == "" && env.Getenv("TERM") != "dumb"
	cfg := &readline.Config{
		Prompt:                 "purl> ",
		EOFPrompt:              "\n", // Suppress readline's default ^D message.
		Stdin:                  stdin,
		Stdout:                 out,
		Stderr:                 env.Stderr,
		DisableAutoSaveHistory: true,
		HistoryLimit:           historyLimit,
		AutoComplete:           r,
		FuncFilterInputRune:    r.filter,
		// Stay raw during calls so Ctrl-C reaches the filter, not cli.Main.
		FuncMakeRaw:    func() error { return nil },
		FuncExitRaw:    func() error { return nil },
		FuncIsTerminal: func() bool { return true },
		FuncGetWidth: func() int {
			width, _, err := term.GetSize(int(out.Fd()))
			if err != nil {
				return 80
			}
			return width
		},
	}
	if color {
		cfg.Painter = r
	}
	editor, err := readline.NewEx(cfg)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.op = editor.Operation
	r.mu.Unlock()
	defer func() {
		// Release the reader's pump before closing the editor.
		closeErr := input.Close()
		stdin.Close()
		err = errors.Join(err, closeErr, editor.Close())
	}()
	defer r.finish()
	stop := context.AfterFunc(ctx, func() { stdin.Close() })
	defer stop()

	var h *history
	if !c.noHistory {
		h, err = openHistory(env.Getenv)
		if err != nil {
			return fmt.Errorf("open history (use -no-history to disable): %w", err)
		}
		defer func() { err = errors.Join(err, h.f.Close()) }()
		entries, err := h.load(ctx)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := editor.SaveHistory(entry); err != nil {
				return err
			}
		}
	}

	d := &display{
		in:     in,
		out:    out,
		stderr: env.Stderr,
		input:  tty,
		state:  state,
		color:  color,
	}
	if !c.json {
		d.pager = pagerArgs(env.Getenv)
		if _, err := fmt.Fprintf(out, intro, s.schema.Package, s.schema.Major); err != nil {
			return err
		}
	}

	evalCtx, stopEval := context.WithCancel(ctx)
	var pending <-chan replResult
	defer func() {
		stopEval()
		if pending != nil {
			<-pending
		}
	}()

	var source string
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := editor.Readline()
		if pending != nil {
			if err != nil && !errors.Is(err, readline.ErrInterrupt) {
				stopEval()
			}
			result := <-pending
			pending = nil
			if result.quit || result.err != nil {
				return result.err
			}
		}
		if errors.Is(err, readline.ErrInterrupt) {
			source = ""
			r.mu.Lock()
			r.prefix = ""
			r.mu.Unlock()
			editor.SetPrompt("purl> ")
			continue
		}
		if errors.Is(err, io.EOF) {
			return ctx.Err()
		}
		if err != nil {
			return err
		}

		source += line
		var stmts []stmt
		command := strings.HasPrefix(strings.TrimSpace(source), ":")
		if !command {
			stmts, err = parse(source)
			var syntax *syntaxError
			if errors.As(err, &syntax) && syntax.incomplete {
				source += "\n"
				r.mu.Lock()
				r.prefix = source
				r.mu.Unlock()
				editor.SetPrompt(" ...> ")
				continue
			}
		}

		if !c.noHistory && !strings.HasPrefix(source, " ") && strings.TrimSpace(source) != "" {
			if saveErr := editor.SaveHistory(source); saveErr != nil {
				return saveErr
			}
			if saveErr := h.save(ctx, source); saveErr != nil {
				return saveErr
			}
		}

		call, cancel := context.WithCancel(evalCtx)
		r.mu.Lock()
		r.cancel = cancel
		r.idle = make(chan struct{})
		r.prefix = ""
		r.mu.Unlock()
		editor.SetPrompt("")
		// Read the next line while evaluation runs. Readline then owns the
		// prompt redraw and input restart; queued keys wait in the filter.
		done := make(chan replResult, 1)
		pending = done
		go func(source string, err error) {
			var result replResult
			defer func() {
				if !result.quit && result.err == nil {
					editor.SetPrompt("purl> ")
					editor.Operation.SetBuffer("")
				}
				r.finish()
				done <- result
				if result.quit || result.err != nil {
					stdin.Close()
				}
			}()

			if err == nil && command {
				var text string
				text, result.quit, err = s.command(source)
				if err == nil {
					err = d.show(evalCtx, text, text, func(text string) string {
						return highlightCommand(source, text)
					})
				}
			}
			if err == nil && !command {
				for _, st := range stmts {
					var (
						v       value
						display bool
					)
					eval, stop := c.context(call)
					v, display, err = s.execute(eval, st)
					stop()
					if err != nil {
						break
					}

					if display {
						if c.json {
							err = c.print(out, v)
						} else {
							err = d.value(evalCtx, v)
						}
					} else if !c.json {
						_, err = fmt.Fprintf(out, "%s (deferred)\n", st.name)
					}
					if err != nil {
						break
					}
				}
			}
			cancel()
			if err != nil {
				if _, writeErr := fmt.Fprintln(env.Stderr, clean(err.Error())); writeErr != nil {
					result.err = writeErr
					return
				}
			}
		}(source, err)
		source = ""
	}
}

const intro = `Connected to %s (schema v%d).

Tab completes methods and arguments.
:help explores the API; Ctrl-C cancels; Ctrl-D exits.

`

const helpText = `Methods take named arguments. A bare method name shows its documentation.
Tab completes methods and arguments without making calls.

let saves an expression's first result or error, including cancellation.
Rebind to try again; use a function for fresh calls:
  let twice = x => x * 2
  [1, 2, 3] | map(twice)

_ holds the last successful result. x | f(y) means f(x, y).
nil and null both mean a missing value. JSON uses null.
Pipelines work on fetched values; they do not fetch more pages.

  :help [NAME]  help for a method, type, or function
  :schema       show the schema
  :vars         show user bindings
  :quit         exit

Ctrl-C cancels input or evaluation; Ctrl-D exits. Ctrl-R searches history.
Use purl -help for endpoints, flags, paging, and history settings.
PlugScript guide: https://github.com/astrophena/plug/blob/master/docs/purl.md
`

func (s *session) command(source string) (string, bool, error) {
	parts := strings.Fields(source)
	if len(parts) == 0 {
		return "", false, nil
	}
	var text string
	switch parts[0] {
	case ":quit":
		if len(parts) != 1 {
			return "", false, fmt.Errorf("usage: :quit")
		}
		return "", true, nil
	case ":schema":
		if len(parts) != 1 {
			return "", false, fmt.Errorf("usage: :schema")
		}
		text = s.schema.Source
	case ":help":
		if len(parts) == 1 {
			var b strings.Builder
			b.WriteString(helpText)
			b.WriteString("\nBuiltins:\n")
			w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "  FUNCTION\tDESCRIPTION")
			funcs := builtins()
			for _, name := range slices.Sorted(maps.Keys(funcs)) {
				fn := funcs[name]
				fmt.Fprintf(w, "  %s(%s)\t%s\n", name, strings.Join(fn.params, ", "), fn.doc)
			}
			w.Flush()
			b.WriteString("\nMethods:\n")
			for _, method := range s.schema.Methods {
				fmt.Fprintf(&b, "  %s\n", method.Name)
			}
			text = b.String()
		} else if len(parts) == 2 {
			var err error
			text, err = s.help(parts[1])
			if err != nil {
				return "", false, err
			}
		} else {
			return "", false, fmt.Errorf("usage: :help [NAME]")
		}
	case ":vars":
		if len(parts) != 1 {
			return "", false, fmt.Errorf("usage: :vars")
		}
		bindings := make(map[string]*binding)
		for env := s.env; env != nil && env.parent != nil; env = env.parent {
			for name, b := range env.bindings {
				if _, found := bindings[name]; !found {
					bindings[name] = b
				}
			}
		}
		names := slices.Sorted(maps.Keys(bindings))
		var b strings.Builder
		for _, name := range names {
			v := bindings[name]
			state := "deferred"
			if v.done {
				state = typeName(v.value)
				if v.err != nil {
					state = "failed"
				}
			}
			fmt.Fprintf(&b, "%s: %s\n", name, state)
		}
		text = b.String()
	default:
		return "", false, fmt.Errorf("unknown command %q; use :help", parts[0])
	}

	if parts[0] == ":help" {
		text = strings.TrimRight(text, "\n") + "\n\n"
	}
	return clean(text), false, nil
}

func (s *session) help(name string) (string, error) {
	var b strings.Builder
	for _, method := range s.schema.Methods {
		if method.Name != name {
			continue
		}
		return methodHelp(method), nil
	}
	if typ := s.types[name]; typ != nil {
		fmt.Fprintf(&b, "%s\n\n%s\n", typ.Name, typ.Documentation)
		if typ.Underlying != "" {
			fmt.Fprintf(&b, "\n    type %s = %s\n", typ.Name, typ.Underlying)
		}
		for _, ctor := range typ.Constructors {
			fmt.Fprintf(&b, "\n    %s\n", ctor.Name)
			for _, f := range ctor.Fields {
				fmt.Fprintf(&b, "      %s: %s\n", f.Name, schemaType(f.Type))
			}
			fmt.Fprintf(&b, "    = %s\n\n%s\n", typ.Name, ctor.Documentation)
		}
		return b.String(), nil
	}
	v := s.knownSource(name)
	if fn, ok := v.data.(*function); ok {
		if fn.method != nil {
			return methodHelp(*fn.method), nil
		}
		fmt.Fprintf(&b, "    %s(%s)\n", name, strings.Join(fn.params, ", "))
		if fn.doc != "" {
			fmt.Fprintf(&b, "\n%s\n", fn.doc)
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("no documentation for %q", name)
}
