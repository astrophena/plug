// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plugimpl

import "fmt"

// Method describes a typed RPC method.
type Method[Request, Response any] struct {
	schema   *Schema
	id       uint32
	request  Codec[Request]
	response Codec[Response]
	errors   ErrorCodec
}

// RawMethod describes a method that takes over a dedicated connection.
type RawMethod[Request any] struct {
	schema  *Schema
	id      uint32
	request Codec[Request]
	errors  ErrorCodec
}

// NewMethod returns a method descriptor for generated bindings.
func NewMethod[Request, Response any](
	schema *Schema,
	id uint32,
	request Codec[Request],
	response Codec[Response],
	errors ErrorCodec,
) Method[Request, Response] {
	return Method[Request, Response]{
		schema:   schema,
		id:       id,
		request:  request,
		response: response,
		errors:   errors,
	}
}

// NewRawMethod returns a raw method descriptor for generated bindings.
func NewRawMethod[Request any](
	schema *Schema,
	id uint32,
	request Codec[Request],
	errors ErrorCodec,
) RawMethod[Request] {
	return RawMethod[Request]{
		schema:  schema,
		id:      id,
		request: request,
		errors:  errors,
	}
}

// Info returns the schema metadata for m.
func (m Method[Request, Response]) Info() MethodInfo { return methodInfo(m.schema, m.id) }

// Schema returns the generated schema that owns m.
func (m Method[Request, Response]) Schema() *Schema { return m.schema }

// Info returns the schema metadata for m.
func (m RawMethod[Request]) Info() MethodInfo { return methodInfo(m.schema, m.id) }

// Schema returns the generated schema that owns m.
func (m RawMethod[Request]) Schema() *Schema { return m.schema }

// Validate checks that m belongs to schema and has the expected response kind.
func (m Method[Request, Response]) Validate(schema *Schema) error {
	return ValidateMethod(m.schema, schema, m.id, false)
}

// Validate checks that m belongs to schema and is raw.
func (m RawMethod[Request]) Validate(schema *Schema) error {
	return ValidateMethod(m.schema, schema, m.id, true)
}

// ID returns the method's wire ID.
func (m Method[Request, Response]) ID() uint32 { return m.id }

// Request returns the method's request codec.
func (m Method[Request, Response]) Request() Codec[Request] { return m.request }

// Response returns the method's response codec.
func (m Method[Request, Response]) Response() Codec[Response] { return m.response }

// Errors returns the method's application-error codec.
func (m Method[Request, Response]) Errors() ErrorCodec { return m.errors }

// ID returns the method's wire ID.
func (m RawMethod[Request]) ID() uint32 { return m.id }

// Request returns the method's request codec.
func (m RawMethod[Request]) Request() Codec[Request] { return m.request }

// Errors returns the method's application-error codec.
func (m RawMethod[Request]) Errors() ErrorCodec { return m.errors }

func methodInfo(schema *Schema, id uint32) MethodInfo {
	if method, ok := LookupMethod(schema, id); ok {
		return method
	}
	return MethodInfo{
		ID: id,
	}
}

// ValidateMethod checks that a generated method belongs to schema.
func ValidateMethod(owner, connection *Schema, id uint32, raw bool) error {
	if owner == nil || connection == nil {
		return fmt.Errorf("plug: method or connection schema is nil")
	}
	if owner.Family != connection.Family {
		return fmt.Errorf("plug: method 0x%08x belongs to a different schema family", id)
	}
	info, ok := LookupMethod(connection, id)
	if !ok {
		return fmt.Errorf("plug: method 0x%08x is absent from schema", id)
	}
	if raw == MethodIsRaw(info) {
		return nil
	}
	if raw {
		return fmt.Errorf("plug: method %s is not raw", info.Name)
	}
	return fmt.Errorf("plug: method %s is raw", info.Name)
}

// ErrorInfo returns schema metadata for a generated declared error.
func (m Method[Request, Response]) ErrorInfo(err error) (ConstructorInfo, bool) {
	if m.Errors().Identify == nil || m.Schema() == nil {
		return ConstructorInfo{}, false
	}
	id, ok := m.Errors().Identify(err)
	if !ok {
		return ConstructorInfo{}, false
	}
	return LookupConstructor(m.Schema(), id)
}
