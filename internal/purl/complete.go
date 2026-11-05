// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"slices"
	"strings"
	"text/scanner"
	"unicode"
	"unicode/utf8"

	"go.astrophena.name/plug"
)

// known finds values or types without running expressions or forcing bindings.
func (s *session) known(env *scope, x expr, depth int) value {
	if depth > maxDepth {
		return value{}
	}
	switch n := x.node.(type) {
	case nameExpr:
		if b, ok := env.lookup(n.name); ok {
			if b.done && b.err == nil {
				return b.value
			}
			if !b.done {
				return s.known(b.env, b.x, depth+1)
			}
		}
	case selectExpr:
		v := s.known(env, n.x, depth+1)
		if r, ok := v.data.(*record); ok {
			f, _ := r.lookup(n.name)
			return f
		}
		for _, f := range s.knownFields(v) {
			if f.Name == n.name {
				t := f.Type
				return value{typ: &t}
			}
		}
	case indexExpr:
		v := s.known(env, n.x, depth+1)
		if v.typ != nil && v.typ.Kind == plug.TypeExprList {
			return value{typ: v.typ.Element}
		}
	case callExpr:
		v := s.known(env, n.fn, depth+1)
		if fn, ok := v.data.(*function); ok {
			return value{typ: fn.response}
		}
	case lambdaExpr:
		return value{
			data: &function{
				params: n.params,
			},
		}
	}
	return value{}
}

func (s *session) knownFields(v value) []plug.FieldInfo {
	if v.typ == nil || v.typ.Kind != plug.TypeExprNamed {
		return nil
	}
	t := s.types[v.typ.Name]
	if t == nil || t.Kind != plug.TypeRecord {
		return nil
	}
	return t.Constructors[0].Fields
}

func (s *session) knownSource(src string) value {
	x, ok := parseExpr(src)
	if !ok {
		return value{}
	}
	return s.known(s.env, x, 0)
}

func parseExpr(src string) (expr, bool) {
	stmts, err := parse(src)
	if err != nil || len(stmts) != 1 || stmts[0].name != "" {
		return expr{}, false
	}
	return stmts[0].x, true
}

func (s *session) complete(line string) ([]string, string) {
	start := len(line)
	for start > 0 {
		r, n := utf8.DecodeLastRuneInString(line[:start])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			break
		}
		start -= n
	}

	var (
		prefix     = line[start:]
		before     = line[:start]
		candidates []string
	)
	if strings.HasPrefix(strings.TrimSpace(line), ":") {
		if _, after, ok := strings.Cut(line, " "); ok {
			prefix = strings.TrimSpace(after)
			for _, method := range s.schema.Methods {
				candidates = append(candidates, method.Name)
			}
			for _, typ := range s.schema.Types {
				candidates = append(candidates, typ.Name)
			}
		} else {
			prefix = strings.TrimSpace(line)
			candidates = []string{":help", ":schema", ":vars", ":quit"}
		}
	} else if head, ok := strings.CutSuffix(before, "."); ok {
		source := exprTail(head)
		v := s.knownSource(source)
		if r, ok := v.data.(*record); ok {
			for _, f := range r.fields {
				candidates = append(candidates, f.name)
			}
		} else {
			for _, f := range s.knownFields(v) {
				candidates = append(candidates, f.Name)
			}
		}
	} else {
		for env := s.env; env != nil; env = env.parent {
			for name := range env.bindings {
				candidates = append(candidates, name)
			}
		}
		candidates = append(candidates, "let ", "true", "false", "nil")

		// Tokenize so parentheses in strings and comments do not count.
		var sc scanner.Scanner
		sc.Init(strings.NewReader(before))
		sc.Error = func(*scanner.Scanner, string) {}
		var opens []int
		for kind := sc.Scan(); kind != scanner.EOF; kind = sc.Scan() {
			if kind == '(' {
				opens = append(opens, sc.Position.Offset)
			}
			if kind == ')' && len(opens) > 0 {
				opens = opens[:len(opens)-1]
			}
		}
		if len(opens) > 0 {
			open := opens[len(opens)-1]
			v := s.knownSource(exprTail(before[:open]))
			if fn, ok := v.data.(*function); ok {
				for _, name := range fn.params {
					if !strings.Contains(before[open+1:], name+":") {
						candidates = append(candidates, name+": ")
					}
				}
			}
		}
	}

	slices.Sort(candidates)
	candidates = slices.Compact(candidates)
	out := candidates[:0]
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, prefix) {
			out = append(out, candidate)
		}
	}
	return out, prefix
}

func exprTail(source string) string {
	// Try the whole expression, then suffixes after statement separators.
	source = strings.TrimSpace(source)
	if _, ok := parseExpr(source); ok {
		return source
	}
	for i := range source {
		if strings.ContainsRune("=|,;\n{(", rune(source[i])) {
			tail := strings.TrimSpace(source[i+1:])
			if _, ok := parseExpr(tail); ok {
				return tail
			}
		}
	}
	return ""
}
