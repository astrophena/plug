// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"slices"
	"strconv"
	"strings"

	"go.astrophena.name/plug/internal/doc"
)

// Parse parses and validates src as a Plug schema.
func Parse(filename string, src []byte) (*Schema, error) {
	p := &parser{
		lexer: lexer{
			filename: filename,
			source:   string(src),
			line:     1,
			column:   1,
		},
	}
	s, err := p.parse()
	if err != nil {
		return nil, err
	}
	if err := validate(s); err != nil {
		return nil, err
	}
	if err := assignIDs(s); err != nil {
		return nil, err
	}
	return s, nil
}

type parser struct {
	lexer lexer
	peek  *tokenValue
}

func (p *parser) parse() (*Schema, error) {
	docs, err := p.docs()
	if err != nil {
		return nil, err
	}
	kw, err := p.expect(tokenIdentifier, "package")
	if err != nil {
		return nil, err
	}
	if kw.text != "package" {
		return nil, p.unexpected(kw, "package declaration")
	}

	pkg, err := p.expect(tokenIdentifier, "package name")
	if err != nil {
		return nil, err
	}
	major := uint16(1)
	next, err := p.look()
	if err != nil {
		return nil, err
	}
	if next.kind == tokenIdentifier && next.text == "version" {
		p.take()
		value, err := p.expect(tokenNumber, "schema major version")
		if err != nil {
			return nil, err
		}
		version, err := strconv.ParseUint(value.text, 10, 16)
		if err != nil || version == 0 {
			return nil, diagnostic(value.position, "invalid schema major version %q", value.text)
		}
		major = uint16(version)
	}
	if err := p.optionalSemicolon(); err != nil {
		return nil, err
	}

	s := &Schema{
		packageName:   pkg.text,
		position:      pkg.position,
		major:         major,
		documentation: docs,
	}
	cat := -1

	for {
		doc, err := p.docs()
		if err != nil {
			return nil, err
		}
		next, err := p.look()
		if err != nil {
			return nil, err
		}
		if next.kind == tokenEOF {
			if doc != "" {
				return nil, diagnostic(next.position, "documentation is not followed by a declaration")
			}
			return s, nil
		}

		name, err := p.expect(tokenIdentifier, "declaration")
		if err != nil {
			return nil, err
		}
		if name.text == "type" {
			value, err := p.parseType(doc)
			if err != nil {
				return nil, err
			}
			switch v := value.(type) {
			case newtype:
				s.newtypes = append(s.newtypes, v)
				appendCategoryType(s, cat, v.name)
			case declaredType:
				s.types = append(s.types, v)
				appendCategoryType(s, cat, v.name)
			}
			continue
		}
		if name.text == "permission" {
			value, err := p.parsePermission(doc)
			if err != nil {
				return nil, err
			}
			s.permissions = append(s.permissions, value)
			continue
		}
		if name.text == "category" {
			next, err := p.look()
			if err != nil {
				return nil, err
			}
			if next.kind == tokenString {
				value, err := p.parseCategory(doc)
				if err != nil {
					return nil, err
				}
				s.categories = append(s.categories, value)
				cat = len(s.categories) - 1
				continue
			}
		}

		fields, delimiter, err := p.parseFields()
		if err != nil {
			return nil, err
		}
		switch delimiter.kind {
		case tokenEqual:
			value, err := p.parseConstructor(name, doc, fields)
			if err != nil {
				return nil, err
			}
			s.constructors = append(s.constructors, value)
			appendCategoryType(s, cat, value.result)
		case tokenArrow:
			value, err := p.parseMethod(name, doc, fields)
			if err != nil {
				return nil, err
			}
			s.methods = append(s.methods, value)
			if cat >= 0 {
				c := &s.categories[cat]
				c.methods = append(c.methods, name.text)
			}
		default:
			return nil, p.unexpected(delimiter, "'=' or '->'")
		}
	}
}

