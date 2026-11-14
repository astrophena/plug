// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"context"
	"fmt"
	"time"

	"go.astrophena.name/plug/plugimpl"
)

// Invoke calls method's handler without encoding or copying values.
// The Go request and response types must match the registered handler.
// Declared errors are returned unchanged.
//
// The handler receives ctx unchanged.
// Derive ctx from [Server.Context] for shutdown cancellation.
func (s *Server) Invoke[Request, Response any](
	ctx context.Context,
	method plugimpl.Method[Request, Response],
	req Request,
) (Response, error) {
	var zero Response

	if s == nil {
		return zero, fmt.Errorf("plug: server is nil")
	}
	if err := method.Validate(s.schema); err != nil {
		return zero, err
	}
	if err := s.Seal(); err != nil {
		return zero, err
	}

	entry := s.handlers[method.ID()]
	handle, ok := entry.typed.(func(context.Context, Request) (Response, error))
	if !ok || handle == nil {
		return zero, fmt.Errorf("plug: method %s has different Go request or response types", entry.info.Name)
	}

	if err := s.addActiveCall(); err != nil {
		return zero, err
	}

	defer s.activeCalls.Done()

	call := s.newCall(ctx, entry.info)
	var (
		resp    Response
		callErr error
	)
	result := invokeSafely(func() callResult {
		// Public calls need no request boxing for the authorizer.
		if !entry.info.Public {
			if result, ok := call.authorize(req); !ok {
				return result
			}
		}
		duration, err, panicErr := call.run(func() error {
			var err error
			resp, err = handle(ctx, req)
			return err
		})
		if panicErr != nil {
			return call.internal(StageHandlerPanic, panicErr, duration, "internal server error")
		}
		if err != nil {
			if result, ok := call.localError(entry.errors, err, duration); ok {
				callErr = err
				return result
			}
			return call.internal(StageHandlerPanic, err, duration, "internal server error")
		}
		return call.succeed(callResult{
			kind: frameResponse,
		}, duration)
	})
	result.observe(s, payloadStats{})
	if result.kind == frameProtocolError {
		return zero, &RemoteError{
			Code:    ErrorCode(result.code),
			Message: string(result.payload),
		}
	}
	if callErr != nil {
		return zero, callErr
	}
	return resp, nil
}

func (c *callState) localError(codec plugimpl.ErrorCodec, err error, duration time.Duration) (callResult, bool) {
	if codec.Match == nil {
		return callResult{}, false
	}
	id, value, ok := codec.Match(err)
	if !ok || value == nil {
		return callResult{}, false
	}

	return c.complete(callResult{
		kind: frameApplicationError,
		code: id,
	}, duration, CallApplicationError, err), true
}
