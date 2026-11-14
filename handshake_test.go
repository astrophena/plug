// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"go.astrophena.name/base/testutil"
)

func TestHandshakeEncoding(t *testing.T) {
	var (
		family      [32]byte
		fingerprint [32]byte
	)
	for i := range family {
		family[i] = byte(i)
		fingerprint[i] = byte(32 + i)
	}
	for name, tc := range map[string]struct {
		mode             handshakeMode
		strict, compress bool
		flags            byte
	}{
		"call": {
			mode: handshakeCall,
		},
		"strict": {
			mode:   handshakeCall,
			strict: true,
			flags:  2,
		},
		"compressed": {
			mode:     handshakeCall,
			compress: true,
			flags:    4,
		},
		"inspect": {
			mode:  handshakeInspect,
			flags: 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			conn := new(memoryConn)
			if err := writeClientHello(conn, tc.mode, tc.strict, tc.compress, family, fingerprint); err != nil {
				t.Fatal(err)
			}
			want := []byte{'P', 'L', 'U', 'G', 1, tc.flags}
			if tc.mode == handshakeCall {
				want = append(want, family[:16]...)
				want = append(want, fingerprint[:16]...)
			}
			testutil.AssertEqual(t, conn.Bytes(), want)
			got, err := readClientHello(conn)
			if err != nil {
				t.Fatal(err)
			}
			testutil.AssertEqual(t, got.mode, tc.mode)
			testutil.AssertEqual(t, got.version, wireVersion)
			testutil.AssertEqual(t, got.strict, tc.strict)
			testutil.AssertEqual(t, got.compression, tc.compress)
			if tc.mode == handshakeCall {
				testutil.AssertEqual(t, got.family, handshakeHash(family))
				testutil.AssertEqual(t, got.fingerprint, handshakeHash(fingerprint))
			}
		})
	}
}

func TestServerHandshakeEncoding(t *testing.T) {
	sch := echoSchema("string")
	accepted := new(memoryConn)

	if err := writeServerHello(accepted, handshakeOK, sch, false); err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, accepted.Bytes(), []byte{0})

	described := new(memoryConn)
	if err := writeServerHello(described, handshakeSchemaMismatch, sch, true); err != nil {
		t.Fatal(err)
	}
	status, remote, err := readServerHello(described, true)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, status, handshakeSchemaMismatch)
	testutil.AssertEqual(t, remote, RemoteSchema{
		Version:     wireVersion,
		Family:      sch.Family,
		Fingerprint: sch.Fingerprint,
		Source:      sch.Source,
	})
}

func TestHandshakeCancellation(t *testing.T) {
	sch := echoSchema("string")
	for name, run := range map[string]func(context.Context, net.Conn) error{
		"client": func(ctx context.Context, conn net.Conn) error {
			_, err := NewClient(ctx, conn, sch, ClientOptions{})
			return err
		},
		"inspect": func(ctx context.Context, conn net.Conn) error {
			_, err := Inspect(ctx, conn)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var (
					conn, peer  = net.Pipe()
					ctx, cancel = context.WithCancel(t.Context())
				)
				defer peer.Close()
				cancel()
				if err := run(ctx, conn); !errors.Is(err, context.Canceled) {
					t.Fatalf("handshake = %v; want context.Canceled", err)
				}
			})
		})
	}
}

func TestHandshakeServerCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sch := echoSchema("string")
		sch.Methods = nil
		s, err := NewServer(sch, ServerOptions{})
		if err != nil {
			t.Fatal(err)
		}
		conn, peer := net.Pipe()
		defer peer.Close()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- s.ServeConn(ctx, conn) }()
		synctest.Wait()
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

type memoryConn struct {
	bytes.Buffer
}

func (*memoryConn) Close() error                     { return nil }
func (*memoryConn) LocalAddr() net.Addr              { return nil }
func (*memoryConn) RemoteAddr() net.Addr             { return nil }
func (*memoryConn) SetDeadline(time.Time) error      { return nil }
func (*memoryConn) SetReadDeadline(time.Time) error  { return nil }
func (*memoryConn) SetWriteDeadline(time.Time) error { return nil }

func TestInspect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _ := echoServer(t, nil, ServerOptions{})
		got, err := Inspect(t.Context(), serveConn(t, s))
		if err != nil {
			t.Fatal(err)
		}

		want := s.Schema()
		testutil.AssertEqual(t, got.Family, want.Family)
		testutil.AssertEqual(t, got.Fingerprint, want.Fingerprint)
		testutil.AssertEqual(t, got.Source, want.Source)
	})
}

func TestSchemaHandshake(t *testing.T) {
	for name, strict := range map[string]bool{"strict mismatch": true, "compatible fingerprint": false} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, m := echoServer(t, nil, ServerOptions{})
				sch := s.Schema()
				sch.Fingerprint[0] ^= 0xff

				c, err := NewClient(t.Context(), serveConn(t, s), sch, ClientOptions{
					StrictSchema: strict,
				})
				if strict {
					e, ok := errors.AsType[*SchemaMismatchError](err)
					if !ok {
						t.Fatalf("NewClient() = %v; want SchemaMismatchError", err)
					}
					testutil.AssertEqual(t, e.Remote.Source, sch.Source)
				} else {
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					if _, err := c.Call(t.Context(), m, ""); err != nil {
						t.Fatal(err)
					}
				}
			})
		})
	}
}