func (p *parser) parseConstructor(name tokenValue, doc string, fields []field) (constructor, error) {
	result, err := p.expect(tokenIdentifier, "constructor result type")
	if err != nil {
		return constructor{}, err
	}
	status := 0
	next, err := p.look()
	if err != nil {
		return constructor{}, err
	}
	if next.kind == tokenIdentifier && next.text == "httpStatus" {
		p.take()
		value, err := p.expect(tokenNumber, "HTTP status")
		if err != nil {
			return constructor{}, err
		}
		status, err = strconv.Atoi(value.text)
		if err != nil {
			return constructor{}, diagnostic(value.position, "invalid HTTP status %q", value.text)
		}
	}
	if err := p.optionalSemicolon(); err != nil {
		return constructor{}, err
	}
	return constructor{
		name:          name.text,
		documentation: doc,
		fields:        fields,
		result:        result.text,
		position:      name.position,
		httpStatus:    status,
	}, nil
}

func (p *parser) parseMethod(name tokenValue, doc string, fields []field) (method, error) {
	resp, err := p.parseTypeRef()
	if err != nil {
		return method{}, err
	}
	m := method{
		name:          name.text,
		documentation: doc,
		fields:        fields,
		response:      resp,
		position:      name.position,
		public:        true,
	}

	next, err := p.look()
	if err != nil {
		return method{}, err
	}
	if next.kind == tokenIdentifier && next.text == "throws" {
		p.take()
		value, err := p.parseTypeRef()
		if err != nil {
			return method{}, err
		}
		m.errorType = &value
	}

	var accessSet bool
annotations:
	for {
		next, err := p.look()
		if err != nil {
			return method{}, err
		}
		if next.kind != tokenIdentifier {
			break
		}
		switch next.text {
		case "public", "requires":
			if accessSet {
				return method{}, diagnostic(next.position, "method access is declared more than once")
			}
			p.take()
			accessSet = true
			m.public = next.text == "public"
			if !m.public {
				value, err := p.expect(tokenIdentifier, "permission name")
				if err != nil {
					return method{}, err
				}
				m.permission = value.text
			}
		case "http":
			p.take()
			verb, err := p.expect(tokenIdentifier, "HTTP verb")
			if err != nil {
				return method{}, err
			}
			path, err := p.expect(tokenString, "quoted HTTP path")
			if err != nil {
				return method{}, err
			}
			m.http = append(m.http, httpBinding{
				verb:     verb.text,
				path:     path.text,
				position: next.position,
			})
		default:
			break annotations
		}
	}
	if err := p.optionalSemicolon(); err != nil {
		return method{}, err
	}
	return m, nil
}

func (p *parser) parseCategory(docs string) (category, error) {
	name, err := p.expect(tokenString, "quoted category name")
	if err != nil {
		return category{}, err
	}
	if err := p.optionalSemicolon(); err != nil {
		return category{}, err
	}
	return category{
		name:          name.text,
		documentation: docs,
		position:      name.position,
	}, nil
}

func appendCategoryType(s *Schema, index int, name string) {
	if index < 0 {
		return
	}
	c := &s.categories[index]
	if !slices.Contains(c.types, name) {
		c.types = append(c.types, name)
	}
}

func (p *parser) parsePermission(docs string) (permission, error) {
	name, err := p.expect(tokenIdentifier, "permission name")
	if err != nil {
		return permission{}, err
	}
	if err := p.optionalSemicolon(); err != nil {
		return permission{}, err
	}
	return permission{
		name:          name.text,
		documentation: docs,
		position:      name.position,
	}, nil
}

