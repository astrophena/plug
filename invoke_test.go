// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/plug/plugimpl"
)

func TestInvoke(t *testing.T) {
	var (
		sch   = echoSchema("bytes")
		codec = plugimpl.Codec[[]byte]{
			Encode: func(*plugimpl.Encoder, []byte) { panic("encoded a local value") },
			Decode: func(*plugimpl.Decoder) []byte { panic("decoded a local value") },
		}
		m = plugimpl.NewMethod(sch, 1, codec, codec, plugimpl.ErrorCodec{})
		s = newServer(t, sch, ServerOptions{
			Limits: Limits{
				MaxDecodeBytes: 1,
			},
		})
		calls int
		regs  []plugimpl.Registration
	)
	if err := plugimpl.AppendHandler(&regs, m, func(_ context.Context, v []byte) ([]byte, error) {
		calls++
		v[0] = 42
		return v, nil
	}, "Echo"); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterHandlers(regs); err != nil {
		t.Fatal(err)
	}

	req := make([]byte, 256<<10)
	got, err := s.Invoke(t.Context(), m, req)
	if err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, len(got), len(req))
	if &got[0] != &req[0] {
		t.Fatal("Invoke copied the request or response")
	}
	testutil.AssertEqual(t, req[0], byte(42))

	wrong := plugimpl.NewMethod(sch, 1, plugimpl.I64Codec, codec, plugimpl.ErrorCodec{})
	if _, err := s.Invoke(t.Context(), wrong, 1); err == nil {
		t.Fatal("invoked with mismatched Go types")
	}
	testutil.AssertEqual(t, calls, 1)
}

func TestInvokeError(t *testing.T) {
	var (
		sch   = echoSchema("u32")
		cause = errors.New("lookup failed")
		want  = fmt.Errorf("lookup: %w", cause)
		event CallEvent
	)
	sch.Methods[0].Error = "Failure"
	sch.Types = []TypeInfo{
		{
			Name:  "Failure",
			Kind:  TypeRecord,
			Error: true,
			Constructors: []ConstructorInfo{
				{
					Name: "failed",
					ID:   1,
				},
			},
		},
	}
	var (
		m = plugimpl.NewMethod(sch, 1, plugimpl.U32Codec, plugimpl.U32Codec, plugimpl.ErrorCodec{
			Match: func(err error) (uint32, error, bool) { return 1, err, errors.Is(err, cause) },
		})
		s = newServer(t, sch, ServerOptions{
			ObserveCall: func(_ context.Context, e CallEvent) { event = e },
		})
		regs []plugimpl.Registration
	)
	if err := plugimpl.AppendHandler(&regs, m, func(context.Context, uint32) (uint32, error) { return 0, want }, "Echo"); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterHandlers(regs); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Invoke(t.Context(), m, 42); err != want {
		t.Fatalf("Invoke() = %v; want original error", err)
	}

	testutil.AssertEqual(t, event.Outcome, CallApplicationError)
	if event.Err != want {
		t.Fatalf("observed error = %v; want original error", event.Err)
	}
	testutil.AssertEqual(t, event.PayloadSize, 0)
}
