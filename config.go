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

const (
	// DefaultMaxFrameSize is the default frame size limit.
	DefaultMaxFrameSize uint32 = 16 << 20
	// DefaultMaxListElements is the default list element limit.
	DefaultMaxListElements uint32 = 1 << 20
	// DefaultMaxDecodeBytes is the default decoding allocation limit.
	DefaultMaxDecodeBytes uint64 = plugimpl.DefaultMaxDecodeBytes
	// DefaultMaxConcurrentCalls is the default call limit per connection.
	DefaultMaxConcurrentCalls uint32 = 100
)

// Limits bounds resource use for binary calls.
// Zero fields use defaults.
type Limits struct {
	// MaxFrameSize bounds the bytes following a frame length prefix.
	MaxFrameSize uint32

	// MaxListElements limits each encoded list
	// and the total list elements decoded from a payload.
	MaxListElements uint32

	// MaxDecodeBytes limits total requested allocation bytes per decoded payload.
	// Frame buffers are excluded.
	MaxDecodeBytes uint64

	// MaxConcurrentCalls limits active calls per server connection.
	MaxConcurrentCalls uint32
}

// ErrorStage identifies where a runtime error occurred.
type ErrorStage uint8

const (
	StageAuthenticate ErrorStage = iota + 1
	StageHandshake
	StageReadFrame
	StageValidateFrame
	StageUnknownMethod
	StageDecodeRequest
	StageAuthorize
	StageEncodeResponse
	StageWriteFrame
	StageHandlerPanic
)

// String returns the stage name.
func (s ErrorStage) String() string {
	switch s {
	case StageAuthenticate:
		return "authenticate"
	case StageHandshake:
		return "handshake"
	case StageReadFrame:
		return "read_frame"
	case StageValidateFrame:
		return "validate_frame"
	case StageUnknownMethod:
		return "unknown_method"
	case StageDecodeRequest:
		return "decode_request"
	case StageAuthorize:
		return "authorize"
	case StageEncodeResponse:
		return "encode_response"
	case StageWriteFrame:
		return "write_frame"
	case StageHandlerPanic:
		return "handler_panic"
	default:
		return fmt.Sprintf("stage(%d)", uint8(s))
	}
}

// ErrorEvent describes a runtime error.
type ErrorEvent struct {
	Stage     ErrorStage
	Method    MethodInfo
	Principal Principal
	Err       error
}

// ErrorReporter receives runtime errors.
// It may be called concurrently.
type ErrorReporter func(context.Context, ErrorEvent)

// ServerOptions configures a Plug server.
type ServerOptions struct {
	Limits Limits

	// Compression accepts zstd-compressed frames.
	Compression bool

	// StrictSchema requires matching schema fingerprints.
	StrictSchema bool

	Security Security

	ObserveCall CallObserver
	ReportError ErrorReporter
}

// ClientOptions configures a Plug client.
type ClientOptions struct {
	Limits Limits

	// Compression requests zstd-compressed frames.
	// See [DialFunc] for fallback behavior.
	Compression bool

	// StrictSchema requires matching schema fingerprints.
	StrictSchema bool
}

// CallOutcome classifies a completed call.
type CallOutcome uint8

const (
	CallSucceeded CallOutcome = iota + 1
	CallApplicationError
	CallRejected
	CallInternalError
)

// String returns the outcome name.
func (o CallOutcome) String() string {
	switch o {
	case CallSucceeded:
		return "succeeded"
	case CallApplicationError:
		return "application_error"
	case CallRejected:
		return "rejected"
	case CallInternalError:
		return "internal_error"
	default:
		return fmt.Sprintf("outcome(%d)", uint8(o))
	}
}

// CallEvent describes a completed method call.
type CallEvent struct {
	Method    MethodInfo
	Principal Principal
	// HasPrincipal reports whether the caller was authenticated.
	HasPrincipal bool

	// Duration includes decoding, authorization, handling, and response encoding.
	Duration time.Duration
	// HandlerDuration is the time spent in the handler, or zero if it was not called.
	HandlerDuration time.Duration

	// PayloadSize is the uncompressed response payload size in bytes.
	// It is zero for local calls.
	PayloadSize int
	// Compressed reports whether the response payload was compressed.
	Compressed bool
	// CompressedSize is the compressed size in bytes, or zero if uncompressed.
	CompressedSize int
	// CompressionSavings counts bytes saved by compression.
	CompressionSavings int

	Outcome CallOutcome
	// Err is the diagnostic error, if any.
	Err error
}

// CallObserver receives completed calls, including decode and authorization failures.
// Callbacks may run concurrently and must not block or modify their arguments.
type CallObserver func(context.Context, CallEvent)

func normalizeSchemaLimits(schema *Schema, limits Limits) (*Schema, Limits, error) {
	if err := plugimpl.ValidateSchema(schema); err != nil {
		return nil, Limits{}, err
	}
	if limits.MaxFrameSize == 0 {
		limits.MaxFrameSize = DefaultMaxFrameSize
	}
	if limits.MaxListElements == 0 {
		limits.MaxListElements = DefaultMaxListElements
	}
	if limits.MaxDecodeBytes == 0 {
		limits.MaxDecodeBytes = DefaultMaxDecodeBytes
	}
	if limits.MaxConcurrentCalls == 0 {
		limits.MaxConcurrentCalls = DefaultMaxConcurrentCalls
	}
	if limits.MaxFrameSize < minimumRequestFrameSize {
		return nil, Limits{}, fmt.Errorf("plug: maximum frame size %d is smaller than request header size %d", limits.MaxFrameSize, minimumRequestFrameSize)
	}
	return plugimpl.CopySchema(schema), limits, nil
}
