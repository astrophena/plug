// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"go.astrophena.name/plug/plugimpl"
)

type callState struct {
	server       *Server
	ctx          context.Context
	method       MethodInfo
	principal    Principal
	hasPrincipal bool
	started      time.Time
}

func (s *Server) beginCall(
	ctx context.Context,
	method MethodInfo,
	decode func([]byte, uint32, uint64) (any, error),
	payload []byte,
) (callState, any, callResult, bool) {
	principal, found := PrincipalFromContext(ctx)
	c := callState{
		server:       s,
		ctx:          ctx,
		method:       method,
		principal:    principal,
		hasPrincipal: found,
		started:      time.Now(),
	}

	request, err := decode(payload, s.limits.MaxListElements, s.limits.MaxDecodeBytes)
	if err != nil {
		return c, request, c.reject(
			StageDecodeRequest,
			err,
			ErrorBadRequest,
			fmt.Sprintf("decode %s request: %v", method.Name, err),
			true,
		), false
	}
	if method.Public {
		return c, request, callResult{}, true
	}

	if !c.hasPrincipal {
		err := fmt.Errorf("method requires an authenticated principal")
		return c, request, c.reject(
			StageAuthorize,
			err,
			ErrorUnauthenticated,
			"method requires an authenticated principal",
			false,
		), false
	}
	if err := s.security.Authorize(ctx, c.principal, method.Permission, method, request); err != nil {
		return c, request, c.reject(
			StageAuthorize,
			err,
			ErrorPermissionDenied,
			"permission denied",
			false,
		), false
	}
	return c, request, callResult{}, true
}

func (c *callState) reject(
	stage ErrorStage,
	err error,
	code ErrorCode,
	message string,
	fatal bool,
) callResult {
	c.report(stage, err)
	return c.complete(coreResult(code, message, fatal), 0, CallRejected, err)
}

func (c *callState) applicationError(codec plugimpl.ErrorCodec, err error, duration time.Duration) (callResult, bool) {
	if codec.Encode == nil {
		return callResult{}, false
	}
	id, enc, ok := plugimpl.EncodeError(codec, err, c.server.limits.MaxListElements)
	if !ok {
		return callResult{}, false
	}
	if encodeErr := enc.Err(); encodeErr != nil {
		plugimpl.ReleaseEncoder(enc)
		return c.internal(
			StageEncodeResponse,
			encodeErr,
			duration,
			fmt.Sprintf("encode %s application error: %v", c.method.Name, encodeErr),
		), true
	}
	return c.complete(callResult{
		kind:    frameApplicationError,
		code:    id,
		payload: enc.Data(),
		enc:     enc,
	}, duration, CallApplicationError, err), true
}

func (c *callState) internal(stage ErrorStage, err error, duration time.Duration, message string) callResult {
	c.report(stage, err)
	return c.complete(coreResult(ErrorInternal, message, false), duration, CallInternalError, err)
}

func (c *callState) succeed(result callResult, duration time.Duration) callResult {
	return c.complete(result, duration, CallSucceeded, nil)
}

func (c *callState) report(stage ErrorStage, err error) {
	c.server.safeReportError(c.ctx, ErrorEvent{
		Stage:     stage,
		Method:    c.method,
		Principal: c.principal,
		Err:       err,
	})
}

func (c *callState) complete(result callResult, duration time.Duration, outcome CallOutcome, err error) callResult {
	result.observeCtx = c.ctx
	result.event = CallEvent{
		Method:          c.method,
		Principal:       c.principal,
		HasPrincipal:    c.hasPrincipal,
		Duration:        time.Since(c.started),
		HandlerDuration: duration,
		Outcome:         outcome,
		Err:             err,
	}
	return result
}

func runHandler(handler func() error) (duration time.Duration, err, panicErr error) {
	started := time.Now()
	defer func() {
		duration = time.Since(started)
		if recovered := recover(); recovered != nil {
			panicErr = fmt.Errorf("panic: %v\n%s", recovered, debug.Stack())
		}
	}()
	err = handler()
	return duration, err, panicErr
}
