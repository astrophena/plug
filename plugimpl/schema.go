// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plugimpl

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

const defaultMaxFrameSize uint32 = 16 << 20

// TypeKind classifies a generated schema type.
type TypeKind uint8

const (
	// TypeNewtype is a distinct scalar type.
	TypeNewtype TypeKind = iota + 1
	// TypeRecord is a concrete positional record.
	TypeRecord
	// TypeSum is a sealed constructor union.
	TypeSum
)

// Schema describes a Plug service contract.
type Schema struct {
	// Package is the schema namespace and generated Go package name.
	Package string
	// Major is the service compatibility generation.
	Major uint16
	// Family identifies Package and Major independently of additive changes.
	Family        [32]byte
	Documentation string
	// Fingerprint identifies the complete normalized schema generation.
	Fingerprint [32]byte
	// Source is the documented canonical schema returned by inspection.
	Source string
	// Permissions lists declared authorization capabilities.
	Permissions []PermissionInfo
	// Types lists every generated named type.
	Types []TypeInfo
	// Categories group types and methods for documentation.
	Categories []CategoryInfo
	// Methods lists every RPC method in the service contract.
	Methods []MethodInfo
}

// FamilyFingerprint identifies compatible schemas with packageName and major.
func FamilyFingerprint(packageName string, major uint16) [32]byte {
	return sha256.Sum256([]byte(packageName + "\x00" + strconv.FormatUint(uint64(major), 10)))
}

// TypeInfo describes a named schema type.
type TypeInfo struct {
	Name          string
	Documentation string
	Kind          TypeKind
	// Underlying names the primitive used by a newtype.
	Underlying string
	// Error reports whether the type is dedicated to method failures.
	Error bool
	// Constructors lists record or sum constructors.
	Constructors []ConstructorInfo
}

// ConstructorInfo describes a record or sum constructor.
type ConstructorInfo struct {
	Name          string
	Documentation string
	// ID is the constructor's canonical CRC32.
	ID uint32
	// Fields lists positional constructor fields.
	Fields []FieldInfo
	// HTTPStatus is the HTTP response status for this application error.
	HTTPStatus int
}

// TypeExprKind classifies a type expression.
type TypeExprKind uint8

const (
	TypeExprPrimitive TypeExprKind = iota + 1
	TypeExprNamed
	TypeExprList
)

// String returns the kind name used in diagnostics.
func (k TypeExprKind) String() string {
	switch k {
	case TypeExprPrimitive:
		return "primitive"
	case TypeExprNamed:
		return "named"
	case TypeExprList:
		return "list"
	default:
		return fmt.Sprintf("kind(%d)", uint8(k))
	}
}

// TypeExpr is a type expression in runtime schema metadata.
type TypeExpr struct {
	Kind     TypeExprKind
	Name     string
	Element  *TypeExpr
	Optional bool
}

// PrimitiveType returns an expression for the named primitive.
func PrimitiveType(name string) TypeExpr {
	return TypeExpr{
		Kind: TypeExprPrimitive,
		Name: name,
	}
}

// NamedType returns an expression for a schema-defined type.
func NamedType(name string) TypeExpr {
	return TypeExpr{
		Kind: TypeExprNamed,
		Name: name,
	}
}

// ListType returns a list expression with the given element type.
func ListType(element TypeExpr) TypeExpr {
	return TypeExpr{
		Kind:    TypeExprList,
		Element: &element,
	}
}

// OptionalType returns the optional form of expr.
func OptionalType(expr TypeExpr) TypeExpr {
	expr.Optional = true
	return expr
}

// String returns the canonical diagnostic spelling of the expression.
func (t TypeExpr) String() string {
	var b strings.Builder
	if t.Optional {
		b.WriteString("opt ")
	}
	switch t.Kind {
	case TypeExprList:
		b.WriteString("list ")
		if t.Element != nil {
			b.WriteString(t.Element.String())
		}
	default:
		b.WriteString(t.Name)
	}
	return b.String()
}

// FieldInfo describes a request or constructor field.
type FieldInfo struct {
	Name          string
	Documentation string
	Type          TypeExpr
}

// MethodInfo describes an RPC method.
type MethodInfo struct {
	Name          string
	Documentation string
	// ID is the method's canonical CRC32.
	ID uint32
	// Fields lists positional request fields.
	Fields []FieldInfo
	// Response is the structured response type expression.
	Response TypeExpr
	// Error names the declared application-error type, if any.
	Error string
	// Public reports whether anonymous callers may invoke the method.
	Public bool
	// Permission is the capability required to invoke the method.
	Permission string
	HTTP       []HTTPBindingInfo
}

// CategoryInfo groups types and methods for generated documentation. Categories
// do not participate in method IDs, schema fingerprints, or RPC dispatch.
type CategoryInfo struct {
	Name          string
	Documentation string
	// Types contains the names of logical types in this category.
	Types []string
	// Methods contains the names of RPC methods in this category.
	Methods []string
}

