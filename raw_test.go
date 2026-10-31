// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"go.astrophena.name/plug/plugimpl"
)

const (
	rawTestMethodID uint32 = 0x10203040
	rawDeniedID     uint32 = 0x50607080
	rawPingMethodID uint32 = 0x90a0b0c0
)

type rawTestRequest struct {
	Message string
}

type rawDenied struct {
	Reason string
}

func (e *rawDenied) Error() string { return "raw denied: " + e.Reason }

func rawTestContract(t *testing.T) (*Schema, plugimpl.RawMethod[rawTestRequest]) {
	t.Helper()
	schema := &Schema{
		Package:     "rawtest",
		Major:       1,
		Family:      plugimpl.FamilyFingerprint("rawtest", 1),
		Fingerprint: [32]byte{1},
		Source:      "package rawtest version 1\nrawtest.open message: string -> raw public\n",
		Types: []TypeInfo{
			{
				Name:  "RawError",
				Kind:  TypeRecord,
				Error: true,
				Constructors: []ConstructorInfo{
					{
						Name: "denied",
						ID:   rawDeniedID,
						Fields: []FieldInfo{
							{
								Name: "reason",
								Type: TypeExpr{
									Kind: TypeExprPrimitive,
									Name: "string",
								},
							},
						},
					},
				},
			},
		},
		Methods: []MethodInfo{
			{
				Name: "rawtest.open",
				ID:   rawTestMethodID,
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
					Name: "raw",
				},
				Error:  "RawError",
				Public: true,
			},
		},
	}
	if err := plugimpl.ValidateSchema(schema); err != nil {
		t.Fatal(err)
	}
	method := plugimpl.NewRawMethod(
		schema,
		rawTestMethodID,
		plugimpl.Codec[rawTestRequest]{
			Encode: func(encoder *plugimpl.Encoder, value rawTestRequest) {
				encoder.String(value.Message)
			},
			Decode: func(decoder *plugimpl.Decoder) rawTestRequest {
				return rawTestRequest{
					Message: decoder.String(),
				}
			},
		},
		plugimpl.ErrorCodec{
			Encode: func(encoder *plugimpl.Encoder, err error) (uint32, bool) {
				var denied *rawDenied
				if !errors.As(err, &denied) {
					return 0, false
				}
				encoder.String(denied.Reason)
				return rawDeniedID, true
			},
			Decode: func(decoder *plugimpl.Decoder, id uint32) error {
				if id != rawDeniedID {
					decoder.UnknownConstructor("RawError", id)
					return nil
				}
				return &rawDenied{
					Reason: decoder.String(),
				}
			},
		},
	)
	return schema, method
}

func TestRawHandlerEntryRejectsNilRegistrationTarget(t *testing.T) {
	_, method := rawTestContract(t)
	handler := func(context.Context, rawTestRequest, *RawStream) error { return nil }

	if err := plugimpl.AppendRawHandler(nil, method, handler, "Open"); err == nil {
		t.Fatal("expected nil batch error")
	}
	if err := (*Server)(nil).RegisterHandlers(nil); err == nil {
		t.Fatal("expected nil server error")
	}
}

