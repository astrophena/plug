// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/example/greeter"
)

func TestServerCallbacks(t *testing.T) {
	var (
		observed = make(chan plug.CallEvent, 1)
		reported = make(chan plug.ErrorEvent, 1)
	)
	server, err := configuredServerWithOptions(func(_ context.Context, req greeter.GetUserRequest) (greeter.User, error) {
		switch req.ID {
		case 1:
			return greeter.User{}, errors.New("database failed")
		case 2:
			panic("handler failed")
		default:
			return greeter.User{Role: &greeter.Guest{}}, nil
		}
	}, plug.ServerOptions{
		ObserveCall: func(_ context.Context, event plug.CallEvent) {
			observed <- event
			panic("observer failed")
		},
		ReportError: func(_ context.Context, event plug.ErrorEvent) {
			reported <- event
			panic("reporter failed")
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	serverConn, clientConn := net.Pipe()
	served := make(chan error, 1)
	go func() { served <- server.ServeConn(t.Context(), serverConn) }()
	client, err := plug.NewClient(t.Context(), clientConn, &greeter.Schema, plug.ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		client.Close()
		if err := <-served; err != nil {
			t.Error(err)
		}
	})

	for _, id := range []greeter.UserID{0, 1, 2, 0} {
		_, err := greeter.NewClient(client).GetUser(t.Context(), greeter.GetUserRequest{ID: id})
		outcome := plug.CallSucceeded
		if id != 0 {
			outcome = plug.CallInternalError
		}
		if (err != nil) != (id != 0) {
			t.Fatalf("GetUser(%d) error = %v", id, err)
		}

		var event plug.CallEvent
		select {
		case event = <-observed:
		case <-time.After(time.Second):
			t.Fatal("call was not observed")
		}
		if event.Outcome != outcome || event.Duration <= 0 || event.HandlerDuration <= 0 {
			t.Fatalf("GetUser(%d) event = %+v", id, event)
		}
		if id != 0 {
			select {
			case report := <-reported:
				if report.Err == nil || event.Err == nil {
					t.Fatal("failed call has no diagnostic error")
				}
			case <-time.After(time.Second):
				t.Fatal("failed call was not reported")
			}
		}
	}
}

func TestServerAtomicRegistration(t *testing.T) {
	server, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{
		Security: permissiveSecurity(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := greeter.RegisterHandlers(server, greeter.Handlers{
		GetUser: func(context.Context, greeter.GetUserRequest) (greeter.User, error) { return greeter.User{}, nil },
		Ping:    func(context.Context) error { return nil },
	}); err == nil {
		t.Fatal("expected error registering nil handler")
	}

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	err = server.ServeConn(t.Context(), serverConn)
	if err == nil {
		t.Fatal("served an empty server")
	}
	if !strings.Contains(err.Error(), "missing handlers") || !strings.Contains(err.Error(), "getUser") {
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

	for range 2 {
		if err := server.Seal(); err != nil {
			t.Fatal(err)
		}
	}
	if err := greeter.RegisterHandlers(server, handlers); err == nil {
		t.Fatal("registered handlers after sealing")
	}
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

	callDone := make(chan error, 1)
	go func() {
		callDone <- typedClient.Ping(t.Context())
	}()

	<-started

	shutdownCtx, cancelShutdown := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancelShutdown()
	err = server.Shutdown(shutdownCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded during shutdown, got: %v", err)
	}

	close(release)
	<-handlerDone

	if err := <-callDone; err != nil {
		t.Fatalf("background call failed: %v", err)
	}

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