// CopySchema returns an independent copy of schema.
func CopySchema(schema *Schema) *Schema {
	if schema == nil {
		return nil
	}

	cp := *schema
	cp.Permissions = slices.Clone(schema.Permissions)
	cp.Categories = slices.Clone(schema.Categories)
	for i := range cp.Categories {
		cp.Categories[i].Types = slices.Clone(cp.Categories[i].Types)
		cp.Categories[i].Methods = slices.Clone(cp.Categories[i].Methods)
	}
	cp.Methods = slices.Clone(schema.Methods)
	for i := range cp.Methods {
		cp.Methods[i].Fields = copyFields(cp.Methods[i].Fields)
		cp.Methods[i].Response = copyTypeExpr(cp.Methods[i].Response)
		cp.Methods[i].HTTP = slices.Clone(cp.Methods[i].HTTP)
	}
	cp.Types = slices.Clone(schema.Types)
	for i := range cp.Types {
		cp.Types[i].Constructors = slices.Clone(cp.Types[i].Constructors)
		for j := range cp.Types[i].Constructors {
			c := &cp.Types[i].Constructors[j]
			c.Fields = copyFields(c.Fields)
		}
	}
	return &cp
}

func copyFields(fields []FieldInfo) []FieldInfo {
	fields = slices.Clone(fields)
	for i := range fields {
		fields[i].Type = copyTypeExpr(fields[i].Type)
	}
	return fields
}

func copyTypeExpr(expr TypeExpr) TypeExpr {
	if expr.Element != nil {
		element := copyTypeExpr(*expr.Element)
		expr.Element = &element
	}
	return expr
}

// ValidateSchema checks schema identity, access rules, and runtime limits.
func ValidateSchema(schema *Schema) error {
	if schema == nil {
		return fmt.Errorf("plug: schema is nil")
	}
	if schema.Package == "" {
		return fmt.Errorf("plug: schema package is empty")
	}
	if schema.Major == 0 {
		return fmt.Errorf("plug: schema major version is zero")
	}
	if expected := FamilyFingerprint(schema.Package, schema.Major); schema.Family != expected {
		return fmt.Errorf("plug: schema family does not match package %s version %d", schema.Package, schema.Major)
	}
	if schema.Source == "" {
		return fmt.Errorf("plug: canonical schema source is empty")
	}
	if !utf8.ValidString(schema.Source) {
		return fmt.Errorf("plug: canonical schema source is not UTF-8")
	}
	if uint64(len(schema.Source)) > uint64(defaultMaxFrameSize) {
		return fmt.Errorf("plug: canonical schema has %d bytes; maximum is %d", len(schema.Source), defaultMaxFrameSize)
	}
	permissions := make(map[string]bool, len(schema.Permissions))
	for _, permission := range schema.Permissions {
		if permission.Name == "" || permissions[permission.Name] {
			return fmt.Errorf("plug: schema contains an empty or duplicate permission %q", permission.Name)
		}
		permissions[permission.Name] = true
	}
	ids := make(map[uint32]string, len(schema.Methods))
	for _, method := range schema.Methods {
		if method.Name == "" {
			return fmt.Errorf("plug: schema contains a method without a name")
		}
		if previous, ok := ids[method.ID]; ok {
			return fmt.Errorf("plug: method ID 0x%08x is shared by %s and %s", method.ID, previous, method.Name)
		}
		if method.Public == (method.Permission != "") {
			return fmt.Errorf("plug: method %s has invalid access metadata", method.Name)
		}
		if method.Permission != "" && !permissions[method.Permission] {
			return fmt.Errorf("plug: method %s requires undeclared permission %s", method.Name, method.Permission)
		}
		if method.Response.Kind == TypeExprPrimitive && method.Response.Name == "raw" {
			if method.Response.Optional || method.Response.Element != nil {
				return fmt.Errorf("plug: method %s has invalid raw response metadata", method.Name)
			}
			if len(method.HTTP) != 0 {
				return fmt.Errorf("plug: raw method %s cannot have an HTTP binding", method.Name)
			}
		}
		ids[method.ID] = method.Name
	}
	return nil
}

// LookupMethod returns the method with id.
func LookupMethod(schema *Schema, id uint32) (MethodInfo, bool) {
	if schema == nil {
		return MethodInfo{}, false
	}
	for _, method := range schema.Methods {
		if method.ID == id {
			return method, true
		}
	}
	return MethodInfo{}, false
}

// LookupConstructor returns the constructor with id.
func LookupConstructor(schema *Schema, id uint32) (ConstructorInfo, bool) {
	if schema == nil {
		return ConstructorInfo{}, false
	}
	for _, typ := range schema.Types {
		for _, constructor := range typ.Constructors {
			if constructor.ID == id {
				return constructor, true
			}
		}
	}
	return ConstructorInfo{}, false
}

// MethodIsRaw reports whether method transfers its connection to the handler.
func MethodIsRaw(method MethodInfo) bool {
	return method.Response.Kind == TypeExprPrimitive && method.Response.Name == "raw" && !method.Response.Optional && method.Response.Element == nil
}

// PermissionInfo describes an authorization capability.
type PermissionInfo struct {
	// Name is the dotted permission identifier.
	Name          string
	Documentation string
}

// HTTPBindingInfo describes an HTTP route declared by a method.
type HTTPBindingInfo struct {
	// Verb is the uppercase HTTP method.
	Verb string
	// Path is the net/http ServeMux path pattern.
	Path string
}

// RemoteSchema describes a peer during inspection or a handshake error.
type RemoteSchema struct {
	// Version is the peer's supported wire version.
	Version uint16
	// Family identifies the peer's compatible package generation.
	Family [32]byte
	// Fingerprint identifies the peer's normalized wire schema.
	Fingerprint [32]byte
	// Source is the peer's documented canonical schema.
	Source string
}
