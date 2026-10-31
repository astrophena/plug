// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import "go.astrophena.name/plug/plugimpl"

// TypeKind classifies a generated schema type.
type TypeKind = plugimpl.TypeKind

const (
	// TypeNewtype is a distinct scalar type.
	TypeNewtype = plugimpl.TypeNewtype
	// TypeRecord is a concrete positional record.
	TypeRecord = plugimpl.TypeRecord
	// TypeSum is a sealed constructor union.
	TypeSum = plugimpl.TypeSum
)

// Schema describes a Plug service contract.
type Schema = plugimpl.Schema

// TypeInfo describes a named schema type.
type TypeInfo = plugimpl.TypeInfo

// ConstructorInfo describes a record or sum constructor.
type ConstructorInfo = plugimpl.ConstructorInfo

// TypeExprKind classifies a type expression.
type TypeExprKind = plugimpl.TypeExprKind

const (
	TypeExprPrimitive = plugimpl.TypeExprPrimitive
	TypeExprNamed     = plugimpl.TypeExprNamed
	TypeExprList      = plugimpl.TypeExprList
)

// TypeExpr is a type expression in runtime schema metadata.
type TypeExpr = plugimpl.TypeExpr

// FieldInfo describes a request or constructor field.
type FieldInfo = plugimpl.FieldInfo

// MethodInfo describes an RPC method.
type MethodInfo = plugimpl.MethodInfo

// CategoryInfo groups types and methods for generated documentation.
type CategoryInfo = plugimpl.CategoryInfo

// PermissionInfo describes an authorization capability.
type PermissionInfo = plugimpl.PermissionInfo

// HTTPBindingInfo describes an HTTP route declared by a method.
type HTTPBindingInfo = plugimpl.HTTPBindingInfo

// RemoteSchema describes a peer during inspection or a handshake error.
type RemoteSchema = plugimpl.RemoteSchema