func (p *parser) parseType(docs string) (any, error) {
	name, err := p.expect(tokenIdentifier, "type name")
	if err != nil {
		return nil, err
	}
	next, err := p.look()
	if err != nil {
		return nil, err
	}
	if next.kind == tokenEqual {
		p.take()
		base, err := p.expect(tokenIdentifier, "newtype scalar")
		if err != nil {
			return nil, err
		}
		if err := p.optionalSemicolon(); err != nil {
			return nil, err
		}
		return newtype{
			name:          name.text,
			documentation: docs,
			base:          base.text,
			position:      name.position,
		}, nil
	}

	if err := p.optionalSemicolon(); err != nil {
		return nil, err
	}
	return declaredType{
		name:          name.text,
		documentation: docs,
		position:      name.position,
	}, nil
}

func (p *parser) parseFields() ([]field, tokenValue, error) {
	var fields []field
	for {
		docs, err := p.docs()
		if err != nil {
			return nil, tokenValue{}, err
		}
		next, err := p.look()
		if err != nil {
			return nil, tokenValue{}, err
		}
		if next.kind == tokenEqual || next.kind == tokenArrow {
			if docs != "" {
				return nil, tokenValue{}, diagnostic(next.position, "documentation is not followed by a field")
			}
			p.take()
			return fields, next, nil
		}
		name, err := p.expect(tokenIdentifier, "field name, '=' or '->'")
		if err != nil {
			return nil, tokenValue{}, err
		}
		if _, err := p.expect(tokenColon, "':'"); err != nil {
			return nil, tokenValue{}, err
		}
		typ, err := p.parseTypeRef()
		if err != nil {
			return nil, tokenValue{}, err
		}
		fields = append(fields, field{
			name:          name.text,
			documentation: docs,
			typ:           typ,
			position:      name.position,
		})
	}
}

func (p *parser) parseTypeRef() (typeRef, error) {
	name, err := p.expect(tokenIdentifier, "type")
	if err != nil {
		return typeRef{}, err
	}
	ref := typeRef{
		name: name.text,
	}
	if name.text == "list" {
		if _, err := p.expect(tokenLess, "'<'"); err != nil {
			return typeRef{}, err
		}
		elem, err := p.parseTypeRef()
		if err != nil {
			return typeRef{}, err
		}
		if _, err := p.expect(tokenGreater, "'>'"); err != nil {
			return typeRef{}, err
		}
		ref.name = ""
		ref.element = &elem
	}
	next, err := p.look()
	if err != nil {
		return typeRef{}, err
	}
	if next.kind == tokenQuestion {
		p.take()
		ref.optional = true
	}
	return ref, nil
}

func (p *parser) docs() (string, error) {
	var lines []string
	for {
		next, err := p.look()
		if err != nil {
			return "", err
		}
		if next.kind != tokenDoc {
			return doc.Normalize(strings.Join(lines, "\n")), nil
		}
		p.take()
		lines = append(lines, next.text)
	}
}

func (p *parser) optionalSemicolon() error {
	next, err := p.look()
	if err != nil {
		return err
	}
	if next.kind != tokenSemicolon {
		return nil
	}
	_, err = p.take()
	return err
}

func (p *parser) expect(kind tokenKind, description string) (tokenValue, error) {
	value, err := p.take()
	if err != nil {
		return tokenValue{}, err
	}
	if value.kind != kind {
		return tokenValue{}, p.unexpected(value, description)
	}
	return value, nil
}

func (p *parser) unexpected(value tokenValue, expected string) error {
	description := value.text
	if value.kind == tokenEOF {
		description = "end of file"
	}
	return diagnostic(value.position, "unexpected %q; expected %s", description, expected)
}

func (p *parser) look() (tokenValue, error) {
	if p.peek != nil {
		return *p.peek, nil
	}
	value, err := p.lexer.next()
	if err != nil {
		return tokenValue{}, err
	}
	p.peek = &value
	return value, nil
}

func (p *parser) take() (tokenValue, error) {
	value, err := p.look()
	if err != nil {
		return tokenValue{}, err
	}
	p.peek = nil
	return value, nil
}

type tokenKind uint8

const (
	tokenEOF tokenKind = iota
	tokenIdentifier
	tokenNumber
	tokenString
	tokenDoc
	tokenColon
	tokenSemicolon
	tokenEqual
	tokenQuestion
	tokenLess
	tokenGreater
	tokenArrow
)

