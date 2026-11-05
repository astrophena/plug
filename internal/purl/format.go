// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"go.astrophena.name/base/humanfmt"
	"go.astrophena.name/plug"
	syntax "go.astrophena.name/plug/syntax/chroma"

	"github.com/alecthomas/chroma/v2"
)

type safeError struct {
	err error
}

func (e *safeError) Error() string { return clean(e.err.Error()) }
func (e *safeError) Unwrap() error { return e.err }

// clean permits layout but never terminal control sequences from a peer.
func clean(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return '\uFFFD'
		}
		return r
	}, text)
}

func identifier(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r != '_' && !unicode.IsLetter(r) && !(i > 0 && unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}

func highlight(text string) string { return colorize(text, syntax.Script, 0) }

// colorize uses the terminal's palette, without changing its background or
// making ordinary identifiers compete with strings, keywords, and functions.
// skip hides a clean byte prefix while retaining its lexical state.
func colorize(text string, lexer chroma.Lexer, skip int) string {
	text = clean(text)
	tokens, err := chroma.Coalesce(lexer).Tokenise(nil, text)
	if err != nil {
		return text[skip:]
	}
	var b strings.Builder
	for token := tokens(); token != chroma.EOF; token = tokens() {
		if skip >= len(token.Value) {
			skip -= len(token.Value)
			continue
		}
		text := token.Value[skip:]
		skip = 0
		var color string
		switch {
		case token.Type == chroma.KeywordConstant, token.Type.InSubCategory(chroma.Number):
			color = "33"
		case token.Type == chroma.KeywordType, token.Type == chroma.NameClass:
			color = "33"
		case token.Type.InCategory(chroma.Keyword):
			color = "35"
		case token.Type == chroma.NameFunction, token.Type.InSubCategory(chroma.NameBuiltin):
			color = "34"
		case token.Type == chroma.StringEscape, token.Type.InCategory(chroma.Operator):
			color = "36"
		case token.Type.InSubCategory(chroma.String):
			color = "32"
		case token.Type.InCategory(chroma.Comment):
			color = "90"
		}
		if color == "" {
			b.WriteString(text)
			continue
		}
		for line := range strings.SplitAfterSeq(text, "\n") {
			body, newline := strings.CutSuffix(line, "\n")
			if body != "" {
				b.WriteString("\x1b[" + color + "m" + body + "\x1b[39m")
			}
			if newline {
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}

func highlightCommand(source, text string) string {
	parts := strings.Fields(source)
	if len(parts) == 0 {
		return clean(text)
	}
	if parts[0] == ":schema" {
		return colorize(text, syntax.Lexer, 0)
	}
	if parts[0] != ":help" {
		return clean(text)
	}
	lexer := syntax.Script
	if len(parts) == 2 {
		lexer = syntax.Lexer
	}
	var b strings.Builder
	builtins := false
	for line := range strings.SplitAfterSeq(text, "\n") {
		if line == "Builtins:\n" {
			builtins = true
		}
		if line == "Methods:\n" {
			builtins = false
			lexer = syntax.Lexer
		}
		// Generated API declarations and Go documentation use code indentation;
		// ordinary prose and two-space list items keep the default foreground.
		prefix := "    "
		if len(parts) == 1 {
			prefix = "  "
		} else if strings.HasPrefix(line, "\t") {
			prefix = "\t"
		}
		if code, ok := strings.CutPrefix(line, prefix); ok {
			if builtins {
				signature, description, _ := strings.Cut(code, "  ")
				b.WriteString(prefix + highlight(signature) + clean("  "+description))
			} else {
				b.WriteString(prefix + colorize(code, lexer, 0))
			}
		} else {
			b.WriteString(clean(line))
		}
	}
	return b.String()
}

func highlightValue(v value, text string) string {
	if fn, ok := v.data.(*function); ok && fn.method != nil {
		return highlightCommand(":help "+fn.name, text)
	}
	return highlight(text)
}

func methodHelp(method plug.MethodInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "    %s\n", method.Name)
	for _, f := range method.Fields {
		fmt.Fprintf(&b, "      %s: %s\n", f.Name, schemaType(f.Type))
	}
	fmt.Fprintf(&b, "    -> %s\n", schemaType(method.Response))
	if method.Error != "" {
		fmt.Fprintf(&b, "      throws %s\n", method.Error)
	}
	if method.Permission != "" {
		fmt.Fprintf(&b, "      requires %s\n", method.Permission)
	}
	fmt.Fprintf(&b, "\n%s\n", method.Documentation)
	for _, f := range method.Fields {
		if f.Documentation != "" {
			fmt.Fprintf(&b, "\n%s: %s\n", f.Name, f.Documentation)
		}
	}
	return clean(strings.TrimRight(b.String(), "\n") + "\n\n")
}

func format(v value, limit int) string { return formatAt(v, limit, time.Time{}) }

func formatAt(v value, limit int, now time.Time) string {
	f := formatter{
		limit: limit,
		now:   now,
	}
	f.value(v, 0)
	if f.truncated {
		f.b.WriteString("\n… (display truncated; the full value is available in _)")
	}
	return f.b.String()
}

type formatter struct {
	b         strings.Builder
	limit     int
	now       time.Time
	truncated bool
}

func (f *formatter) write(text string) {
	if f.truncated {
		return
	}
	if f.limit > 0 && f.b.Len()+len(text) > f.limit {
		n := f.limit - f.b.Len()
		for n > 0 && !utf8.RuneStart(text[n]) {
			n--
		}
		f.b.WriteString(text[:n])
		f.truncated = true
		return
	}
	f.b.WriteString(text)
}

func (f *formatter) value(v value, depth int) {
	if f.truncated {
		return
	}
	if depth > maxDepth || f.limit > 0 && depth > 12 {
		f.truncated = true
		return
	}
	switch x := v.data.(type) {
	case nil:
		if v.typ != nil && v.typ.Name == "void" {
			f.write("void")
		} else {
			f.write("nil")
		}
	case bool:
		f.write(strconv.FormatBool(x))
	case string:
		f.write(strconv.QuoteToGraphic(x))
		if !f.now.IsZero() {
			if t, err := time.Parse(time.RFC3339Nano, x); err == nil {
				f.write(" (" + humanfmt.RelativeTime(t, f.now) + ")")
			}
		}
	case []byte:
		f.write("bytes(" + strconv.QuoteToGraphic(string(x)) + ")")
	case *big.Int:
		f.write(x.String())
	case float64:
		f.write(strconv.FormatFloat(x, 'g', -1, 64))
	case *function:
		if depth == 0 && x.method != nil {
			f.write(strings.TrimSuffix(methodHelp(*x.method), "\n"))
		} else {
			f.write("<function " + clean(x.name) + "(" + strings.Join(x.params, ", ") + ")>")
		}
	case []value:
		f.write("[")
		for i, x := range x {
			if f.truncated {
				break
			}
			if i > 0 {
				f.write(", ")
			}
			f.value(x, depth+1)
		}
		f.write("]")
	case *record:
		close := "}"
		if x.ctor != "" {
			f.write(x.ctor + "(")
			close = ")"
		} else {
			f.write("{")
		}
		for i, field := range x.fields {
			if f.truncated {
				break
			}
			if i > 0 {
				f.write(",")
			}
			name := field.name
			if !identifier(name) {
				name = strconv.QuoteToGraphic(name)
			}
			f.write("\n" + strings.Repeat("  ", depth+1) + name + ": ")
			f.value(field.value, depth+1)
		}
		if len(x.fields) > 0 {
			f.write("\n" + strings.Repeat("  ", depth))
		}
		f.write(close)
	default:
		f.write(fmt.Sprintf("<%T>", x))
	}
}
