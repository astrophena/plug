// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"go.astrophena.name/plug"
)

func TestFormatEscapesTerminalControls(t *testing.T) {
	v := value{
		data: "\x1b[2J\x00\u009b31m",
	}
	plain := format(v, 0)
	for _, text := range []string{plain, highlight(plain)} {
		if strings.Contains(text, "\x1b[2J") || strings.ContainsRune(text, '\x00') || strings.ContainsRune(text, '\u009b') {
			t.Fatalf("unsafe output %q", text)
		}
	}
	if text := clean("doc\x1b[2J\nnext"); strings.ContainsRune(text, '\x1b') || !strings.Contains(text, "\n") {
		t.Fatal(text)
	}
}

func uncolor(text string) string {
	return regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(text, "")
}

func TestHighlight(t *testing.T) {
	source := `let names = users | map(u => "hello") // saved`
	text := highlight(source)
	for _, want := range []string{
		"\x1b[35mlet\x1b[39m",
		"\x1b[34mmap\x1b[39m",
		"\x1b[36m|\x1b[39m",
		"\x1b[32m\"hello\"\x1b[39m",
		"\x1b[90m// saved\x1b[39m",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("highlighting does not contain %q: %q", want, text)
		}
	}
	if uncolor(text) != source {
		t.Fatalf("highlighting changed source to %q", uncolor(text))
	}
	if text := highlight("names"); text != "names" {
		t.Fatalf("ordinary name is colored: %q", text)
	}
	for _, source := range []string{"", "let", "greeter.getUser(", `"unfinished`, `"hello\`, `let привет = nil`, "/* unfinished", "[\n1,\n2]"} {
		if text := uncolor(highlight(source)); text != source {
			t.Fatalf("highlighting changed %q to %q", source, text)
		}
	}
}

func TestBuiltinHighlighting(t *testing.T) {
	for name := range builtins() {
		if text := highlight(name); text != "\x1b[34m"+name+"\x1b[39m" {
			t.Errorf("builtin %s is not highlighted: %q", name, text)
		}
	}
}

func TestPainterContinuation(t *testing.T) {
	r := repl{
		prefix: "/* first\n",
	}
	text := string(r.Paint([]rune("still a comment"), 0))
	if text != "\x1b[90mstill a comment\x1b[39m" {
		t.Fatalf("continuation = %q", text)
	}
	r.prefix = "let привет = [\n"
	text = string(r.Paint([]rune("nil, 2]"), 0))
	if uncolor(text) != "nil, 2]" || !strings.Contains(text, "\x1b[33mnil\x1b[39m") {
		t.Fatalf("Unicode prefix changed the painted line: %q", text)
	}
}

func TestFormatTimestamp(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	cases := map[string]struct {
		text string
		age  string
	}{
		"past": {
			"2026-07-01T09:00:00Z",
			"3 hours ago",
		},
		"fraction": {
			"2026-07-01T09:00:00.123456789Z",
			"2 hours ago",
		},
		"offset": {
			"2026-07-01T11:00:00+02:00",
			"3 hours ago",
		},
		"future": {
			"2026-07-03T12:00:00Z",
			"in 2 days",
		},
		"now": {
			"2026-07-01T12:00:00Z",
			"just now",
		},
		"date only": {
			"2026-07-01",
			"",
		},
		"invalid": {
			"not a timestamp",
			"",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			v := value{
				data: tc.text,
			}
			want := `"` + tc.text + `"`
			if tc.age != "" {
				want += " (" + tc.age + ")"
			}
			if got := formatAt(v, 0, now); got != want {
				t.Fatalf("formatted timestamp = %q, want %q", got, want)
			}
			if got := format(v, 0); got != `"`+tc.text+`"` {
				t.Fatalf("batch timestamp changed: %s", got)
			}
			if got, err := marshalJSON(v); err != nil || got != `"`+tc.text+`"` {
				t.Fatalf("JSON timestamp = %s, %v", got, err)
			}
		})
	}
	v := value{
		data: []value{
			{
				data: "2026-07-01T09:00:00Z",
			},
			{},
		},
	}
	if got := formatAt(v, 0, now); got != `["2026-07-01T09:00:00Z" (3 hours ago), nil]` {
		t.Fatal(got)
	}
}

func TestFormatNil(t *testing.T) {
	for _, v := range []value{
		{},
		{
			typ: &plug.TypeExpr{
				Kind:     plug.TypeExprPrimitive,
				Name:     "string",
				Optional: true,
			},
		},
	} {
		if got := format(v, 0); got != "nil" {
			t.Fatal(got)
		}
		if got, err := marshalJSON(v); err != nil || got != "null" {
			t.Fatalf("JSON nil = %s, %v", got, err)
		}
	}
}

func TestDisplayLimitDoesNotTruncateJSON(t *testing.T) {
	v := value{
		data: []value{
			{
				data: strings.Repeat("é", 100),
			},
		},
	}
	text := format(v, 30)
	if !strings.Contains(text, "display truncated") {
		t.Fatal(text)
	}
	full, err := marshalJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(full, strings.Repeat("é", 100)) || strings.Contains(full, "truncated") {
		t.Fatal(full)
	}
}
