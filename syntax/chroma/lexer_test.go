// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package chroma_test

import (
	"slices"
	"testing"

	chromaplug "go.astrophena.name/plug/syntax/chroma"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

func TestLexerRegistration(t *testing.T) {
	for name, got := range map[string]chroma.Lexer{
		"filename": lexers.Match("schema.plug"),
		"alias":    lexers.Get("plug"),
	} {
		if got != chromaplug.Lexer {
			t.Errorf("%s lookup = %v, want Plug lexer", name, got)
		}
	}
}

func TestLexerTokens(t *testing.T) {
	source := `// A user lookup.
package greeter version 1
permission user.read
category "Users"
type UserID = i64
user id: UserID labels: list<string> email: string? = User
greeter.getUser id: UserID -> User requires user.read http GET "/v1/users/{id}"
greeter.console -> raw
`
	tokens, err := chroma.Tokenise(chroma.Coalesce(chromaplug.Lexer), nil, source)
	if err != nil {
		t.Fatal(err)
	}

	wants := []chroma.Token{
		{Type: chroma.CommentSingle, Value: "// A user lookup."},
		{Type: chroma.Keyword, Value: "package"},
		{Type: chroma.Keyword, Value: "category"},
		{Type: chroma.NumberInteger, Value: "1"},
		{Type: chroma.NameFunction, Value: "user.read"},
		{Type: chroma.NameClass, Value: "UserID"},
		{Type: chroma.KeywordType, Value: "i64"},
		{Type: chroma.NameFunction, Value: "user"},
		{Type: chroma.NameAttribute, Value: "id"},
		{Type: chroma.KeywordType, Value: "list"},
		{Type: chroma.KeywordType, Value: "raw"},
		{Type: chroma.Operator, Value: "->"},
		{Type: chroma.NameBuiltin, Value: "GET"},
		{Type: chroma.StringDouble, Value: `"/v1/users/{id}"`},
	}
	for _, want := range wants {
		if !slices.Contains(tokens, want) {
			t.Errorf("tokens do not contain %#v:\n%#v", want, tokens)
		}
	}
}
