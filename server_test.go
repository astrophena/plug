// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/plug/plugimpl"
)

func TestServerCallbacks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			calls = make(chan CallEvent, 1)
			errs  = make(chan ErrorEvent, 1)
			fail  = true
		)
		s, m := echoServer(t, func(_ context.Context, v string) (string, error) {
			if fail {
				time.Sleep(time.Millisecond) // Give durations a known value in virtual time.
				return "", errors.New("database failed")
			}
			return v, nil
		}, ServerOptions{
			ObserveCall: func(_ context.Context, e CallEvent) {
				calls <- e
				panic("observer failed")
			},
			ReportError: func(_ context.Context, e ErrorEvent) {
				errs <- e
				panic("reporter failed")
			},
		})
		c := connect(t, s, ClientOptions{})

		_, err := c.Call(t.Context(), m, "fail")
		checkError(t, err, ErrorInternal, "internal server error")

		var (
			call   = <-calls
			report = <-errs
		)
		testutil.AssertEqual(t, call.Outcome, CallInternalError)
		testutil.AssertEqual(t, call.HandlerDuration, time.Millisecond)
		testutil.AssertEqual(t, call.Duration, time.Millisecond)
		if report.Err == nil || call.Err == nil {
			t.Fatal("failed call has no diagnostic error")
		}

		fail = false
		if _, err := c.Call(t.Context(), m, "ok"); err != nil {
			t.Fatalf("call after callback panic: %v", err)
		}
		testutil.AssertEqual(t, (<-calls).Outcome, CallSucceeded)
	})
}

func TestServerRegistration(t *testing.T) {
	var (
		sch  = echoSchema("string")
		s    = newServer(t, sch, ServerOptions{})
		m    = plugimpl.NewMethod(sch, 1, plugimpl.StringCodec, plugimpl.StringCodec, plugimpl.ErrorCodec{})
		regs []plugimpl.Registration
	)
	if err := plugimpl.AppendHandler(&regs, m, func(_ context.Context, v string) (string, error) { return v, nil }, "Echo"); err != nil {
		t.Fatal(err)
	}

	if err := s.RegisterHandlers(append(regs, regs[0])); err == nil {
		t.Fatal("registered duplicate handlers in a batch")
	}
	if err := s.Seal(); err == nil || !strings.Contains(err.Error(), "test.echo") {
		t.Fatalf("Seal() = %v; want missing echo handler", err)
	}

	if err := s.RegisterHandlers(regs); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterHandlers(regs); err == nil {
		t.Fatal("registered duplicate handlers")
	}
	for range 2 {
		if err := s.Seal(); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RegisterHandlers(regs); err == nil {
		t.Fatal("registered after sealing")
	}
}

func TestServerOwnsSchema(t *testing.T) {
	sch := echoSchema("string")
	sch.Categories = []CategoryInfo{
		{
			Name:    "Methods",
			Methods: []string{"test.echo"},
		},
	}
	sch.Methods[0].Response = plugimpl.ListType(plugimpl.PrimitiveType("string"))
	sch.Methods[0].HTTP = []HTTPBindingInfo{
		{
			Verb: "GET",
			Path: "/echo",
		},
	}
	var (
		want = plugimpl.CopySchema(sch)
		s    = newServer(t, sch, ServerOptions{})
	)

	for _, view := range []*Schema{sch, s.Schema()} {
		view.Categories[0].Methods[0] = "changed category"
		view.Methods[0].Response.Element.Name = "changed response"
		view.Methods[0].Fields[0].Name = "changed field"
		view.Methods[0].HTTP[0].Path = "/changed"

		testutil.AssertEqual(t, s.Schema(), want)
	}
}

func TestServerShutdown(t *testing.T) {
	for name, wait := range map[string]bool{"cooperative": false, "ignores cancellation": true} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var (
					release = make(chan struct{})
					unblock = sync.OnceFunc(func() { close(release) })
					started bool
				)
				defer unblock()

				s, m := echoServer(t, func(ctx context.Context, v string) (string, error) {
					started = true
					if wait {
						<-release
					} else {
						<-ctx.Done()
					}
					return v, nil
				}, ServerOptions{})
				var (
					c    = connect(t, s, ClientOptions{})
					done = make(chan error, 1)
				)
				go func() {
					_, err := c.Call(t.Context(), m, "")
					done <- err
				}()
				synctest.Wait()
				testutil.AssertEqual(t, started, true)

				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				err := s.Shutdown(ctx)
				if wait {
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("Shutdown() = %v; want deadline", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if !errors.Is(s.Context().Err(), context.Canceled) {
					t.Fatal("server context was not canceled")
				}

				unblock()
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if err := s.Shutdown(t.Context()); err != nil {
					t.Fatal(err)
				}

				_, err = c.Call(t.Context(), m, "after shutdown")
				checkError(t, err, ErrorInternal, "shutting down")
			})
		})
	}
}

func TestServeConnCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			release = make(chan struct{})
			unblock = sync.OnceFunc(func() { close(release) })
			started bool
		)
		defer unblock()

		s, m := echoServer(t, func(_ context.Context, v string) (string, error) {
			started = true
			<-release
			return v, nil
		}, ServerOptions{
			Compression: true,
		})
		var (
			conn, peer = net.Pipe()
			served     = make(chan error, 1)
			done       = make(chan error, 1)
		)
		defer peer.Close()
		go func() { served <- s.ServeConn(t.Context(), conn) }()
		c, err := NewClient(t.Context(), peer, s.Schema(), ClientOptions{
			Compression: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()

		go func() {
			_, err := c.Call(t.Context(), m, "")
			done <- err
		}()
		synctest.Wait()
		testutil.AssertEqual(t, started, true)

		c.Close()
		if err := <-served; err != nil {
			t.Fatal(err)
		}
		if err := <-done; !errors.Is(err, ErrClientClosed) {
			t.Fatal(err)
		}

		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := s.Shutdown(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("Shutdown() = %v; handler is still active", err)
		}

		unblock()
		if err := s.Shutdown(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestServerCancelLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			release  = make(chan struct{})
			unblock  = sync.OnceFunc(func() { close(release) })
			started  bool
			canceled bool
		)
		defer unblock()
		peer, events := cancelPeer(t, 1, func(ctx context.Context, n uint32) (uint32, error) {
			if n == 1 {
				started = true
				<-ctx.Done()
				canceled = true
				<-release
			}
			return n, nil
		})
		if err := peer.writeRequest(1, 1, []byte{1}); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		testutil.AssertEqual(t, started, true)
		if err := peer.writeCancel(1); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		testutil.AssertEqual(t, canceled, true)
		if err := peer.writeRequest(2, 1, []byte{2}); err != nil {
			t.Fatal(err)
		}
		fr, err := peer.readFrame()
		if err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, fr.kind, frameProtocolError)
		testutil.AssertEqual(t, fr.code, uint32(ErrorInternal))

		unblock()
		<-events
		synctest.Wait()
		if err := peer.writeRequest(3, 1, []byte{3}); err != nil {
			t.Fatal(err)
		}
		fr, err = peer.readFrame()
		if err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, fr.kind, frameResponse)
		testutil.AssertEqual(t, fr.requestID, uint64(3))
	})
}

func TestServerCancelReuse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			first         = make(chan struct{})
			second        = make(chan struct{})
			releaseFirst  = sync.OnceFunc(func() { close(first) })
			releaseSecond = sync.OnceFunc(func() { close(second) })
			started       = make(chan uint32, 2)
		)
		defer releaseFirst()
		defer releaseSecond()
		peer, events := cancelPeer(t, 2, func(ctx context.Context, n uint32) (uint32, error) {
			started <- n
			if n == 1 {
				<-first
			} else {
				<-second
				if err := ctx.Err(); err != nil {
					return 0, err
				}
			}
			return n, nil
		})
		if err := peer.writeRequest(1, 1, []byte{1}); err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, <-started, uint32(1))
		if err := peer.writeCancel(1); err != nil {
			t.Fatal(err)
		}
		if err := peer.writeRequest(1, 1, []byte{2}); err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, <-started, uint32(2))

		releaseFirst()
		<-events // The old handler must finish without writing under the reused ID.
		releaseSecond()
		fr, err := peer.readFrame()
		if err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, fr.kind, frameResponse)
		testutil.AssertEqual(t, fr.requestID, uint64(1))
		testutil.AssertEqual(t, fr.payload, []byte{2})
	})
}

