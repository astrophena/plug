// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"strconv"
	"strings"
)

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

func newLexer(filename, source string) *lexer {
	return &lexer{
		filename: filename,
		source:   source,
		line:     1,
		column:   1,
	}
}

func (l *lexer) next() (tokenValue, error) {
	for l.offset < len(l.source) && isSpace(l.source[l.offset]) {
		l.advance(l.source[l.offset])
	}

	position := l.position()
	if l.offset == len(l.source) {
		return tokenValue{
			kind:     tokenEOF,
			position: position,
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
			position: position,
		}, nil
	}

	if strings.HasPrefix(l.source[l.offset:], "->") {
		l.advance('-')
		l.advance('>')
		return tokenValue{
			kind:     tokenArrow,
			text:     "->",
			position: position,
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
			position: position,
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
			position: position,
		}, nil
	}

	if l.source[l.offset] == '"' {
		start := l.offset
		l.advance('"')
		for l.offset < len(l.source) && l.source[l.offset] != '"' {
			if l.source[l.offset] == '\n' {
				return tokenValue{}, diagnostic(position, "unterminated string")
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
			return tokenValue{}, diagnostic(position, "unterminated string")
		}
		l.advance('"')
		value, err := strconv.Unquote(l.source[start:l.offset])
		if err != nil {
			return tokenValue{}, diagnostic(position, "invalid string: %v", err)
		}
		return tokenValue{
			kind:     tokenString,
			text:     value,
			position: position,
		}, nil
	}

	value := l.source[l.offset]
	l.advance(value)
	var kind tokenKind
	switch value {
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
		return tokenValue{}, diagnostic(position, "unexpected character %q", value)
	}
	return tokenValue{
		kind:     kind,
		text:     string(value),
		position: position,
	}, nil
}

func (l *lexer) position() Position {
	return Position{
		Filename: l.filename,
		Line:     l.line,
		Column:   l.column,
	}
}

func (l *lexer) advance(value byte) {
	l.offset++
	if value == '\n' {
		l.line++
		l.column = 1
		return
	}
	l.column++
}

func isSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r'
}

func isIdentifierStart(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isIdentifierContinue(value byte) bool {
	return isIdentifierStart(value) || value >= '0' && value <= '9' || value == '.'
}

func isDigit(value byte) bool { return value >= '0' && value <= '9' }
