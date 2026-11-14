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
	"testing"
	"testing/synctest"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/plug/plugimpl"
)

func TestRawBufferedRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			sch, m     = rawMethod()
			conn, peer = net.Pipe()
			done       = make(chan error, 1)
		)
		defer conn.Close()
		defer peer.Close()

		go func() {
			done <- func() error {
				if _, err := readClientHello(conn); err != nil {
					return err
				}
				if err := writeServerHello(conn, handshakeOK, sch, false); err != nil {
					return err
				}
				fr, err := newFramer(nil, conn, Limits{
					MaxFrameSize: DefaultMaxFrameSize,
				}, false).readFrame()
				if err != nil {
					return err
				}
				if fr.code != m.ID() {
					return fmt.Errorf("method ID = %d; want %d", fr.code, m.ID())
				}
				// One write lets the framed reader read ahead into the raw stream.
				return writeAll(conn, append([]byte{2, 2, 1}, "buffered"...))
			}()
		}()
		raw, err := OpenRaw(t.Context(), peer, m, "", ClientOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer raw.Close()
		data := make([]byte, len("buffered"))
		if _, err := io.ReadFull(raw, data); err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, string(data), "buffered")
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

func TestRawReuse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			sch, m = rawMethod()
			s      = newServer(t, sch, ServerOptions{})
			ping   = plugimpl.NewMethod(sch, 2, plugimpl.VoidCodec, plugimpl.VoidCodec, plugimpl.ErrorCodec{})
			regs   []plugimpl.Registration
		)
		if err := plugimpl.AppendHandler(&regs, ping, func(context.Context, plugimpl.Void) (plugimpl.Void, error) { return plugimpl.Void{}, nil }, "Ping"); err != nil {
			t.Fatal(err)
		}
		if err := plugimpl.AppendRawHandler(&regs, m, func(context.Context, string, *RawStream) error {
			t.Error("raw handler ran on a reused connection")
			return nil
		}, "Echo"); err != nil {
			t.Fatal(err)
		}
		if err := s.RegisterHandlers(regs); err != nil {
			t.Fatal(err)
		}
		var (
			conn, peer = net.Pipe()
			done       = make(chan error, 1)
		)
		defer peer.Close()

		go func() { done <- s.ServeConn(t.Context(), conn) }()
		if _, err := clientHandshake(t.Context(), peer, sch, false, false); err != nil {
			t.Fatal(err)
		}
		f := newFramer(peer, peer, s.Limits(), false)
		if err := f.writeRequest(1, ping.ID(), nil); err != nil {
			t.Fatal(err)
		}
		resp, err := f.readFrame()
		if err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, resp.kind, frameResponse)
		data, err := plugimpl.Encode(m.Request(), "", DefaultMaxListElements)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.writeRequest(2, m.ID(), data); err != nil {
			t.Fatal(err)
		}
		resp, err = f.readFrame()
		if err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, resp.kind, frameProtocolError)
		testutil.AssertEqual(t, resp.code, uint32(ErrorBadRequest))
		if err := <-done; err == nil {
			t.Fatal("server accepted a reused raw connection")
		}
	})
}

func TestRawCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			conn, peer  = net.Pipe()
			ctx, cancel = context.WithCancel(t.Context())
			raw         = newRawConn(ctx, conn, conn)
		)
		defer peer.Close()
		defer raw.Close()
		defer cancel()

		cancel()
		synctest.Wait()

		if _, err := peer.Write([]byte("closed")); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("Write() = %v; want closed pipe", err)
		}
	})
}
func rawMethod() (*Schema, plugimpl.RawMethod[string]) {
	sch := echoSchema("string")
	sch.Source = "package test version 1\ntest.echo value: string -> raw public\ntest.ping -> void public\n"
	sch.Methods[0].Response = plugimpl.PrimitiveType("raw")
	sch.Methods = append(sch.Methods, MethodInfo{
		ID:       2,
		Name:     "test.ping",
		Public:   true,
		Response: plugimpl.PrimitiveType("void"),
	})
	return sch, plugimpl.NewRawMethod(sch, 1, plugimpl.StringCodec, plugimpl.ErrorCodec{})
}