func TestRawRequestDecodeBudget(t *testing.T) {
	schema, method := rawTestContract(t)
	server, err := NewServer(schema, ServerOptions{
		Limits: Limits{
			MaxDecodeBytes: 1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var batch []plugimpl.Registration
	if err := plugimpl.AppendRawHandler(&batch, method, func(context.Context, rawTestRequest, *RawStream) error {
		t.Error("handler received a request over its decode budget")
		return nil
	}, "Open"); err != nil {
		t.Fatal(err)
	}
	if err := server.RegisterHandlers(batch); err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.ServeConn(t.Context(), a) }()
	_, err = OpenRaw(t.Context(), b, method, rawTestRequest{
		Message: "too long",
	}, ClientOptions{})
	var remote *RemoteError
	if !errors.As(err, &remote) || remote.Code != ErrorBadRequest || !strings.Contains(remote.Message, "allocation limit") {
		t.Fatalf("raw request error = %v, want allocation limit", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRawMethodTransfersDedicatedConnection(t *testing.T) {
	schema, method := rawTestContract(t)
	server, err := NewServer(schema, ServerOptions{
		Compression: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	message := strings.Repeat("hello", 300)
	var registrations []plugimpl.Registration
	if err := plugimpl.AppendRawHandler(&registrations, method, func(_ context.Context, request rawTestRequest, stream *RawStream) error {
		if request.Message != message {
			return fmt.Errorf("message = %q", request.Message)
		}
		conn, err := stream.Accept()
		if err != nil {
			return err
		}
		var requestBody [4]byte
		if _, err := io.ReadFull(conn, requestBody[:]); err != nil {
			return err
		}
		_, err = conn.Write(append([]byte("echo:"), requestBody[:]...))
		return err
	}, "Open"); err != nil {
		t.Fatal(err)
	}
	if err := server.RegisterHandlers(registrations); err != nil {
		t.Fatal(err)
	}

	serverConn, clientConn := net.Pipe()
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.ServeConn(t.Context(), serverConn)
	}()

	raw, err := OpenRaw(t.Context(), clientConn, method, rawTestRequest{
		Message: message,
	}, ClientOptions{
		Compression: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len("echo:ping"))
	if _, err := io.ReadFull(raw, response); err != nil {
		t.Fatal(err)
	}
	if string(response) != "echo:ping" {
		t.Fatalf("response = %q", response)
	}
	if err := raw.CloseWrite(); !errors.Is(err, ErrCloseWriteUnsupported) {
		t.Fatalf("CloseWrite error = %v", err)
	}
	if err := raw.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("raw server did not stop")
	}
}

func TestRawMethodReturnsDeclaredErrorBeforeAcceptance(t *testing.T) {
	schema, method := rawTestContract(t)
	server, err := NewServer(schema, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var registrations []plugimpl.Registration
	if err := plugimpl.AppendRawHandler(&registrations, method, func(context.Context, rawTestRequest, *RawStream) error {
		return &rawDenied{
			Reason: "maintenance",
		}
	}, "Open"); err != nil {
		t.Fatal(err)
	}
	if err := server.RegisterHandlers(registrations); err != nil {
		t.Fatal(err)
	}

	serverConn, clientConn := net.Pipe()
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.ServeConn(t.Context(), serverConn) }()
	_, err = OpenRaw(t.Context(), clientConn, method, rawTestRequest{}, ClientOptions{})
	var denied *rawDenied
	if !errors.As(err, &denied) || denied.Reason != "maintenance" {
		t.Fatalf("error = %v, denied = %#v", err, denied)
	}
	if err := <-serveDone; err != nil {
		t.Fatal(err)
	}
}

func TestOpenRawPreservesBytesBufferedAfterAcceptance(t *testing.T) {
	schema, method := rawTestContract(t)
	serverConn, clientConn := net.Pipe()
	serverDone := make(chan error, 1)
	go func() {
		defer serverConn.Close()
		hello, err := readClientHello(serverConn)
		if err != nil {
			serverDone <- err
			return
		}
		if hello.family != handshakeHash(schema.Family) {
			serverDone <- fmt.Errorf("family mismatch")
			return
		}
		if err := writeServerHello(serverConn, handshakeOK, schema, false); err != nil {
			serverDone <- err
			return
		}
		request, err := newFramer(nil, bufio.NewReader(serverConn), Limits{
			MaxFrameSize: DefaultMaxFrameSize,
		}, false).readFrame()
		if err != nil {
			serverDone <- err
			return
		}
		if request.code != rawTestMethodID {
			serverDone <- fmt.Errorf("method ID = 0x%08x", request.code)
			return
		}
		response := encodedFrame(frame{
			kind:      frameResponse,
			requestID: 1,
		})
		response = append(response, "buffered"...)
		serverDone <- writeAll(serverConn, response)
	}()

	raw, err := OpenRaw(t.Context(), clientConn, method, rawTestRequest{}, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, len("buffered"))
	if _, err := io.ReadFull(raw, data); err != nil {
		t.Fatal(err)
	}
	if string(data) != "buffered" {
		t.Fatalf("buffered data = %q", data)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestRawConnectionClosesWhenContextEnds(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	ctx, cancel := context.WithCancel(t.Context())
	raw := newRawConn(ctx, clientConn, clientConn)
	defer raw.Close()
	if err := serverConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	cancel()
	var data [1]byte
	if _, err := serverConn.Read(data[:]); err == nil {
		t.Fatal("peer remained open after raw context cancellation")
	}
}

func TestRawConnectionWithCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for range 1000 {
		peer, conn := net.Pipe()
		raw := newRawConn(ctx, conn, conn)
		if err := raw.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := peer.Write([]byte("closed")); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("Write() = %v; want io.ErrClosedPipe", err)
		}
		peer.Close()
	}
}

func TestRawMethodRejectsReusedConnection(t *testing.T) {
	schema, rawMethod := rawTestContract(t)
	schema.Methods = append(schema.Methods, MethodInfo{
		Name: "rawtest.ping",
		ID:   rawPingMethodID,
		Response: TypeExpr{
			Kind: TypeExprPrimitive,
			Name: "void",
		},
		Public: true,
	})
	pingMethod := plugimpl.NewMethod(schema, rawPingMethodID, plugimpl.VoidCodec, plugimpl.VoidCodec, plugimpl.ErrorCodec{})
	server, err := NewServer(schema, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var registrations []plugimpl.Registration
	if err := plugimpl.AppendHandler(&registrations, pingMethod, func(context.Context, plugimpl.Void) (plugimpl.Void, error) {
		return plugimpl.Void{}, nil
	}, "Ping"); err != nil {
		t.Fatal(err)
	}
	if err := plugimpl.AppendRawHandler(&registrations, rawMethod, func(context.Context, rawTestRequest, *RawStream) error {
		t.Error("raw handler ran on a reused connection")
		return nil
	}, "Open"); err != nil {
		t.Fatal(err)
	}
	if err := server.RegisterHandlers(registrations); err != nil {
		t.Fatal(err)
	}

	serverConn, clientConn := net.Pipe()
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.ServeConn(t.Context(), serverConn) }()
	if err := writeClientHello(clientConn, handshakeCall, false, false, schema.Family, schema.Fingerprint); err != nil {
		t.Fatal(err)
	}
	status, _, err := readServerHello(clientConn, false)
	if err != nil {
		t.Fatal(err)
	}
	if status != handshakeOK {
		t.Fatalf("handshake status = %d", status)
	}
	reader := bufio.NewReader(clientConn)
	framer := newFramer(clientConn, reader, Limits{
		MaxFrameSize: DefaultMaxFrameSize,
	}, false)
	if err := framer.writeFrame(frame{
		kind:      frameRequest,
		requestID: 1,
		code:      rawPingMethodID,
	}); err != nil {
		t.Fatal(err)
	}
	response, err := framer.readFrame()
	if err != nil {
		t.Fatal(err)
	}
	if response.kind != frameResponse {
		t.Fatalf("ping response kind = %d", response.kind)
	}
	payload, err := plugimpl.Encode(rawMethod.Request(), rawTestRequest{}, DefaultMaxListElements)
	if err != nil {
		t.Fatal(err)
	}
	if err := framer.writeFrame(frame{
		kind:      frameRequest,
		requestID: 2,
		code:      rawTestMethodID,
		payload:   payload,
	}); err != nil {
		t.Fatal(err)
	}
	response, err = framer.readFrame()
	if err != nil {
		t.Fatal(err)
	}
	if response.kind != frameProtocolError || ErrorCode(response.code) != ErrorBadRequest {
		t.Fatalf("raw rejection = %#v", response)
	}
	if err := <-serveDone; err == nil {
		t.Fatal("server accepted a raw method on a reused connection")
	}
}

func encodedFrame(value frame) []byte {
	var body []byte
	body = append(body, byte(value.kind))
	body = binary.AppendUvarint(body, value.requestID)
	switch value.kind {
	case frameRequest, frameApplicationError:
		body = binary.LittleEndian.AppendUint32(body, value.code)
	case frameProtocolError:
		body = append(body, byte(value.code))
	}
	body = append(body, value.payload...)
	data := binary.AppendUvarint(nil, uint64(len(body)))
	data = append(data, body...)
	return data
}
