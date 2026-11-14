// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/plug/plugimpl"
)

func TestClientErr(t *testing.T) {
	for name, want := range map[string]error{"close": ErrClientClosed, "disconnect": io.EOF} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c, peer := blockedClient(t)
				testutil.AssertEqual(t, c.Err(), nil)

				if name == "close" {
					c.Close()
				} else {
					peer.w.(net.Conn).Close()
				}
				synctest.Wait()
				if err := c.Err(); !errors.Is(err, want) {
					t.Fatalf("Err() = %v; want %v", err, want)
				}
			})
		})
	}
}

func TestClientCancelWrite(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, peer := blockedClient(t)
		var (
			m           = plugimpl.NewMethod(c.schema, 1, plugimpl.StringCodec, plugimpl.StringCodec, plugimpl.ErrorCodec{})
			ctx, cancel = context.WithCancel(t.Context())
			msg         = strings.Repeat("request", 1024)
			done        = make(chan error, 1)
		)
		defer cancel()

		go func() {
			_, err := c.Call(ctx, m, msg)
			done <- err
		}()
		synctest.Wait() // The writer is blocked because the peer has not read the request.
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}

		queued, stop := context.WithTimeout(t.Context(), time.Second)
		defer stop()
		if _, err := c.Call(queued, m, "queued"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("queued call = %v", err)
		}

		// Reusing an encoder must not change the bytes still owned by the writer.
		enc, err := plugimpl.EncodePooled(plugimpl.StringCodec, strings.Repeat("changed", 1024), c.limits.MaxListElements)
		if err != nil {
			t.Fatal(err)
		}
		defer plugimpl.ReleaseEncoder(enc)
		req, err := peer.readFrame()
		if err != nil {
			t.Fatal(err)
		}
		got, err := plugimpl.Decode(plugimpl.StringCodec, req.payload, c.limits.MaxListElements, 0)
		if err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, got, msg)
		cancelFrame, err := peer.readFrame()
		if err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, cancelFrame.kind, frameCancel)
		testutil.AssertEqual(t, cancelFrame.requestID, req.requestID)

		var resp string
		go func() {
			var err error
			resp, err = c.Call(t.Context(), m, "next")
			done <- err
		}()
		req, err = peer.readFrame()
		if err != nil {
			t.Fatal(err)
		}
		data, err := plugimpl.Encode(plugimpl.StringCodec, "reply", c.limits.MaxListElements)
		if err != nil {
			t.Fatal(err)
		}
		if err := peer.writeResponse(req.requestID, data); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, resp, "reply")
	})
}

func TestClientCloseWrite(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, _ := blockedClient(t)
		var (
			m    = plugimpl.NewMethod(c.schema, 1, plugimpl.StringCodec, plugimpl.StringCodec, plugimpl.ErrorCodec{})
			done = make(chan error, 1)
		)

		go func() {
			_, err := c.Call(t.Context(), m, "blocked")
			done <- err
		}()
		synctest.Wait()
		c.Close()
		if err := <-done; !errors.Is(err, ErrClientClosed) {
			t.Fatalf("Call() = %v; want ErrClientClosed", err)
		}
	})
}

func TestClientCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			started  bool
			canceled bool
		)
		s, m := echoServer(t, func(ctx context.Context, v string) (string, error) {
			if v == "cancel" {
				started = true
				<-ctx.Done()
				canceled = true
				return "", ctx.Err()
			}
			return v, nil
		}, ServerOptions{})
		var (
			c           = connect(t, s, ClientOptions{})
			ctx, cancel = context.WithCancel(t.Context())
			done        = make(chan error, 1)
		)
		defer cancel()

		go func() {
			_, err := c.Call(ctx, m, "cancel")
			done <- err
		}()
		synctest.Wait()
		testutil.AssertEqual(t, started, true)

		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		synctest.Wait()
		testutil.AssertEqual(t, canceled, true)

		got, err := c.Call(t.Context(), m, "ok")
		if err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, got, "ok")
	})
}

