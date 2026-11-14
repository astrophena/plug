// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import "go.astrophena.name/plug/plugimpl"

// TypeKind classifies a schema type.
type TypeKind = plugimpl.TypeKind

const (
	// TypeNewtype is a distinct scalar type.
	TypeNewtype = plugimpl.TypeNewtype
	// TypeRecord is a positional record.
	TypeRecord = plugimpl.TypeRecord
	// TypeSum is a constructor union.
	TypeSum = plugimpl.TypeSum
)

// Schema describes a Plug service.
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

// TypeExpr describes a schema type expression.
type TypeExpr = plugimpl.TypeExpr

// FieldInfo describes a request or constructor field.
type FieldInfo = plugimpl.FieldInfo

// MethodInfo describes an RPC method.
type MethodInfo = plugimpl.MethodInfo

// CategoryInfo groups schema types and methods.
type CategoryInfo = plugimpl.CategoryInfo

// PermissionInfo describes a permission.
type PermissionInfo = plugimpl.PermissionInfo

// HTTPBindingInfo describes an HTTP route declared by a method.
type HTTPBindingInfo = plugimpl.HTTPBindingInfo

// RemoteSchema describes a peer's schema.
type RemoteSchema = plugimpl.RemoteSchema
