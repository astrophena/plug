// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"slices"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
	"go.astrophena.name/plug/example/greeter"
)

func TestCompletionDoesNotEvaluate(t *testing.T) {
	s, calls := testSession(t)
	if _, err := evaluate(t, s, "let user = greeter.getUser(id: 42)"); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"greeter.":                             "getUser",
		"greeter.getU":                         "getUser",
		"greeter.getUser(":                     "id: ",
		"greeter.getUser(i":                    "id: ",
		"greeter.getUser(id: ":                 "greeter",
		"user.":                                "name",
		"user.ro":                              "role",
		"greeter.getUser(id: 1).":              "name",
		"greeter.listUsers(limit: 2)[0].":      "name",
		"let result = greeter.getUser(id: 1).": "email",
		"ni":                                   "nil",
		":he":                                  ":help",
		":help greeter.g":                      "greeter.getUser",
	}
	for line, want := range cases {
		t.Run(line, func(t *testing.T) {
			candidates, _ := s.complete(line)
			if !slices.Contains(candidates, want) {
				t.Fatalf("got %q, want %q", candidates, want)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("completion sent a request")
	}
	candidates, _ := s.complete("greeter.getUser(id: ")
	if slices.Contains(candidates, "id: ") {
		t.Fatal("offered an argument already supplied")
	}
}

func TestCompletionUnicode(t *testing.T) {
	s := newSession(nil, &greeter.Schema)
	if _, err := evaluate(t, s, "let привет = 1"); err != nil {
		t.Fatal(err)
	}
	candidates, prefix := s.complete("при")
	if prefix != "при" || !slices.Contains(candidates, "привет") {
		t.Fatalf("got %q, %q", candidates, prefix)
	}
}

func TestMethodDocumentation(t *testing.T) {
	s, calls := testSession(t)
	if _, err := evaluate(t, s, "greeter.getUser; let lookup = _; lookup; _"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("viewing a method sent a request")
	}
	v, err := evaluate(t, s, "lookup(id: 1).name")
	if err != nil || format(v, 0) != `"Alice"` || calls.Load() != 1 {
		t.Fatalf("method alias stopped being callable: %v, %v", v, err)
	}
}

func TestBuiltinHelp(t *testing.T) {
	testutil.RunGolden(t, "testdata/help/*.txtar", func(t *testing.T, path string) []byte {
		archive, err := txtar.ParseFile(path)
		if err != nil {
			t.Fatal(err)
		}
		s := newSession(nil, &greeter.Schema)
		var b strings.Builder
		for _, file := range archive.Files {
			text, _, err := s.command(string(file.Data))
			if err != nil {
				t.Fatal(err)
			}
			b.WriteString("-- " + file.Name + " --\n" + text)
		}
		return []byte(b.String())
	}, *update)
}

func TestHelpHighlighting(t *testing.T) {
	s, calls := testSession(t)
	if _, err := evaluate(t, s, "let user = greeter.getUser(id: 42)"); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{":vars", ":help", ":help greeter.getUser", ":help User", ":help UserID", ":help count", ":schema"} {
		t.Run(command, func(t *testing.T) {
			plain, _, err := s.command(command)
			if err != nil {
				t.Fatal(err)
			}
			if plain == "" {
				t.Fatal("empty command output")
			}
			colored := highlightCommand(command, plain)
			if uncolor(colored) != plain {
				t.Fatalf("highlighting changed help:\n%s", colored)
			}
			if command != ":vars" && !strings.Contains(colored, "\x1b[") {
				t.Fatal("help has no highlighting")
			}
			if command == ":help" && !strings.Contains(colored, "Whole seconds since an RFC 3339 timestamp; future is negative.\n") {
				t.Fatal("builtin descriptions are colored")
			}
			if command == ":help greeter.getUser" {
				for _, prose := range []string{"\n\ngreeter.getUser returns the requested user.\n", "\n  - A [User] when the caller can read it.\n"} {
					if !strings.Contains(colored, prose) {
						t.Fatal("ordinary documentation text is colored")
					}
				}
				if !strings.Contains(colored, "\t\x1b[34mgreeter.getUser\x1b[39m") {
					t.Fatal("documentation code block is not highlighted")
				}
			}
			if strings.HasPrefix(command, ":help") && !strings.HasSuffix(plain, "\n\n") {
				t.Fatal("help lost the blank line before the next prompt")
			}
			if command == ":schema" && !strings.Contains(colored, "\x1b[35mtype\x1b[39m") {
				t.Fatal("schema did not use the schema lexer")
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("highlighting help sent a request")
	}
}
