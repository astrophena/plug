// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package chroma_test

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
	syntax "go.astrophena.name/plug/syntax/chroma"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

var update = flag.Bool("update", false, "update lexer golden files")

func TestScriptRegistration(t *testing.T) {
	for name, got := range map[string]chroma.Lexer{
		"filename":   lexers.Match("channels.purl"),
		"purl":       lexers.Get("purl"),
		"plugscript": lexers.Get("plugscript"),
	} {
		if got != syntax.Script {
			t.Errorf("%s lookup = %v, want PlugScript lexer", name, got)
		}
	}
}

func TestScriptTokens(t *testing.T) {
	testutil.RunGolden(t, "testdata/*.purl", func(t *testing.T, path string) []byte {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		tokens, err := chroma.Tokenise(chroma.Coalesce(syntax.Script), nil, string(source))
		if err != nil {
			t.Fatal(err)
		}
		var b, text strings.Builder
		for _, token := range tokens {
			text.WriteString(token.Value)
			if token.Type == chroma.Error {
				t.Errorf("unrecognized token %q", token.Value)
			}
			if !token.Type.InCategory(chroma.Text) {
				fmt.Fprintf(&b, "%s %q\n", token.Type, token.Value)
			}
		}
		if text.String() != string(source) {
			t.Fatal("lexer changed the source text")
		}
		return []byte(b.String())
	}, *update)
}

func TestScriptMultilineComment(t *testing.T) {
	for _, source := range []string{"/* first\nsecond */", "/* first\nsecond"} {
		tokens, err := chroma.Tokenise(syntax.Script, nil, source)
		if err != nil {
			t.Fatal(err)
		}
		if len(tokens) != 1 || tokens[0].Type != chroma.CommentMultiline || tokens[0].Value != source {
			t.Fatalf("multiline comment = %#v", tokens)
		}
	}
}
