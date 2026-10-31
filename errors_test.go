// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/example/greeter"
)

func TestUnknownApplicationErrorRoundTrip(t *testing.T) {
	// Create a server that returns UnknownLookupError directly from a handler.
	server, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{
		Security: plug.Security{
			Authenticate: func(ctx context.Context, conn net.Conn) (plug.Principal, error) {
				return plug.Principal{
					Subject: "test",
				}, nil
			},
			Authorize: func(ctx context.Context, principal plug.Principal, permission string, info plug.MethodInfo, request any) error {
				return nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	expectedID := uint32(0xdeadbeef)
	expectedPayload := []byte("some opaque data from the future")

	if err := greeter.RegisterHandlers(server, greeter.Handlers{
		Ping:      func(context.Context) error { return nil },
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) { return nil, nil },
		GetUser: func(context.Context, greeter.GetUserRequest) (greeter.User, error) {
			return greeter.User{}, &greeter.UnknownLookupError{
				ConstructorID: expectedID,
				Payload:       expectedPayload,
			}
		},
	}); err != nil {
		t.Fatal(err)
	}

	serverConn, clientConn := net.Pipe()
	go server.ServeConn(t.Context(), serverConn)

	client, err := plug.NewClient(t.Context(), clientConn, &greeter.Schema, plug.ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = greeter.NewClient(client).GetUser(t.Context(), greeter.GetUserRequest{
		ID: 1,
	})
	if err == nil {
		t.Fatal("expected error")
	}

	// The errors.As check against the generated error-sum interface (greeter.LookupError) must succeed.
	var lookupErr greeter.LookupError
	if !errors.As(err, &lookupErr) {
		t.Fatalf("expected errors.As to succeed for greeter.LookupError, got: %T", err)
	}

	// Verify that the specific type is *greeter.UnknownLookupError.
	unknownErr, ok := lookupErr.(*greeter.UnknownLookupError)
	if !ok {
		t.Fatalf("expected *greeter.UnknownLookupError, got: %T", lookupErr)
	}

	// Verify that the constructor ID and opaque payload are correctly preserved.
	if unknownErr.ConstructorID != expectedID {
		t.Errorf("expected ConstructorID %08x, got %08x", expectedID, unknownErr.ConstructorID)
	}
	if string(unknownErr.Payload) != string(expectedPayload) {
		t.Errorf("expected Payload %q, got %q", expectedPayload, unknownErr.Payload)
	}

	client.Close()
}
