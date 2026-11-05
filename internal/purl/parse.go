// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"text/scanner"
)

const maxDepth = 256

type token struct {
	text string
	kind rune
	pos  scanner.Position
}

type syntaxError struct {
	pos        scanner.Position
	message    string
	incomplete bool
}

func (e *syntaxError) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.pos.Line, e.pos.Column, e.message)
}

// expr pairs a syntax node with its position for error reporting.
type expr struct {
	pos  scanner.Position
	node any
}

type nameExpr struct {
	name string
}

type literalExpr struct {
	text  string
	kind  rune
	once  sync.Once
	value value
	err   error
}

func (n *literalExpr) load() (value, error) {
	n.once.Do(func() {
		switch n.kind {
		case scanner.String:
			n.value.data, n.err = strconv.Unquote(n.text)
		case scanner.Int:
			z, ok := new(big.Int).SetString(n.text, 0)
			if !ok {
				n.err = fmt.Errorf("invalid integer %q", n.text)
			}
			n.value.data = z
		case scanner.Float:
			n.value.data, n.err = strconv.ParseFloat(n.text, 64)
		}
	})
	return n.value, n.err
}

type unaryExpr struct {
	op string
	x  expr
}

type binaryExpr struct {
	op string
	x  expr
	y  expr
}

type selectExpr struct {
	x    expr
	name string
}

type indexExpr struct {
	x     expr
	index expr
}

type arg struct {
	name string
	x    expr
}

type callExpr struct {
	fn   expr
	args []arg
}

type listExpr struct {
	items []expr
}

type recordExpr struct {
	fields []arg
}

type lambdaExpr struct {
	params []string
	body   expr
}

type stmt struct {
	name string
	x    expr
}

type parser struct {
	tokens []token
	i      int
	depth  int
}

func parse(src string) ([]stmt, error) {
	var (
		s       scanner.Scanner
		tokens  []token
		scanErr error
	)
	s.Init(strings.NewReader(src))
	s.Mode = scanner.ScanIdents | scanner.ScanInts | scanner.ScanFloats | scanner.ScanStrings | scanner.ScanComments | scanner.SkipComments
	s.Whitespace &^= 1 << '\n'

	s.Error = func(s *scanner.Scanner, message string) {
		if scanErr == nil {
			scanErr = &syntaxError{
				pos:        s.Position,
				message:    message,
				incomplete: strings.Contains(message, "not terminated"),
			}
		}
	}
	for {
		kind := s.Scan()
		tokens = append(tokens, token{
			text: s.TokenText(),
			kind: kind,
			pos:  s.Position,
		})
		if kind == scanner.EOF {
			break
		}
	}
	if scanErr != nil {
		return nil, scanErr
	}

	// Whitespace must not turn "= >" into "=>".
	joined := tokens[:0]
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if i+1 < len(tokens) && tokens[i+1].pos.Offset == t.pos.Offset+len(t.text) {
			pair := t.text + tokens[i+1].text
			switch pair {
			case "=>", "==", "!=", "<=", ">=", "&&", "||":
				t.text = pair
				i++
			}
		}
		joined = append(joined, t)
	}

	var (
		p = parser{
			tokens: joined,
		}
		stmts []stmt
	)
	for {
		for p.accept("\n") || p.accept(";") {
		}
		if p.peek().kind == scanner.EOF {
			return stmts, nil
		}
		var st stmt
		if p.accept("let") {
			t := p.take()
			if t.kind != scanner.Ident || reserved(t.text) {
				return nil, p.error(t, "expected a binding name")
			}
			st.name = t.text
			if err := p.expect("="); err != nil {
				return nil, err
			}
		}

		x, err := p.expr(0)
		if err != nil {
			return nil, err
		}
		st.x = x
		stmts = append(stmts, st)
		if p.peek().kind != scanner.EOF && p.peek().text != "\n" && p.peek().text != ";" {
			return nil, p.error(p.peek(), "expected a newline or semicolon")
		}
	}
}

func reserved(name string) bool {
	return name == "let" || name == "true" || name == "false" || name == "nil" || name == "null" || name == "_"
}

func (p *parser) peek() token { return p.tokens[p.i] }

func (p *parser) take() token {
	t := p.peek()
	if t.kind != scanner.EOF {
		p.i++
	}
	return t
}

func (p *parser) accept(text string) bool {
	if p.peek().text != text {
		return false
	}
	p.take()
	return true
}

func (p *parser) newlines() {
	for p.accept("\n") {
	}
}

func (p *parser) expect(text string) error {
	if p.accept(text) {
		return nil
	}
	return p.error(p.peek(), "expected "+strconv.Quote(text))
}

func (p *parser) error(t token, message string) error {
	return &syntaxError{
		pos:        t.pos,
		message:    message,
		incomplete: t.kind == scanner.EOF,
	}
}

func precedence(op string) int {
	switch op {
	case "|":
		return 1
	case "||":
		return 2
	case "&&":
		return 3
	case "==", "!=", "<", "<=", ">", ">=":
		return 4
	case "+", "-":
		return 5
	case "*", "/", "%":
		return 6
	}
	return 0
}