func TestClientMultiplexing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			release = make(chan struct{})
			unblock = sync.OnceFunc(func() { close(release) })
			started bool
		)
		defer unblock()

		s, m := echoServer(t, func(_ context.Context, v string) (string, error) {
			if v == "slow" {
				started = true
				<-release
			}
			return v, nil
		}, ServerOptions{})
		var (
			c    = connect(t, s, ClientOptions{})
			done = make(chan error, 1)
		)
		go func() {
			got, err := c.Call(t.Context(), m, "slow")
			if err == nil && got != "slow" {
				err = fmt.Errorf("slow response = %q", got)
			}
			done <- err
		}()
		synctest.Wait()
		testutil.AssertEqual(t, started, true)

		got, err := c.Call(t.Context(), m, "fast")
		if err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, got, "fast")

		unblock()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

func TestClientCompression(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		events := make(chan CallEvent, 1)
		s, m := echoServer(t, nil, ServerOptions{
			Compression: true,
			ObserveCall: func(_ context.Context, e CallEvent) { events <- e },
		})
		var (
			c = connect(t, s, ClientOptions{
				Compression: true,
			})
			msg = strings.Repeat("compressible ", 200)
		)

		got, err := c.Call(t.Context(), m, msg)
		if err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, got, msg)

		e := <-events
		testutil.AssertEqual(t, e.Compressed, true)
		if e.PayloadSize <= len(msg) || e.CompressedSize <= 0 || e.CompressedSize >= e.PayloadSize {
			t.Fatalf("compression event = %+v", e)
		}
		testutil.AssertEqual(t, e.CompressionSavings, e.PayloadSize-e.CompressedSize)
	})
}

func TestClientConcurrentClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _ := echoServer(t, nil, ServerOptions{})
		var (
			c    = connect(t, s, ClientOptions{})
			done = make(chan error, 10)
		)

		for range cap(done) {
			go func() { done <- c.Close() }()
		}
		for range cap(done) {
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}

		if !errors.Is(c.Err(), ErrClientClosed) {
			t.Fatal(c.Err())
		}
	})
}

func TestDialFallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, m := echoServer(t, nil, ServerOptions{})
		n := 0
		c, err := DialFunc(t.Context(), func(context.Context) (net.Conn, error) {
			n++
			return serveConn(t, s), nil
		}, s.Schema(), ClientOptions{
			Compression: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()

		testutil.AssertEqual(t, n, 2)
		if _, err := c.Call(t.Context(), m, ""); err != nil {
			t.Fatal(err)
		}
	})
}

func TestDial(t *testing.T) {
	s, m := echoServer(t, nil, ServerOptions{})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var (
		ctx, cancel = context.WithCancel(t.Context())
		done        = make(chan error, 1)
	)
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	go func() { done <- s.Serve(ctx, ln) }()

	c, err := Dial(t.Context(), "tcp", ln.Addr().String(), s.Schema(), ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.Call(t.Context(), m, ""); err != nil {
		t.Fatal(err)
	}
}

func connect(t testing.TB, s *Server, opts ClientOptions) *Client {
	t.Helper()
	c, err := NewClient(t.Context(), serveConn(t, s), s.Schema(), opts)
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

func checkError(t *testing.T, err error, code ErrorCode, msg string) {
	t.Helper()
	e, ok := errors.AsType[*RemoteError](err)
	if !ok {
		t.Fatalf("error = %v; want RemoteError", err)
	}
	testutil.AssertEqual(t, e.Code, code)
	if !strings.Contains(e.Message, msg) {
		t.Fatalf("message = %q; want %q", e.Message, msg)
	}
}

// blockedClient leaves request reads to the test.
func blockedClient(t *testing.T) (*Client, *framer) {
	t.Helper()
	var (
		sch        = echoSchema("string")
		peer, conn = net.Pipe()
		done       = make(chan error, 1)
	)
	t.Cleanup(func() { peer.Close() })
	go func() {
		_, err := readClientHello(peer)
		if err == nil {
			err = writeServerHello(peer, handshakeOK, sch, false)
		}
		done <- err
	}()
	c, err := NewClient(t.Context(), conn, sch, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	return c, newFramer(peer, peer, c.limits, false)
}
