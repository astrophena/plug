// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package schema parses and validates Plug schemas, checks compatibility, and
// reports lint diagnostics. [RuntimeSchema] returns metadata for generation and
// inspection. The parsed declarations stay private to this package.
package schema

import "go/token"

// Schema is a parsed and validated Plug schema. Use [Parse] to construct a schema.
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
	var value string
	if t.element != nil {
		value = "list<" + t.element.String() + ">"
	} else {
		value = t.name
	}
	if t.optional {
		value += "?"
	}
	return value
}

var primitives = map[string]bool{
	"bool":   true,
	"bytes":  true,
	"f64":    true,
	"i32":    true,
	"i64":    true,
	"raw":    true,
	"string": true,
	"u32":    true,
	"u64":    true,
	"void":   true,
}

func isPrimitive(name string) bool { return primitives[name] }

func isRaw(ref typeRef) bool {
	return ref.name == "raw" && ref.element == nil && !ref.optional
}

func validIdentifier(name string) bool {
	return token.IsIdentifier(name) && name != "_"
}
