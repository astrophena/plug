// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/synctest"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/plug"
	"go.astrophena.name/plug/internal/rawtest"
)

func rawServer(t *testing.T, handler func(context.Context, rawtest.ConnectRequest, *plug.RawStream) error, opts plug.ServerOptions) *plug.Server {
	t.Helper()
	s := newServer(t, &rawtest.Schema, opts)
	if err := rawtest.RegisterHandlers(s, rawtest.Handlers{
		Connect: handler,
		Ping:    func(context.Context) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRawStream(t *testing.T) {
	for name, compress := range map[string]bool{"plain": false, "compressed": true} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				message := strings.Repeat("hello", 300)
				s := rawServer(t, func(_ context.Context, req rawtest.ConnectRequest, stream *plug.RawStream) error {
					if req.Message != message {
						return fmt.Errorf("message = %q", req.Message)
					}
					conn, err := stream.Accept()
					if err != nil {
						return err
					}
					var body [4]byte
					if _, err := io.ReadFull(conn, body[:]); err != nil {
						return err
					}
					_, err = conn.Write(append([]byte("echo:"), body[:]...))
					return err
				}, plug.ServerOptions{
					Compression: compress,
				})
				raw, err := rawtest.OpenConnect(t.Context(), serveConn(t, s), rawtest.ConnectRequest{
					Message: message,
				}, plug.ClientOptions{
					Compression: compress,
				})
				if err != nil {
					t.Fatal(err)
				}
				defer raw.Close()
				if _, err := raw.Write([]byte("ping")); err != nil {
					t.Fatal(err)
				}
				response := make([]byte, len("echo:ping"))
				if _, err := io.ReadFull(raw, response); err != nil {
					t.Fatal(err)
				}
				testutil.AssertEqual(t, string(response), "echo:ping")
				if err := raw.CloseWrite(); !errors.Is(err, plug.ErrCloseWriteUnsupported) {
					t.Fatalf("CloseWrite() = %v", err)
				}
			})
		})
	}
}

func TestRawRejection(t *testing.T) {
	for name, budget := range map[string]uint64{"declared error": 0, "decode budget": 1} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := rawServer(t, func(context.Context, rawtest.ConnectRequest, *plug.RawStream) error {
					if budget != 0 {
						t.Error("handler received a request over its decode budget")
					}
					return &rawtest.OpenError{
						Reason: "maintenance",
					}
				}, plug.ServerOptions{
					Limits: plug.Limits{
						MaxDecodeBytes: budget,
					},
				})
				_, err := rawtest.OpenConnect(t.Context(), serveConn(t, s), rawtest.ConnectRequest{
					Message: "too long",
				}, plug.ClientOptions{})
				if budget != 0 {
					checkError(t, err, plug.ErrorBadRequest, "allocation limit")
				} else {
					denied, ok := errors.AsType[*rawtest.OpenError](err)
					if !ok {
						t.Fatalf("OpenConnect() = %v; want OpenError", err)
					}
					testutil.AssertEqual(t, denied.Reason, "maintenance")
				}
			})
		})
	}
}
