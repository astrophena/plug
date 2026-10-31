// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"errors"
	"strings"
	"testing"
)

func TestIncompleteInput(t *testing.T) {
	for _, source := range []string{"[", "(1 +", "let x =", "x =>", "() =>", "f(id:", "{x: 1,", `"unfinished`} {
		t.Run(source, func(t *testing.T) {
			_, err := parse(source)
			var syntax *syntaxError
			if !errors.As(err, &syntax) || !syntax.incomplete {
				t.Fatalf("got %v, want incomplete input", err)
			}
		})
	}
}

func TestInvalidSyntax(t *testing.T) {
	for _, source := range []string{"let = 1", "let true = 1", "let _ = 1", "1 2", "[1 2]", "{1}", "(a,a) => a", "(true) => 1", "x = > x", "1 @ 2"} {
		t.Run(source, func(t *testing.T) {
			_, err := parse(source)
			var syntax *syntaxError
			if !errors.As(err, &syntax) || syntax.incomplete {
				t.Fatalf("got %v, want invalid syntax", err)
			}
		})
	}
}

func TestParserDepth(t *testing.T) {
	if _, err := parse(strings.Repeat("[", maxDepth+1)); err == nil {
		t.Fatal("accepted excessive nesting")
	}
}

func FuzzParse(f *testing.F) {
	for _, source := range []string{"1", "greeter.getUser(id: 1)", "[1,2] | map(x => x + 1)", "let f = () => 1", "\xff", "{x:"} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 64<<10 {
			return
		}
		parse(source)
	})
}
