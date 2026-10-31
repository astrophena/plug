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
	"time"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/example/greeter"
	"go.astrophena.name/plug/plugimpl"
)

func TestServerObserverPanicsAndDuration(t *testing.T) {
	observerCalled := make(chan plug.CallEvent, 1)
	server, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{
		ObserveCall: func(ctx context.Context, event plug.CallEvent) {
			observerCalled <- event
			panic("observer panic should not crash the server")
		},
		Security: permissiveSecurity(),
	})
	if err != nil {
		t.Fatal(err)
	}

	handlerCalled := make(chan struct{})
	if err := greeter.RegisterHandlers(server, greeter.Handlers{
		Ping: func(context.Context) error {
			close(handlerCalled)
			return nil
		},
		GetUser: func(context.Context, greeter.GetUserRequest) (greeter.User, error) {
			return greeter.User{}, fmt.Errorf("app error")
		},
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) {
			panic("handler panic")
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
	typedClient := greeter.NewClient(client)

	// Verify that an observer panic does not change a successful outcome.
	if err := typedClient.Ping(t.Context()); err != nil {
		t.Fatalf("expected successful call despite observer panic, got: %v", err)
	}
	<-handlerCalled

	select {
	case event := <-observerCalled:
		if event.Outcome != plug.CallSucceeded {
			t.Errorf("expected outcome CallSucceeded, got %v", event.Outcome)
		}
		if event.Duration == 0 {
			t.Errorf("expected non-zero duration")
		}
		if event.HandlerDuration == 0 {
			t.Errorf("expected non-zero handler duration")
		}
	case <-time.After(time.Second):
		t.Fatal("observer was not called")
	}

	// Verify that an observer panic does not replace the application error.
	_, err = typedClient.GetUser(t.Context(), greeter.GetUserRequest{
		ID: 1,
	})
	if err == nil {
		t.Fatalf("expected error")
	}

	select {
	case event := <-observerCalled:
		if event.Outcome != plug.CallInternalError {
			t.Errorf("expected outcome CallInternalError, got %v", event.Outcome)
		}
	case <-time.After(time.Second):
		t.Fatal("observer was not called")
	}

	// Verify that a handler panic is converted to an internal error.
	_, err = typedClient.ListUsers(t.Context(), greeter.ListUsersRequest{})
	if err == nil {
		t.Fatalf("expected error on handler panic")
	}

	select {
	case event := <-observerCalled:
		if event.Outcome != plug.CallInternalError {
			t.Errorf("expected outcome CallInternalError for panic, got %v", event.Outcome)
		}
		if event.Err == nil || event.Err.Error() == "" {
			t.Errorf("expected panic details in observer event error")
		}
	case <-time.After(time.Second):
		t.Fatal("observer was not called")
	}
}

func TestServerAtomicRegistration(t *testing.T) {
	server, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if err := greeter.RegisterHandlers(server, greeter.Handlers{
		Ping: func(context.Context) error { return nil },
	}); err == nil {
		t.Fatal("expected error registering nil handler")
	}
	err = server.Seal()
	if err == nil {
		t.Fatal("expected error sealing empty server")
	}
	if !strings.Contains(err.Error(), "missing handlers") || !strings.Contains(err.Error(), "ping") {
		t.Errorf("unexpected seal error: %v", err)
	}

	handlers := greeter.Handlers{
		Ping:      func(context.Context) error { return nil },
		GetUser:   func(context.Context, greeter.GetUserRequest) (greeter.User, error) { return greeter.User{}, nil },
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) { return nil, nil },
	}
	if err := greeter.RegisterHandlers(server, handlers); err != nil {
		t.Fatalf("expected successful retry, got: %v", err)
	}

	if err := greeter.RegisterHandlers(server, handlers); err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestServerSealingLifecycle(t *testing.T) {
	server, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{
		Security: permissiveSecurity(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Calling Seal with missing handlers must return an error.
	if err := server.Seal(); err == nil {
		t.Fatal("expected error sealing empty server")
	}

	handlers := greeter.Handlers{
		Ping:      func(context.Context) error { return nil },
		GetUser:   func(context.Context, greeter.GetUserRequest) (greeter.User, error) { return greeter.User{}, nil },
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) { return nil, nil },
	}
	if err := greeter.RegisterHandlers(server, handlers); err != nil {
		t.Fatal(err)
	}

	// A subsequent Seal succeeds once handlers are complete.
	if err := server.Seal(); err != nil {
		t.Fatalf("expected successful seal, got: %v", err)
	}
	// Repeated successful Seal calls remain idempotent.
	if err := server.Seal(); err != nil {
		t.Fatalf("expected idempotent seal, got: %v", err)
	}

	// Registration after successful sealing is strictly rejected.
	if err := greeter.RegisterHandlers(server, handlers); err == nil {
		t.Fatal("expected error registering batch after sealing")
	}
}

func TestGeneratedHandlersRejectNil(t *testing.T) {
	cases := map[string]struct {
		handlers greeter.Handlers
	}{
		"get user": {
			handlers: greeter.Handlers{
				ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) { return nil, nil },
				Ping:      func(context.Context) error { return nil },
			},
		},
		"list users": {
			handlers: greeter.Handlers{
				GetUser: func(context.Context, greeter.GetUserRequest) (greeter.User, error) { return greeter.User{}, nil },
				Ping:    func(context.Context) error { return nil },
			},
		},
		"ping": {
			handlers: greeter.Handlers{
				GetUser:   func(context.Context, greeter.GetUserRequest) (greeter.User, error) { return greeter.User{}, nil },
				ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) { return nil, nil },
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			server, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if err := greeter.RegisterHandlers(server, tc.handlers); err == nil {
				t.Fatal("expected nil handler error")
			}
			if err := server.Seal(); err == nil {
				t.Fatal("nil handler was registered")
			}
		})
	}
}

