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
	// DefaultMaxFrameSize is the default maximum number of bytes after a frame's
	// length prefix.
	DefaultMaxFrameSize uint32 = 16 << 20
	// DefaultMaxListElements is the default list element limit.
	DefaultMaxListElements uint32 = 1 << 20
	// DefaultMaxDecodeBytes bounds allocations while decoding one payload.
	DefaultMaxDecodeBytes uint64 = plugimpl.DefaultMaxDecodeBytes
	// DefaultMaxConcurrentCalls is the default maximum number of active calls per connection.
	DefaultMaxConcurrentCalls uint32 = 100
)

// Limits bounds resource use per connection. Zero fields use defaults.
type Limits struct {
	// MaxFrameSize bounds the bytes following a frame length prefix.
	MaxFrameSize uint32
	// MaxListElements bounds each encoded list and the total list elements
	// decoded from one payload.
	MaxListElements uint32
	// MaxDecodeBytes bounds requested allocation bytes while decoding one
	// payload, including replaced slice storage. Frame buffers are separate.
	MaxDecodeBytes uint64
	// MaxConcurrentCalls bounds active concurrent calls per server connection.
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

// String returns the stage name used in diagnostics.
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

// ErrorEvent describes a failure outside normal method execution.
type ErrorEvent struct {
	Stage     ErrorStage
	Method    MethodInfo
	Principal Principal
	Err       error
}

// ErrorReporter receives runtime errors.
type ErrorReporter func(context.Context, ErrorEvent)

// ServerOptions configures a Plug server. Zero fields select safe defaults.
type ServerOptions struct {
	// Limits optionally overrides the runtime safety defaults.
	Limits Limits
	// Compression accepts zstd-compressed frames.
	Compression bool
	// StrictSchema requires matching schema fingerprints.
	StrictSchema bool
	// Security configures authentication and authorization.
	Security Security
	// ObserveCall is called after every completed handler invocation. The
	// observer must not modify any argument and must not block.
	ObserveCall CallObserver
	// ReportError receives diagnostic runtime error events.
	ReportError ErrorReporter
}

// ClientOptions configures a Plug client. Zero fields select safe defaults.
type ClientOptions struct {
	// Limits optionally overrides the runtime safety defaults.
	Limits Limits
	// Compression requests zstd-compressed frames. Dial retries without
	// compression when the peer does not support it. NewClient cannot retry an
	// existing connection and returns an error instead.
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

// String returns the outcome name used in diagnostics.
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
	// Method is the invoked method's schema metadata.
	Method MethodInfo
	// Principal is the authenticated caller when present.
	Principal Principal
	// HasPrincipal reports whether an authenticated principal was present.
	HasPrincipal bool
	// Duration is the total wall-clock duration of the call lifecycle.
	Duration time.Duration
	// HandlerDuration is the wall-clock handler execution time when entered.
	HandlerDuration time.Duration
	// PayloadSize is the result frame payload size in bytes before compression.
	PayloadSize int
	// Compressed reports whether the result frame payload was compressed.
	Compressed bool
	// CompressedSize is the compressed result frame payload size in bytes. It
	// is zero when Compressed is false.
	CompressedSize int
	// CompressionSavings is the number of result payload bytes saved by
	// compression. It is zero when Compressed is false.
	CompressionSavings int
	// Outcome is the classified call outcome.
	Outcome CallOutcome
	// Err is the diagnostic error, when present.
	Err error
}

// CallObserver receives completed calls.
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
