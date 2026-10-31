// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"go.astrophena.name/plug/plugimpl"
)

func cancelPeer(t *testing.T, limit uint32, handler func(context.Context, uint32) (uint32, error)) (*Server, *framer, <-chan CallEvent) {
	t.Helper()
	schema := &Schema{
		Package: "canceltest",
		Major:   1,
		Family:  plugimpl.FamilyFingerprint("canceltest", 1),
		Source:  "package canceltest version 1\ncanceltest.echo value: u32 -> u32 public\n",
		Methods: []MethodInfo{
			{
				ID:     1,
				Name:   "canceltest.echo",
				Public: true,
				Fields: []FieldInfo{
					{
						Name: "value",
						Type: TypeExpr{
							Kind: TypeExprPrimitive,
							Name: "u32",
						},
					},
				},
				Response: TypeExpr{
					Kind: TypeExprPrimitive,
					Name: "u32",
				},
			},
		},
	}
	events := make(chan CallEvent, 4)
	s, err := NewServer(schema, ServerOptions{
		Limits: Limits{
			MaxConcurrentCalls: limit,
		},
		ObserveCall: func(_ context.Context, e CallEvent) { events <- e },
	})
	if err != nil {
		t.Fatal(err)
	}
	method := plugimpl.NewMethod(schema, 1, plugimpl.U32Codec, plugimpl.U32Codec, plugimpl.ErrorCodec{})
	var batch []plugimpl.Registration
	if err := plugimpl.AppendHandler(&batch, method, handler, "Echo"); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterHandlers(batch); err != nil {
		t.Fatal(err)
	}

	conn, peer := net.Pipe()
	t.Cleanup(func() { peer.Close() })
	peer.SetDeadline(time.Now().Add(5 * time.Second))
	done := make(chan error, 1)
	go func() { done <- s.ServeConn(t.Context(), conn) }()
	t.Cleanup(func() {
		peer.Close()
		<-done
	})
	if _, err := clientHandshake(t.Context(), peer, schema, false, false); err != nil {
		t.Fatal(err)
	}
	return s, newFramer(peer, peer, s.limits, false), events
}

func TestCanceledCallRetainsCapacity(t *testing.T) {
	var (
		started  = make(chan struct{})
		canceled = make(chan struct{})
		release  = make(chan struct{})
	)
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	s, peer, events := cancelPeer(t, 1, func(ctx context.Context, n uint32) (uint32, error) {
		if n == 1 {
			close(started)
			<-ctx.Done()
			close(canceled)
			<-release
		}
		return n, nil
	})
	if err := peer.writeRequest(1, 1, []byte{1}); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := peer.writeCancel(1); err != nil {
		t.Fatal(err)
	}
	<-canceled
	if err := peer.writeRequest(2, 1, []byte{2}); err != nil {
		t.Fatal(err)
	}
	f, err := peer.readFrame()
	if err != nil || f.kind != frameProtocolError || f.code != uint32(ErrorInternal) {
		t.Fatalf("request beyond limit = %+v, %v; want protocol error", f, err)
	}

	unblock()
	<-events
	s.activeCalls.Wait()
	if err := peer.writeRequest(3, 1, []byte{3}); err != nil {
		t.Fatal(err)
	}
	f, err = peer.readFrame()
	if err != nil || f.kind != frameResponse || f.requestID != 3 {
		t.Fatalf("request after completion = %+v, %v", f, err)
	}
}

func TestCanceledCallIDReuse(t *testing.T) {
	var (
		started = make(chan uint32, 2)
		first   = make(chan struct{})
		second  = make(chan struct{})
	)
	releaseFirst := sync.OnceFunc(func() { close(first) })
	releaseSecond := sync.OnceFunc(func() { close(second) })
	defer releaseFirst()
	defer releaseSecond()
	_, peer, events := cancelPeer(t, 2, func(ctx context.Context, n uint32) (uint32, error) {
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
	<-started
	if err := peer.writeCancel(1); err != nil {
		t.Fatal(err)
	}
	if err := peer.writeRequest(1, 1, []byte{2}); err != nil {
		t.Fatal(err)
	}
	<-started
	releaseFirst()
	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("canceled call tried to write a response under the reused ID")
	}

	releaseSecond()
	f, err := peer.readFrame()
	if err != nil || f.kind != frameResponse || f.requestID != 1 || len(f.payload) != 1 || f.payload[0] != 2 {
		t.Fatalf("reused ID response = %+v, %v; want second call", f, err)
	}
}
