// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package schema parses and validates Plug schemas.
package schema

import (
	"fmt"
	"go/token"
)

// Schema is a parsed and validated Plug schema.
type Schema struct {
	packageName   string
	position      Position
	major         uint16
	documentation string
	permissions   []permission
	newtypes      []newtype
	types         []declaredType // record and sum documentation declarations
	constructors  []constructor
	categories    []category
	methods       []method
}

type category struct {
	name          string
	documentation string
	types         []string
	methods       []string
	position      Position
}

type declaredType struct {
	name          string
	documentation string
	position      Position
}

type permission struct {
	name          string
	documentation string
	position      Position
}

type newtype struct {
	name          string
	documentation string
	base          string
	position      Position
}

type constructor struct {
	name          string
	documentation string
	fields        []field // wire order
	result        string
	position      Position
	id            uint32
	httpStatus    int
}

type method struct {
	name          string
	documentation string
	fields        []field // wire order
	response      typeRef
	errorType     *typeRef
	position      Position
	id            uint32
	public        bool
	permission    string
	http          []httpBinding
}

type httpBinding struct {
	verb     string
	path     string
	position Position
}

type field struct {
	name          string
	documentation string
	typ           typeRef
	position      Position
}

type typeRef struct {
	name     string
	element  *typeRef // non-nil for lists
	optional bool
}

// String returns the canonical spelling of a type reference.
func (t typeRef) String() string {
	var s string
	if t.element != nil {
		s = "list<" + t.element.String() + ">"
	} else {
		s = t.name
	}
	if t.optional {
		s += "?"
	}
	return s
}

func isPrimitive(name string) bool {
	switch name {
	case "bool", "bytes", "f64", "i32", "i64", "raw", "string", "u32", "u64", "void":
		return true
	}
	return false
}

func isRaw(ref typeRef) bool {
	return ref.name == "raw" && ref.element == nil && !ref.optional
}

func validIdentifier(name string) bool {
	return token.IsIdentifier(name) && name != "_"
}

// Position identifies a source location.
// Line and Column are one-based; Column counts bytes.
type Position struct {
	Filename string
	Line     int
	Column   int
}

// String returns filename:line:column, omitting an empty filename.
func (p Position) String() string {
	if p.Filename == "" {
		return fmt.Sprintf("%d:%d", p.Line, p.Column)
	}
	return fmt.Sprintf("%s:%d:%d", p.Filename, p.Line, p.Column)
}

// Error is a diagnostic at a source position.
type Error struct {
	Position Position
	Message  string
}

// Error returns the source position and diagnostic message.
func (e *Error) Error() string { return e.Position.String() + ": " + e.Message }

func diagnostic(pos Position, format string, values ...any) error {
	return &Error{
		Position: pos,
		Message:  fmt.Sprintf(format, values...),
	}
}
