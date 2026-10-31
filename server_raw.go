// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"bufio"
	"context"
	"fmt"
	"net"

	"go.astrophena.name/plug/plugimpl"
)

func (s *Server) serveRawCall(
	ctx context.Context,
	conn net.Conn,
	r *bufio.Reader,
	f *framer,
	req frame,
	entry handlerEntry,
) error {
	callCtx, cancel, err := s.trackConnectionCall(ctx)
	if err != nil {
		return f.writeProtocolError(req.requestID, ErrorInternal, "server is shutting down")
	}
	defer s.finishConnectionCall(cancel)

	raw := newRawConn(callCtx, conn, r)
	stream := &RawStream{
		conn: raw,
		accept: func() error {
			err := f.writeResponse(req.requestID, nil)
			if err != nil {
				s.safeReportError(callCtx, ErrorEvent{
					Stage:  StageWriteFrame,
					Method: entry.info,
					Err:    err,
				})
			}
			return err
		},
	}
	result := invokeSafely(func() callResult {
		return entry.invokeRaw(callCtx, req.payload, stream)
	})
	defer result.release()

	if stream.isAccepted() {
		result.observe(s, payloadStats{})
		return nil
	}
	stats, err := f.writeResult(req.requestID, result.kind, result.code, result.payload)
	result.observe(s, stats)
	if err != nil {
		s.safeReportError(callCtx, ErrorEvent{
			Stage:  StageWriteFrame,
			Method: entry.info,
			Err:    err,
		})
	}
	return err
}

func newRawInvoke(
	s *Server,
	info MethodInfo,
	reg plugimpl.Registration,
) func(context.Context, []byte, *RawStream) callResult {
	return func(ctx context.Context, payload []byte, stream *RawStream) callResult {
		call, request, result, ok := s.beginCall(ctx, info, reg.Decode, payload)
		if !ok {
			return result
		}
		duration, callErr, panicErr := runHandler(func() error {
			return reg.HandleRaw(ctx, request, stream)
		})
		if panicErr != nil {
			return call.internal(StageHandlerPanic, panicErr, duration, "internal server error")
		}
		if callErr != nil && !stream.isAccepted() {
			if result, ok := call.applicationError(reg.Errors, callErr, duration); ok {
				return result
			}
		}
		if callErr == nil && !stream.isAccepted() {
			callErr = fmt.Errorf("raw handler returned before accepting the connection")
		}
		if callErr != nil {
			return call.internal(StageHandlerPanic, callErr, duration, "internal server error")
		}
		return call.succeed(callResult{}, duration)
	}
}