func (p *parser) expr(min int) (expr, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxDepth {
		return expr{}, p.error(p.peek(), "expression is too deeply nested")
	}

	p.newlines()
	x, err := p.primary()
	if err != nil {
		return expr{}, err
	}
	for {
		// A pipe on the next line continues the preceding expression.
		if p.peek().text == "\n" {
			i := p.i
			p.newlines()
			if p.peek().text != "|" {
				p.i = i
			}
		}
		t := p.peek()
		switch t.text {
		case ".":
			p.take()
			field := p.take()
			if field.kind != scanner.Ident {
				return expr{}, p.error(field, "expected a field name")
			}
			x = expr{
				pos: t.pos,
				node: selectExpr{
					x:    x,
					name: field.text,
				},
			}
			continue
		case "[":
			p.take()
			index, err := p.expr(0)
			if err != nil {
				return expr{}, err
			}
			p.newlines()
			if err := p.expect("]"); err != nil {
				return expr{}, err
			}
			x = expr{
				pos: t.pos,
				node: indexExpr{
					x:     x,
					index: index,
				},
			}
			continue
		case "(":
			p.take()
			args, err := p.args(")", true)
			if err != nil {
				return expr{}, err
			}
			x = expr{
				pos: t.pos,
				node: callExpr{
					fn:   x,
					args: args,
				},
			}
			continue
		}

		prec := precedence(t.text)
		if prec == 0 || prec < min {
			return x, nil
		}
		p.take()
		y, err := p.expr(prec + 1)
		if err != nil {
			return expr{}, err
		}
		x = expr{
			pos: t.pos,
			node: binaryExpr{
				op: t.text,
				x:  x,
				y:  y,
			},
		}
	}
}

func (p *parser) primary() (expr, error) {
	t := p.take()
	x := expr{
		pos: t.pos,
	}
	switch {
	case t.text == "!" || t.text == "-" || t.text == "+":
		y, err := p.expr(7)
		x.node = unaryExpr{
			op: t.text,
			x:  y,
		}
		return x, err
	case t.kind == scanner.Int || t.kind == scanner.Float || t.kind == scanner.String:
		x.node = &literalExpr{
			text: t.text,
			kind: t.kind,
		}
	case t.kind == scanner.Ident:
		if t.text == "let" {
			return expr{}, p.error(t, "let is only allowed at the start of a statement")
		}
		if p.accept("=>") {
			if reserved(t.text) {
				return expr{}, p.error(t, "invalid parameter name")
			}
			body, err := p.expr(0)
			x.node = lambdaExpr{
				params: []string{t.text},
				body:   body,
			}
			return x, err
		}
		x.node = nameExpr{
			name: t.text,
		}
	case t.text == "(":
		// Parameter lists have only identifiers and commas, followed by =>.
		start := p.i
		var params []string
		p.newlines()
		for p.peek().kind == scanner.Ident {
			params = append(params, p.take().text)
			p.newlines()
			if !p.accept(",") {
				break
			}
			p.newlines()
		}
		if p.accept(")") && p.accept("=>") {
			seen := make(map[string]bool)
			for _, name := range params {
				if reserved(name) || seen[name] {
					return expr{}, p.error(t, "invalid or duplicate parameter "+strconv.Quote(name))
				}
				seen[name] = true
			}
			body, err := p.expr(0)
			x.node = lambdaExpr{
				params: params,
				body:   body,
			}
			return x, err
		}

		p.i = start
		y, err := p.expr(0)
		if err != nil {
			return expr{}, err
		}
		p.newlines()
		return y, p.expect(")")
	case t.text == "[":
		args, err := p.args("]", false)
		if err != nil {
			return expr{}, err
		}
		items := make([]expr, len(args))
		for i, arg := range args {
			items[i] = arg.x
		}
		x.node = listExpr{
			items: items,
		}
	case t.text == "{":
		args, err := p.args("}", true)
		if err != nil {
			return expr{}, err
		}
		for _, arg := range args {
			if arg.name == "" {
				return expr{}, p.error(t, "record fields must be named")
			}
		}
		x.node = recordExpr{
			fields: args,
		}
	default:
		return expr{}, p.error(t, "expected an expression")
	}
	return x, nil
}

func (p *parser) args(end string, named bool) ([]arg, error) {
	var args []arg
	p.newlines()
	for !p.accept(end) {
		var arg arg
		if named && p.peek().kind == scanner.Ident && p.i+1 < len(p.tokens) && p.tokens[p.i+1].text == ":" {
			arg.name = p.take().text
			p.take()
		}
		x, err := p.expr(0)
		if err != nil {
			return nil, err
		}
		arg.x = x
		args = append(args, arg)

		p.newlines()
		if p.accept(end) {
			break
		}
		if err := p.expect(","); err != nil {
			return nil, err
		}
		p.newlines()
	}
	return args, nil
}