func TestServerPermissions(t *testing.T) {
	sch := echoSchema("string")
	sch.Permissions = []PermissionInfo{
		{
			Name: "echo",
		},
	}
	sch.Methods[0].Public = false
	sch.Methods[0].Permission = "echo"
	var (
		s = newServer(t, sch, ServerOptions{
			Security: Security{
				Authorize: func(context.Context, Principal, string, MethodInfo, any) error { return nil },
			},
		})
		m    = plugimpl.NewMethod(sch, 1, plugimpl.StringCodec, plugimpl.StringCodec, plugimpl.ErrorCodec{})
		regs []plugimpl.Registration
	)
	if err := plugimpl.AppendHandler(&regs, m, func(_ context.Context, v string) (string, error) { return v, nil }, "Echo"); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterHandlers(regs); err != nil {
		t.Fatal(err)
	}
	c := connect(t, s, ClientOptions{})

	_, err := c.Call(t.Context(), m, "")
	checkError(t, err, ErrorUnauthenticated, "authenticated principal")
}
func echoSchema(typ string) *Schema {
	return &Schema{
		Package:     "test",
		Major:       1,
		Family:      plugimpl.FamilyFingerprint("test", 1),
		Fingerprint: [32]byte{1},
		Source:      "package test version 1\ntest.echo value: " + typ + " -> " + typ + " public\n",
		Methods: []MethodInfo{
			{
				ID:     1,
				Name:   "test.echo",
				Public: true,
				Fields: []FieldInfo{
					{
						Name: "value",
						Type: plugimpl.PrimitiveType(typ),
					},
				},
				Response: plugimpl.PrimitiveType(typ),
			},
		},
	}
}

func newServer(t testing.TB, sch *Schema, opts ServerOptions) *Server {
	t.Helper()
	s, err := NewServer(sch, opts)
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

func echoServer(t *testing.T, h func(context.Context, string) (string, error), opts ServerOptions) (*Server, plugimpl.Method[string, string]) {
	t.Helper()
	var (
		sch  = echoSchema("string")
		s    = newServer(t, sch, opts)
		m    = plugimpl.NewMethod(sch, 1, plugimpl.StringCodec, plugimpl.StringCodec, plugimpl.ErrorCodec{})
		regs []plugimpl.Registration
	)
	if h == nil {
		h = func(_ context.Context, v string) (string, error) { return v, nil }
	}
	if err := plugimpl.AppendHandler(&regs, m, h, "Echo"); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterHandlers(regs); err != nil {
		t.Fatal(err)
	}
	return s, m
}

func serveConn(t testing.TB, s *Server) net.Conn {
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

func cancelPeer(t *testing.T, limit uint32, h func(context.Context, uint32) (uint32, error)) (*framer, <-chan CallEvent) {
	t.Helper()

	var (
		sch    = echoSchema("u32")
		events = make(chan CallEvent, 4)
		s      = newServer(t, sch, ServerOptions{
			Limits: Limits{
				MaxConcurrentCalls: limit,
			},
			ObserveCall: func(_ context.Context, e CallEvent) { events <- e },
		})
		m    = plugimpl.NewMethod(sch, 1, plugimpl.U32Codec, plugimpl.U32Codec, plugimpl.ErrorCodec{})
		regs []plugimpl.Registration
	)
	if err := plugimpl.AppendHandler(&regs, m, h, "Echo"); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterHandlers(regs); err != nil {
		t.Fatal(err)
	}

	peer := serveConn(t, s)
	if _, err := clientHandshake(t.Context(), peer, sch, false, false); err != nil {
		t.Fatal(err)
	}
	return newFramer(peer, peer, s.Limits(), false), events
}
