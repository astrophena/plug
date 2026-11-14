// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"testing/synctest"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/plug"
	"go.astrophena.name/plug/example/greeter"
)

func TestClientValues(t *testing.T) {
	for name, want := range map[string]greeter.User{
		"record": {
			ID:     42,
			Name:   "Ada",
			Email:  plug.Some("ada@example.test"),
			Labels: []string{"test", "plug"},
			Role: &greeter.Admin{
				Level: 7,
			},
		},
		"unknown constructor": {
			Labels: []string{},
			Role: &greeter.UnknownRole{
				ConstructorID: 0xfefefefe,
				Payload:       []byte{0, 1, 2},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var (
					h  = handlers()
					in greeter.GetUserRequest
				)
				h.GetUser = func(_ context.Context, req greeter.GetUserRequest) (greeter.User, error) {
					in = req
					return want, nil
				}
				s := newGreeter(t, h, plug.ServerOptions{
					Security: security(),
				})
				c := greeter.NewClient(connect(t, s, plug.ClientOptions{}))

				got, err := c.GetUser(t.Context(), greeter.GetUserRequest{
					ID: 42,
				})
				if err != nil {
					t.Fatal(err)
				}
				testutil.AssertEqual(t, in.ID, greeter.UserID(42))
				testutil.AssertEqual(t, got, want)
			})
		})
	}
}

func TestClientErrors(t *testing.T) {
	var (
		notFound = &greeter.NotFound{
			ID: 42,
		}
		unknown = &greeter.UnknownLookupError{
			ConstructorID: 0xdeadbeef,
			Payload:       []byte("opaque"),
		}
	)
	for name, tc := range map[string]struct {
		err    error
		want   error
		panics bool
	}{
		"declared": {
			err:  fmt.Errorf("lookup: %w", notFound),
			want: notFound,
		},
		"unknown": {
			err:  unknown,
			want: unknown,
		},
		"internal": {
			err: errors.New("private database failure"),
		},
		"panic": {
			panics: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				h := handlers()
				h.GetUser = func(context.Context, greeter.GetUserRequest) (greeter.User, error) {
					if tc.panics {
						panic("private handler failure")
					}
					return greeter.User{}, tc.err
				}
				s := newGreeter(t, h, plug.ServerOptions{
					Security: security(),
				})
				c := greeter.NewClient(connect(t, s, plug.ClientOptions{}))

				_, err := c.GetUser(t.Context(), greeter.GetUserRequest{})
				if tc.want != nil {
					testutil.AssertEqual(t, err, tc.want)
				} else {
					checkError(t, err, plug.ErrorInternal, "internal server error")
				}
				if err := c.Ping(t.Context()); err != nil {
					t.Fatalf("connection after failed call: %v", err)
				}
			})
		})
	}
}

func TestClientDecodeBudget(t *testing.T) {
	for name, tc := range map[string]struct {
		user   greeter.User
		err    error
		budget uint64
	}{
		"variant": {
			user: greeter.User{
				Role: &greeter.Admin{
					Level: 7,
				},
			},
			budget: 1,
		},
		"unknown variant": {
			user: greeter.User{
				Role: &greeter.UnknownRole{
					ConstructorID: 99,
					Payload:       make([]byte, 64),
				},
			},
			budget: 64,
		},
		"error": {
			err: &greeter.NotFound{
				ID: 7,
			},
			budget: 1,
		},
		"unknown error": {
			err: &greeter.UnknownLookupError{
				ConstructorID: 99,
				Payload:       make([]byte, 64),
			},
			budget: 64,
		},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				h := handlers()
				h.GetUser = func(context.Context, greeter.GetUserRequest) (greeter.User, error) { return tc.user, tc.err }
				s := newGreeter(t, h, plug.ServerOptions{
					Security: security(),
				})
				c := greeter.NewClient(connect(t, s, plug.ClientOptions{
					Limits: plug.Limits{
						MaxDecodeBytes: tc.budget,
					},
				}))
				_, err := c.GetUser(t.Context(), greeter.GetUserRequest{})
				if err == nil || !strings.Contains(err.Error(), "allocation limit") {
					t.Fatalf("GetUser() = %v; want allocation limit", err)
				}
			})
		})
	}
}