func TestRegisterHandlersRejectsNilServer(t *testing.T) {
	if err := greeter.RegisterHandlers(nil, greeter.Handlers{}); err == nil {
		t.Fatal("expected nil server error")
	}
}

func TestServerSchemaProtection(t *testing.T) {
	schema := plugimpl.CopySchema(&greeter.Schema)
	server, err := plug.NewServer(schema, plug.ServerOptions{
		Security: permissiveSecurity(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mutate the caller's schema struct to ensure it is isolated.
	schema.Family[0] ^= 0xff
	schema.Methods[0].Name = "mutated_method"

	if err := greeter.RegisterHandlers(server, greeter.Handlers{
		Ping:      func(context.Context) error { return nil },
		GetUser:   func(context.Context, greeter.GetUserRequest) (greeter.User, error) { return greeter.User{}, nil },
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}

	// Test that an identical but copied schema value works for the client.
	clientSchema := plugimpl.CopySchema(&greeter.Schema)
	serverConn, clientConn := net.Pipe()
	go server.ServeConn(t.Context(), serverConn)
	client, err := plug.NewClient(t.Context(), clientConn, clientSchema, plug.ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if err := greeter.NewClient(client).Ping(t.Context()); err != nil {
		t.Fatalf("call failed: %v", err)
	}
	client.Close()
}

func TestServerBoundedShutdown(t *testing.T) {
	server, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{
		Security: permissiveSecurity(),
	})
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	handlerDone := make(chan struct{})

	if err := greeter.RegisterHandlers(server, greeter.Handlers{
		Ping: func(context.Context) error {
			close(started)
			<-release
			close(handlerDone)
			return nil
		},
		GetUser:   func(context.Context, greeter.GetUserRequest) (greeter.User, error) { return greeter.User{}, nil },
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}

	serverConn, clientConn := net.Pipe()
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.ServeConn(t.Context(), serverConn) }()

	client, err := plug.NewClient(t.Context(), clientConn, &greeter.Schema, plug.ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	typedClient := greeter.NewClient(client)

	// Start the call in the background.
	callDone := make(chan error, 1)
	go func() {
		callDone <- typedClient.Ping(t.Context())
	}()

	<-started

	// Test the timeout bounds on graceful shutdown.
	shutdownCtx, cancelShutdown := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancelShutdown()
	err = server.Shutdown(shutdownCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded during shutdown, got: %v", err)
	}

	// Release the pending handler.
	close(release)
	<-handlerDone

	// Wait for the background call to finish.
	if err := <-callDone; err != nil {
		t.Fatalf("background call failed: %v", err)
	}

	// Try a new call - should fail since shutdown was initiated and rejects new handlers.
	_, err = typedClient.GetUser(t.Context(), greeter.GetUserRequest{})
	if err == nil {
		t.Fatal("expected new call to fail after shutdown")
	}

	client.Close()
}

func TestServerShutdownCancelsHandlerContext(t *testing.T) {
	server, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{
		Security: permissiveSecurity(),
	})
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	if err := greeter.RegisterHandlers(server, greeter.Handlers{
		Ping: func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return nil
		},
		GetUser:   func(context.Context, greeter.GetUserRequest) (greeter.User, error) { return greeter.User{}, nil },
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}

	serverConn, clientConn := net.Pipe()
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.ServeConn(t.Context(), serverConn) }()
	client, err := plug.NewClient(t.Context(), clientConn, &greeter.Schema, plug.ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	typedClient := greeter.NewClient(client)

	callDone := make(chan error, 1)
	go func() {
		callDone <- typedClient.Ping(t.Context())
	}()
	<-started

	if err := server.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-callDone; err != nil {
		t.Fatalf("call after shutdown cancellation: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-serveDone; err != nil {
		t.Fatal(err)
	}
}

func TestServerConcurrentCallLimit(t *testing.T) {
	server, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{
		Limits: plug.Limits{
			MaxConcurrentCalls: 1,
		},
		Security: permissiveSecurity(),
	})
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	release := make(chan struct{})

	if err := greeter.RegisterHandlers(server, greeter.Handlers{
		GetUser: func(context.Context, greeter.GetUserRequest) (greeter.User, error) {
			close(started)
			<-release
			return greeter.User{}, nil
		},
		Ping:      func(context.Context) error { return nil },
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}

	serverConn, clientConn := net.Pipe()
	go server.ServeConn(t.Context(), serverConn)
	client, err := plug.NewClient(t.Context(), clientConn, &greeter.Schema, plug.ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	typedClient := greeter.NewClient(client)

	// The first call blocks indefinitely.
	go func() {
		typedClient.GetUser(t.Context(), greeter.GetUserRequest{})
	}()
	<-started

	// The second call should hit the MaxConcurrentCalls limit.
	if err := typedClient.Ping(t.Context()); err == nil {
		t.Fatal("expected concurrent call limit error")
	}

	close(release)
}

func TestServerErrorReporterPanicIsolation(t *testing.T) {
	reporterCalled := make(chan plug.ErrorEvent, 1)
	server, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{
		ReportError: func(ctx context.Context, event plug.ErrorEvent) {
			reporterCalled <- event
			panic("reporter panic should not crash the server")
		},
		Security: permissiveSecurity(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := greeter.RegisterHandlers(server, greeter.Handlers{
		Ping:      func(context.Context) error { return nil },
		GetUser:   func(context.Context, greeter.GetUserRequest) (greeter.User, error) { return greeter.User{}, nil },
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}

	serverConn, clientConn := net.Pipe()
	go server.ServeConn(t.Context(), serverConn)
	_, err = plug.NewClient(t.Context(), clientConn, &greeter.Schema, plug.ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Trigger a runtime error by sending garbage bytes to fail validation.
	clientConn.Write([]byte("garbage frame data that makes no sense"))

	select {
	case <-reporterCalled:
	case <-time.After(time.Second):
		t.Fatal("reporter was not called")
	}
}
