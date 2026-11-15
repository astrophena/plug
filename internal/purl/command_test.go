// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/cli/clitest"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
)

var update = flag.Bool("update", false, "update transcript golden files")

func TestCommand(t *testing.T) {
	server, _, _ := testServer(t)
	endpoint := serveTest(t, server, "tcp", "127.0.0.1:0")
	file := filepath.Join(t.TempDir(), "script.purl")
	if err := os.WriteFile(file, []byte("greeter.getUser(id: 2).name"), 0600); err != nil {
		t.Fatal(err)
	}
	clitest.Run(t, func(*testing.T) *Command { return new(Command) }, map[string]clitest.Case[*Command]{
		"expression": {
			Args:         []string{"-e", "greeter.getUser(id: 1).name", endpoint},
			WantInStdout: `"Alice"`,
		},
		"bare TCP": {
			Args:         []string{"-e", "greeter.getUser(id: 1).name", strings.TrimPrefix(endpoint, "tcp://")},
			WantInStdout: `"Alice"`,
		},
		"JSON": {
			Args:         []string{"-json", "-e", "greeter.listUsers(limit: 2) | map(u => u.name)", endpoint},
			WantInStdout: `["Alice","Bob"]`,
		},
		"stdin": {
			Args:         []string{endpoint},
			Stdin:        strings.NewReader("let user = greeter.getUser(id: 2)\nuser.name\n"),
			WantInStdout: `"Alice"`,
		},
		"file": {
			Args:         []string{"-f", file, endpoint},
			WantInStdout: `"Alice"`,
		},
		"empty expression": {
			Args:               []string{"-e", "", endpoint},
			Stdin:              strings.NewReader("missing()"),
			WantNothingPrinted: true,
		},
		"no endpoint": {
			WantErr: cli.ErrInvalidArgs,
		},
		"conflicting modes": {
			Args:    []string{"-e", "1", "-f", file, endpoint},
			WantErr: cli.ErrInvalidArgs,
		},
		"negative timeout": {
			Args:    []string{"-timeout", "-1s", endpoint},
			WantErr: cli.ErrInvalidArgs,
		},
	})
}

func TestScriptStopsOnError(t *testing.T) {
	for source, want := range map[string]string{
		"missing()":             "unknown name",
		`assert(false, "stop")`: "assert: stop",
	} {
		t.Run(source, func(t *testing.T) {
			s, calls := testSession(t)
			err := new(Command).script(t.Context(), s, strings.NewReader(source+"\ngreeter.ping()"), io.Discard)
			if err == nil || !strings.Contains(err.Error(), want) || calls.Load() != 0 {
				t.Fatalf("script: %v, calls=%d; want %q and no calls", err, calls.Load(), want)
			}
		})
	}
}

func TestCommandSanitizesErrors(t *testing.T) {
	ctx := cli.WithEnv(t.Context(), &cli.Env{
		Args:   []string{"bad\x1b[2J://host"},
		Getenv: func(string) string { return "" },
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	err := new(Command).Run(ctx)
	if err == nil || strings.ContainsRune(err.Error(), '\x1b') {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestTranscripts(t *testing.T) {
	testutil.RunGolden(t, "testdata/*.txtar", func(t *testing.T, path string) []byte {
		ar, err := txtar.ParseFile(path)
		if err != nil {
			t.Fatal(err)
		}
		s, _ := testSession(t)
		var out strings.Builder
		for _, f := range ar.Files {
			fmt.Fprintf(&out, "-- %s --\n", f.Name)
			if err := new(Command).script(t.Context(), s, strings.NewReader(string(f.Data)), &out); err != nil {
				fmt.Fprintf(&out, "error: %v\n", err)
			}
		}
		return []byte(out.String())
	}, *update)
}
