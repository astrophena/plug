// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plugimpl

import (
	"context"
	"fmt"
)

// Registration describes a generated handler in an atomic batch.
type Registration struct {
	ID        uint32
	Schema    *Schema
	Raw       bool
	Decode    func([]byte, uint32, uint64) (any, error)
	Handle    func(context.Context, any) (any, error)
	HandleRaw func(context.Context, any, any) error
	Encode    func(any, uint32) (*Encoder, error)
	Errors    ErrorCodec
}

// AppendHandler appends a typed handler to batch.
func AppendHandler[Request, Response any](
	batch *[]Registration,
	method Method[Request, Response],
	handler func(context.Context, Request) (Response, error),
	fieldName string,
) error {
	if batch == nil {
		return fmt.Errorf("plug: handler registration batch is nil")
	}
	if handler == nil {
		return fmt.Errorf("plug: handler for method %s (%s) is nil", method.Info().Name, fieldName)
	}
	*batch = append(*batch, Registration{
		ID:     method.ID(),
		Schema: method.Schema(),
		Decode: func(payload []byte, maxList uint32, maxBytes uint64) (any, error) {
			return Decode(method.Request(), payload, maxList, maxBytes)
		},
		Handle: func(ctx context.Context, request any) (any, error) {
			return handler(ctx, request.(Request))
		},
		Encode: func(response any, maxList uint32) (*Encoder, error) {
			return EncodePooled(method.Response(), response.(Response), maxList)
		},
		Errors: method.Errors(),
	})
	return nil
}

// AppendHandlerNoReq appends a handler with no request argument to batch.
func AppendHandlerNoReq[Request, Response any](
	batch *[]Registration,
	method Method[Request, Response],
	handler func(context.Context) (Response, error),
	fieldName string,
) error {
	if handler == nil {
		return fmt.Errorf("plug: handler for method %s (%s) is nil", method.Info().Name, fieldName)
	}
	return AppendHandler(batch, method, func(ctx context.Context, _ Request) (Response, error) {
		return handler(ctx)
	}, fieldName)
}

// AppendHandlerVoid appends a handler with no response value to batch.
func AppendHandlerVoid[Request any](
	batch *[]Registration,
	method Method[Request, Void],
	handler func(context.Context, Request) error,
	fieldName string,
) error {
	if handler == nil {
		return fmt.Errorf("plug: handler for method %s (%s) is nil", method.Info().Name, fieldName)
	}
	return AppendHandler(batch, method, func(ctx context.Context, request Request) (Void, error) {
		return Void{}, handler(ctx, request)
	}, fieldName)
}

// AppendHandlerNoReqVoid appends a handler with no request or response value to batch.
func AppendHandlerNoReqVoid[Request any](
	batch *[]Registration,
	method Method[Request, Void],
	handler func(context.Context) error,
	fieldName string,
) error {
	if handler == nil {
		return fmt.Errorf("plug: handler for method %s (%s) is nil", method.Info().Name, fieldName)
	}
	return AppendHandler(batch, method, func(ctx context.Context, _ Request) (Void, error) {
		return Void{}, handler(ctx)
	}, fieldName)
}

// AppendRawHandler appends a raw handler to batch.
func AppendRawHandler[Request, Stream any](
	batch *[]Registration,
	method RawMethod[Request],
	handler func(context.Context, Request, Stream) error,
	fieldName string,
) error {
	if batch == nil {
		return fmt.Errorf("plug: handler registration batch is nil")
	}
	if handler == nil {
		return fmt.Errorf("plug: handler for method %s (%s) is nil", method.Info().Name, fieldName)
	}
	*batch = append(*batch, Registration{
		ID:     method.ID(),
		Schema: method.Schema(),
		Raw:    true,
		Decode: func(payload []byte, maxList uint32, maxBytes uint64) (any, error) {
			return Decode(method.Request(), payload, maxList, maxBytes)
		},
		HandleRaw: func(ctx context.Context, request, stream any) error {
			return handler(ctx, request.(Request), stream.(Stream))
		},
		Errors: method.Errors(),
	})
	return nil
}

// AppendRawHandlerNoReq appends a raw handler with no request argument to batch.
func AppendRawHandlerNoReq[Request, Stream any](
	batch *[]Registration,
	method RawMethod[Request],
	handler func(context.Context, Stream) error,
	fieldName string,
) error {
	if handler == nil {
		return fmt.Errorf("plug: handler for method %s (%s) is nil", method.Info().Name, fieldName)
	}
	return AppendRawHandler(batch, method, func(ctx context.Context, _ Request, stream Stream) error {
		return handler(ctx, stream)
	}, fieldName)
}
