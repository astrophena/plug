// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"context"
	"fmt"
	"unicode/utf8"

	"go.astrophena.name/plug/plugimpl"
)

// Call invokes method through c. A malformed response closes c.
func Call[Request, Response any](
	ctx context.Context,
	c *Client,
	method plugimpl.Method[Request, Response],
	req Request,
) (Response, error) {
	var zero Response

	if c == nil {
		return zero, fmt.Errorf("plug: client is nil")
	}
	if err := method.Validate(c.schema); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}

	maxList := c.limits.MaxListElements
	enc, err := plugimpl.EncodePooled(method.Request(), req, maxList)
	if err != nil {
		return zero, fmt.Errorf("plug: encode request for %s: %w", method.Info().Name, err)
	}

	result, err := c.call(ctx, method.ID(), enc)
	if err != nil {
		return zero, err
	}

	response, err, protocolError := decodeMethodResult(method, result, c.limits, false)
	if protocolError {
		c.shutdown(err)
	}
	return response, err
}

// Invoke calls method on s without a network round trip.
// Server shutdown cancels the call's context and waits for it to finish.
func Invoke[Request, Response any](
	ctx context.Context,
	s *Server,
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
	if err := s.addActiveCall(); err != nil {
		return zero, err
	}

	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.serverCtx, cancel)
	defer func() {
		stop()
		cancel()
		s.activeCalls.Done()
	}()

	maxList := s.limits.MaxListElements
	payload, err := plugimpl.Encode(method.Request(), req, maxList)
	if err != nil {
		return zero, fmt.Errorf("plug: encode local request for %s: %w", method.Info().Name, err)
	}

	result := s.invoke(ctx, method.ID(), payload)
	response, err, _ := decodeMethodResult(method, result, s.limits, true)
	return response, err
}

func decodeMethodResult[Request, Response any](
	method plugimpl.Method[Request, Response],
	result frame,
	limits Limits,
	isLocal bool,
) (Response, error, bool) {
	var zero Response
	prefix := ""
	if isLocal {
		prefix = "local "
	}
	if result.kind == frameResponse {
		decoded, err := plugimpl.Decode(method.Response(), result.payload, limits.MaxListElements, limits.MaxDecodeBytes)
		if err != nil {
			return zero, fmt.Errorf("plug: decode %sresponse for %s: %w", prefix, method.Info().Name, err), true
		}
		return decoded, nil, false
	}
	err, protocolError := decodeResultError(method.Info(), method.Errors(), result, limits, prefix, prefix+"response")
	return zero, err, protocolError
}

func decodeResultError(
	method MethodInfo,
	errorsCodec plugimpl.ErrorCodec,
	result frame,
	limits Limits,
	prefix string,
	responseName string,
) (error, bool) {
	switch result.kind {
	case frameApplicationError:
		if errorsCodec.Decode == nil {
			return fmt.Errorf("plug: method %s returned an undeclared application error", method.Name), true
		}
		appErr, err := plugimpl.DecodeError(errorsCodec, result.payload, result.code, limits.MaxListElements, limits.MaxDecodeBytes)
		if err != nil {
			return fmt.Errorf("plug: decode %sapplication error for %s: %w", prefix, method.Name, err), true
		}
		if appErr == nil {
			return fmt.Errorf("plug: application error decoder returned nil"), true
		}
		return appErr, false
	case frameProtocolError:
		if !utf8.Valid(result.payload) {
			return fmt.Errorf("plug: %sprotocol error message is not UTF-8", prefix), true
		}
		errCode := ErrorCode(result.code)
		if errCode < ErrorBadRequest || errCode > ErrorUnimplemented {
			return fmt.Errorf("plug: unknown core error code %d", errCode), true
		}
		return &RemoteError{
			Code:    errCode,
			Message: string(result.payload),
		}, false
	default:
		return fmt.Errorf("plug: unexpected %s result kind %d", responseName, result.kind), true
	}
}