type tokenValue struct {
	kind     tokenKind
	text     string
	position Position
}

type lexer struct {
	filename string
	source   string
	offset   int
	line     int
	column   int
}

func (l *lexer) next() (tokenValue, error) {
	for l.offset < len(l.source) && isSpace(l.source[l.offset]) {
		l.advance(l.source[l.offset])
	}

	pos := l.position()
	if l.offset == len(l.source) {
		return tokenValue{
			kind:     tokenEOF,
			position: pos,
		}, nil
	}

	if strings.HasPrefix(l.source[l.offset:], "//") {
		l.advance('/')
		l.advance('/')
		start := l.offset
		for l.offset < len(l.source) && l.source[l.offset] != '\n' {
			l.advance(l.source[l.offset])
		}
		return tokenValue{
			kind:     tokenDoc,
			text:     strings.TrimPrefix(strings.TrimRight(l.source[start:l.offset], " \t\r"), " "),
			position: pos,
		}, nil
	}

	if strings.HasPrefix(l.source[l.offset:], "->") {
		l.advance('-')
		l.advance('>')
		return tokenValue{
			kind:     tokenArrow,
			text:     "->",
			position: pos,
		}, nil
	}

	if isIdentifierStart(l.source[l.offset]) {
		start := l.offset
		for l.offset < len(l.source) && isIdentifierContinue(l.source[l.offset]) {
			l.advance(l.source[l.offset])
		}
		return tokenValue{
			kind:     tokenIdentifier,
			text:     l.source[start:l.offset],
			position: pos,
		}, nil
	}

	if isDigit(l.source[l.offset]) {
		start := l.offset
		for l.offset < len(l.source) && isDigit(l.source[l.offset]) {
			l.advance(l.source[l.offset])
		}
		return tokenValue{
			kind:     tokenNumber,
			text:     l.source[start:l.offset],
			position: pos,
		}, nil
	}

	if l.source[l.offset] == '"' {
		start := l.offset
		l.advance('"')
		for l.offset < len(l.source) && l.source[l.offset] != '"' {
			if l.source[l.offset] == '\n' {
				return tokenValue{}, diagnostic(pos, "unterminated string")
			}
			if l.source[l.offset] == '\\' {
				l.advance('\\')
				if l.offset == len(l.source) {
					break
				}
			}
			l.advance(l.source[l.offset])
		}
		if l.offset == len(l.source) {
			return tokenValue{}, diagnostic(pos, "unterminated string")
		}
		l.advance('"')
		v, err := strconv.Unquote(l.source[start:l.offset])
		if err != nil {
			return tokenValue{}, diagnostic(pos, "invalid string: %v", err)
		}
		return tokenValue{
			kind:     tokenString,
			text:     v,
			position: pos,
		}, nil
	}

	v := l.source[l.offset]
	l.advance(v)
	var kind tokenKind
	switch v {
	case ':':
		kind = tokenColon
	case ';':
		kind = tokenSemicolon
	case '=':
		kind = tokenEqual
	case '?':
		kind = tokenQuestion
	case '<':
		kind = tokenLess
	case '>':
		kind = tokenGreater
	default:
		return tokenValue{}, diagnostic(pos, "unexpected character %q", v)
	}
	return tokenValue{
		kind:     kind,
		text:     string(v),
		position: pos,
	}, nil
}

func (l *lexer) position() Position {
	return Position{
		Filename: l.filename,
		Line:     l.line,
		Column:   l.column,
	}
}

func (l *lexer) advance(b byte) {
	l.offset++
	if b == '\n' {
		l.line++
		l.column = 1
		return
	}
	l.column++
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func isIdentifierStart(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func isIdentifierContinue(b byte) bool {
	return isIdentifierStart(b) || b >= '0' && b <= '9' || b == '.'
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
