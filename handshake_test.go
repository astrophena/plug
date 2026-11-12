// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"go.astrophena.name/plug/plugimpl"
)

func TestHandshakeGoldenEncoding(t *testing.T) {
	var family, fingerprint [32]byte
	for i := range family {
		family[i] = byte(i)
		fingerprint[i] = byte(32 + i)
	}
	cases := map[string]struct {
		mode        handshakeMode
		strict      bool
		compression bool
		want        []byte
	}{
		"call": {
			mode: handshakeCall,
			want: append(append([]byte{'P', 'L', 'U', 'G', 1, 0}, family[:handshakeHashSize]...), fingerprint[:handshakeHashSize]...),
		},
		"strict call": {
			mode:   handshakeCall,
			strict: true,
			want:   append(append([]byte{'P', 'L', 'U', 'G', 1, handshakeFlagStrict}, family[:handshakeHashSize]...), fingerprint[:handshakeHashSize]...),
		},
		"compressed call": {
			mode:        handshakeCall,
			compression: true,
			want:        append(append([]byte{'P', 'L', 'U', 'G', 1, handshakeFlagZstd}, family[:handshakeHashSize]...), fingerprint[:handshakeHashSize]...),
		},
		"inspect": {
			mode: handshakeInspect,
			want: []byte{'P', 'L', 'U', 'G', 1, handshakeFlagInspect},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			conn := new(memoryConn)
			if err := writeClientHello(conn, tc.mode, tc.strict, tc.compression, family, fingerprint); err != nil {
				t.Fatal(err)
			}
			if got := conn.Bytes(); !bytes.Equal(got, tc.want) {
				t.Fatalf("client hello = %x, want %x", got, tc.want)
			}
			hello, err := readClientHello(conn)
			if err != nil {
				t.Fatal(err)
			}
			if hello.mode != tc.mode || hello.strict != tc.strict || hello.compression != tc.compression || hello.version != wireVersion {
				t.Fatalf("decoded hello = %#v", hello)
			}
			if tc.mode == handshakeCall && (hello.family != handshakeHash(family) || hello.fingerprint != handshakeHash(fingerprint)) {
				t.Fatalf("decoded hashes = %x %x", hello.family, hello.fingerprint)
			}
		})
	}
}

func TestServerHandshakeEncoding(t *testing.T) {
	schema := &Schema{
		Family:      [32]byte{1},
		Fingerprint: [32]byte{2},
		Source:      "package test version 1\n",
	}

	accepted := new(memoryConn)
	if err := writeServerHello(accepted, handshakeOK, schema, false); err != nil {
		t.Fatal(err)
	}
	if got := accepted.Bytes(); !bytes.Equal(got, []byte{byte(handshakeOK)}) {
		t.Fatalf("accepted server hello = %x", got)
	}

	described := new(memoryConn)
	if err := writeServerHello(described, handshakeSchemaMismatch, schema, true); err != nil {
		t.Fatal(err)
	}
	status, remote, err := readServerHello(described, true)
	if err != nil {
		t.Fatal(err)
	}
	if status != handshakeSchemaMismatch || remote.Version != wireVersion || remote.Family != schema.Family || remote.Fingerprint != schema.Fingerprint || remote.Source != schema.Source {
		t.Fatalf("decoded server hello = %d, %#v", status, remote)
	}
}

func TestCompressionHandshakeRequiresServerSupport(t *testing.T) {
	schema := &Schema{
		Package: "test",
		Major:   1,
		Family:  plugimpl.FamilyFingerprint("test", 1),
		Source:  "package test version 1\n",
	}
	server, err := NewServer(schema, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	serverConn, clientConn := net.Pipe()
	served := make(chan error, 1)
	go func() { served <- server.ServeConn(t.Context(), serverConn) }()
	if _, err := NewClient(t.Context(), clientConn, schema, ClientOptions{
		Compression: true,
	}); !errors.Is(err, errCompressionUnsupported) {
		t.Fatalf("compression error = %v", err)
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
}

func TestDialRetriesWithoutCompression(t *testing.T) {
	schema := &Schema{
		Package: "test",
		Major:   1,
		Family:  plugimpl.FamilyFingerprint("test", 1),
		Source:  "package test version 1\n",
	}
	server, err := NewServer(schema, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	served := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			served <- err
			return
		}
		var header [clientHeaderSize]byte
		_, readErr := io.ReadFull(conn, header[:])
		closeErr := conn.Close()
		if readErr != nil || closeErr != nil {
			served <- errors.Join(readErr, closeErr)
			return
		}

		conn, err = listener.Accept()
		if err != nil {
			served <- err
			return
		}
		served <- server.ServeConn(t.Context(), conn)
	}()

	client, err := Dial(t.Context(), "tcp", listener.Addr().String(), schema, ClientOptions{
		Compression: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
}

func TestDialFuncRetriesWithoutCompression(t *testing.T) {
	if _, err := DialFunc(t.Context(), nil, &Schema{}, ClientOptions{}); err == nil {
		t.Fatal("DialFunc(nil) succeeded; want error")
	}

	schema := &Schema{
		Package: "test",
		Major:   1,
		Family:  plugimpl.FamilyFingerprint("test", 1),
		Source:  "package test version 1\n",
	}
	server, err := NewServer(schema, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}

	served := make(chan error, 2)
	dials := 0
	client, err := DialFunc(t.Context(), func(ctx context.Context) (net.Conn, error) {
		dials++
		serverConn, clientConn := net.Pipe()
		go func() { served <- server.ServeConn(t.Context(), serverConn) }()
		return clientConn, nil
	}, schema, ClientOptions{Compression: true})
	if err != nil {
		t.Fatal(err)
	}
	if dials != 2 {
		t.Fatalf("dials = %d; want 2", dials)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-served; err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandshakeCancellation(t *testing.T) {
	schema := &Schema{
		Package: "test",
		Major:   1,
		Family:  plugimpl.FamilyFingerprint("test", 1),
		Source:  "package test;\n",
	}
	cases := map[string]struct {
		run func(context.Context, net.Conn) error
	}{
		"call": {
			run: func(ctx context.Context, conn net.Conn) error {
				_, err := NewClient(ctx, conn, schema, ClientOptions{})
				return err
			},
		},
		"inspect": {
			run: func(ctx context.Context, conn net.Conn) error {
				_, err := Inspect(ctx, conn)
				return err
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			serverConn, clientConn := net.Pipe()
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			result := make(chan error, 1)
			go func() {
				result <- tc.run(ctx, clientConn)
			}()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("handshake did not stop after cancellation")
			}
			if err := serverConn.Close(); err != nil {
				t.Errorf("close peer: %v", err)
			}
		})
	}
}

func TestServeConnCancellationDuringHandshake(t *testing.T) {
	schema := &Schema{
		Package: "test",
		Major:   1,
		Family:  plugimpl.FamilyFingerprint("test", 1),
		Source:  "package test;\n",
	}
	server, err := NewServer(schema, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	serverConn, clientConn := net.Pipe()
	defer func() {
		if err := clientConn.Close(); err != nil {
			t.Errorf("close peer: %v", err)
		}
	}()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := server.ServeConn(ctx, serverConn); err != nil {
		t.Fatalf("ServeConn error = %v", err)
	}
}