func BenchmarkRoundTrip(b *testing.B) {
	s := newGreeter(b, handlers(), plug.ServerOptions{
		Security: security(),
	})
	c := greeter.NewClient(connect(b, s, plug.ClientOptions{}))
	ctx := b.Context()
	req := greeter.GetUserRequest{
		ID: 42,
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := c.GetUser(ctx, req); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPingRoundTrip(b *testing.B) {
	s := newGreeter(b, handlers(), plug.ServerOptions{
		Security: security(),
	})
	c := greeter.NewClient(connect(b, s, plug.ClientOptions{}))
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		if err := c.Ping(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParallelRoundTrip(b *testing.B) {
	s := newGreeter(b, handlers(), plug.ServerOptions{
		Security: security(),
	})
	c := greeter.NewClient(connect(b, s, plug.ClientOptions{}))
	ctx := b.Context()
	req := greeter.GetUserRequest{
		ID: 42,
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := c.GetUser(ctx, req); err != nil {
				b.Error(err)
				return
			}
		}
	})
}

func BenchmarkCompressedRPC(b *testing.B) {
	var (
		h   = handlers()
		msg = strings.Repeat("compressible ", 200)
	)
	h.GetUser = func(_ context.Context, req greeter.GetUserRequest) (greeter.User, error) {
		return greeter.User{
			ID:   req.ID,
			Name: msg,
			Role: &greeter.Guest{},
		}, nil
	}
	s := newGreeter(b, h, plug.ServerOptions{
		Security:    security(),
		Compression: true,
	})
	c := greeter.NewClient(connect(b, s, plug.ClientOptions{
		Compression: true,
	}))
	ctx := b.Context()
	req := greeter.GetUserRequest{
		ID: 42,
	}
	// Initialize codecs before measuring steady-state calls.
	if _, err := c.GetUser(ctx, req); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := c.GetUser(ctx, req); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompressedConnection(b *testing.B) {
	var (
		h   = handlers()
		msg = strings.Repeat("compressible ", 200)
	)
	h.GetUser = func(context.Context, greeter.GetUserRequest) (greeter.User, error) {
		return greeter.User{
			Name: msg,
			Role: &greeter.Guest{},
		}, nil
	}
	s := newGreeter(b, h, plug.ServerOptions{
		Security:    security(),
		Compression: true,
	})
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		conn, peer := net.Pipe()
		done := make(chan error, 1)
		go func() { done <- s.ServeConn(ctx, conn) }()
		c, err := plug.NewClient(ctx, peer, &greeter.Schema, plug.ClientOptions{
			Compression: true,
		})
		if err != nil {
			peer.Close()
			b.Fatal(err)
		}
		_, callErr := greeter.NewClient(c).GetUser(ctx, greeter.GetUserRequest{})
		closeErr := c.Close()
		serveErr := <-done
		if callErr != nil {
			b.Fatal(callErr)
		}
		if closeErr != nil {
			b.Fatal(closeErr)
		}
		if serveErr != nil {
			b.Fatal(serveErr)
		}
	}
}

func checkError(t *testing.T, err error, code plug.ErrorCode, msg string) {
	t.Helper()
	e, ok := errors.AsType[*plug.RemoteError](err)
	if !ok {
		t.Fatalf("error = %v; want RemoteError", err)
	}
	testutil.AssertEqual(t, e.Code, code)
	if !strings.Contains(e.Message, msg) {
		t.Fatalf("message = %q; want %q", e.Message, msg)
	}
}

func handlers() greeter.Handlers {
	return greeter.Handlers{
		GetUser: func(_ context.Context, req greeter.GetUserRequest) (greeter.User, error) {
			return greeter.User{
				ID:     req.ID,
				Name:   "bench-user",
				Email:  plug.Some("bench@example.test"),
				Labels: []string{"benchmark"},
				Role:   &greeter.Guest{},
			}, nil
		},
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) { return nil, nil },
		Ping:      func(context.Context) error { return nil },
	}
}

func security() plug.Security {
	return plug.Security{
		Authenticate: func(context.Context, net.Conn) (plug.Principal, error) {
			return plug.Principal{
				Subject: "test",
			}, nil
		},
		Authorize: func(context.Context, plug.Principal, string, plug.MethodInfo, any) error { return nil },
	}
}

func newGreeter(t testing.TB, h greeter.Handlers, opts plug.ServerOptions) *plug.Server {
	t.Helper()
	s := newServer(t, &greeter.Schema, opts)
	if err := greeter.RegisterHandlers(s, h); err != nil {
		t.Fatal(err)
	}
	return s
}

func newServer(t testing.TB, sch *plug.Schema, opts plug.ServerOptions) *plug.Server {
	t.Helper()
	s, err := plug.NewServer(sch, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return s
}

func serveConn(t testing.TB, s *plug.Server) net.Conn {
	t.Helper()
	var (
		conn, peer = net.Pipe()
		done       = make(chan error, 1)
	)
	t.Cleanup(func() {
		peer.Close()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	go func() { done <- s.ServeConn(t.Context(), conn) }()
	return peer
}

func connect(t testing.TB, s *plug.Server, opts plug.ClientOptions) *plug.Client {
	t.Helper()
	c, err := plug.NewClient(t.Context(), serveConn(t, s), s.Schema(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	return c
}
