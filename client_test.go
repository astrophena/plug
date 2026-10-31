// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"go.astrophena.name/plug/plugimpl"
)

type writeNotifyConn struct {
	net.Conn
	writes chan struct{}
}

func (c *writeNotifyConn) Write(data []byte) (int, error) {
	select {
	case c.writes <- struct{}{}:
	default:
	}
	return c.Conn.Write(data)
}

func blockedClient(t *testing.T) (*Client, *framer, <-chan struct{}) {
	t.Helper()
	schema := &Schema{
		Package:     "clienttest",
		Major:       1,
		Family:      plugimpl.FamilyFingerprint("clienttest", 1),
		Fingerprint: [32]byte{1},
		Source:      "package clienttest version 1\nclienttest.echo message: string -> string public\n",
		Methods: []MethodInfo{
			{
				ID:     1,
				Name:   "clienttest.echo",
				Public: true,
				Fields: []FieldInfo{
					{
						Name: "message",
						Type: TypeExpr{
							Kind: TypeExprPrimitive,
							Name: "string",
						},
					},
				},
				Response: TypeExpr{
					Kind: TypeExprPrimitive,
					Name: "string",
				},
			},
		},
	}
	peer, conn := net.Pipe()
	t.Cleanup(func() { peer.Close() })
	if err := peer.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	handshake := make(chan error, 1)
	go func() {
		_, err := readClientHello(peer)
		if err == nil {
			err = writeServerHello(peer, handshakeOK, schema, false)
		}
		handshake <- err
	}()
	notify := &writeNotifyConn{
		Conn:   conn,
		writes: make(chan struct{}, 1),
	}
	client, err := NewClient(t.Context(), notify, schema, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	if err := <-handshake; err != nil {
		t.Fatal(err)
	}
	<-notify.writes // The next notification belongs to a request.
	return client, newFramer(peer, peer, client.limits, false), notify.writes
}

func TestClientErr(t *testing.T) {
	for name, want := range map[string]error{"close": ErrClientClosed, "disconnect": io.EOF} {
		t.Run(name, func(t *testing.T) {
			client, peer, _ := blockedClient(t)
			if err := client.Err(); err != nil {
				t.Fatalf("Err() = %v while open", err)
			}
			if want == ErrClientClosed {
				client.Close()
			} else {
				peer.w.(net.Conn).Close()
			}
			select {
			case <-client.done:
			case <-time.After(time.Second):
				t.Fatal("client did not detect disconnect")
			}
			if err := client.Err(); !errors.Is(err, want) {
				t.Fatalf("Err() = %v; want %v", err, want)
			}
		})
	}
}

func TestClientCancelsBlockedWrite(t *testing.T) {
	client, peer, writing := blockedClient(t)
	method := plugimpl.NewMethod(client.schema, 1, plugimpl.StringCodec, plugimpl.StringCodec, plugimpl.ErrorCodec{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	message := strings.Repeat("request", 1024)
	done := make(chan error, 1)
	go func() {
		_, err := Call(ctx, client, method, message)
		done <- err
	}()
	<-writing
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Call() = %v; want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Call did not return while the write was blocked")
	}

	// A call waiting for the writer must also respect its deadline.
	waitCtx, waitCancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer waitCancel()
	if _, err := Call(waitCtx, client, method, "queued"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued Call() = %v; want context.DeadlineExceeded", err)
	}

	// Exercise encoder reuse before the blocked write resumes.
	enc, err := plugimpl.EncodePooled(plugimpl.StringCodec, strings.Repeat("changed", 1024), client.limits.MaxListElements)
	if err != nil {
		t.Fatal(err)
	}
	defer plugimpl.ReleaseEncoder(enc)
	request, err := peer.readFrame()
	if err != nil {
		t.Fatal(err)
	}
	got, err := plugimpl.Decode(plugimpl.StringCodec, request.payload, client.limits.MaxListElements, 0)
	if err != nil || got != message {
		t.Fatalf("request changed during cancellation: message matches = %t, error = %v", got == message, err)
	}
	canceled, err := peer.readFrame()
	if err != nil || canceled.kind != frameCancel || canceled.requestID != request.requestID {
		t.Fatalf("cancellation frame = %+v, error = %v", canceled, err)
	}

	go func() {
		result, err := Call(t.Context(), client, method, "next")
		if err == nil && result != "reply" {
			err = errors.New("unexpected reply")
		}
		done <- err
	}()
	request, err = peer.readFrame()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := plugimpl.Encode(plugimpl.StringCodec, "reply", client.limits.MaxListElements)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.writeResponse(request.requestID, payload); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("call after cancellation: %v", err)
	}
}

func TestClientCloseUnblocksWrite(t *testing.T) {
	client, _, writing := blockedClient(t)
	method := plugimpl.NewMethod(client.schema, 1, plugimpl.StringCodec, plugimpl.StringCodec, plugimpl.ErrorCodec{})
	done := make(chan error, 1)
	go func() {
		_, err := Call(t.Context(), client, method, "blocked")
		done <- err
	}()
	<-writing
	client.Close()
	select {
	case err := <-done:
		if !errors.Is(err, ErrClientClosed) {
			t.Fatalf("Call() = %v; want ErrClientClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not unblock Call")
	}
}
