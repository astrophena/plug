// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/example/greeter"
)

func TestRPC(t *testing.T) {
	cancelStarted := make(chan struct{})
	canceled := make(chan struct{})
	unknown := &greeter.UnknownRole{
		ConstructorID: 0xfefefefe,
		Payload:       []byte{0, 1, 2},
	}
	unknownError := &greeter.UnknownLookupError{
		ConstructorID: 0xdeadbeef,
		Payload:       []byte("opaque error"),
	}
	client := startGreeter(t, func(ctx context.Context, request greeter.GetUserRequest) (greeter.User, error) {
		switch request.ID {
		case 0:
			return greeter.User{}, fmt.Errorf("database exploded")
		case 1:
			return greeter.User{}, fmt.Errorf("wrapped: %w", &greeter.NotFound{
				ID: request.ID,
			})
		case 2:
			panic("handler exploded")
		case 3:
			close(cancelStarted)
			<-ctx.Done()
			close(canceled)
			return greeter.User{}, ctx.Err()
		case 4:
			return greeter.User{
				Role: unknown,
			}, nil
		case 5:
			return greeter.User{}, unknownError
		default:
			return greeter.User{
				ID:     request.ID,
				Name:   fmt.Sprintf("user-%d", request.ID),
				Email:  plug.Some("user@example.test"),
				Labels: []string{"test", "plug"},
				Role: &greeter.Admin{
					Level: 7,
				},
			}, nil
		}
	})

	user, err := client.GetUser(t.Context(), greeter.GetUserRequest{
		ID: 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != 42 || user.Name != "user-42" || len(user.Labels) != 2 {
		t.Fatalf("user = %#v", user)
	}
	if email, ok := user.Email.Value(); !ok || email != "user@example.test" {
		t.Fatalf("email = %q, %t", email, ok)
	}
	if role, ok := user.Role.(*greeter.Admin); !ok || role.Level != 7 {
		t.Fatalf("role = %#v", user.Role)
	}

	user, err = client.GetUser(t.Context(), greeter.GetUserRequest{
		ID: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	role, ok := user.Role.(*greeter.UnknownRole)
	if !ok || role.ConstructorID != unknown.ConstructorID || !bytes.Equal(role.Payload, unknown.Payload) {
		t.Fatalf("unknown role = %#v", user.Role)
	}

	_, err = client.GetUser(t.Context(), greeter.GetUserRequest{
		ID: 1,
	})
	var notFound *greeter.NotFound
	if !errors.As(err, &notFound) || notFound.ID != 1 {
		t.Fatalf("error = %v, not found = %#v", err, notFound)
	}

	_, err = client.GetUser(t.Context(), greeter.GetUserRequest{ID: 5})
	var lookupError greeter.LookupError
	if !errors.As(err, &lookupError) {
		t.Fatalf("error = %v, want LookupError", err)
	}
	opaque, ok := lookupError.(*greeter.UnknownLookupError)
	if !ok || opaque.ConstructorID != unknownError.ConstructorID || !bytes.Equal(opaque.Payload, unknownError.Payload) {
		t.Fatalf("unknown error = %#v", lookupError)
	}

	_, err = client.GetUser(t.Context(), greeter.GetUserRequest{
		ID: 0,
	})
	assertRemoteError(t, err, plug.ErrorInternal, "internal server error")
	_, err = client.GetUser(t.Context(), greeter.GetUserRequest{
		ID: 2,
	})
	assertRemoteError(t, err, plug.ErrorInternal, "internal server error")

	ctx, cancel := context.WithCancel(t.Context())
	callDone := make(chan error, 1)
	go func() {
		_, err := client.GetUser(ctx, greeter.GetUserRequest{
			ID: 3,
		})
		callDone <- err
	}()
	select {
	case <-cancelStarted:
	case <-time.After(time.Second):
		t.Fatal("cancelable handler did not start")
	}
	cancel()
	if err := <-callDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled call error = %v", err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("server handler was not canceled")
	}
	if err := client.Ping(t.Context()); err != nil {
		t.Fatalf("ping after cancellation: %v", err)
	}
}

func TestClientDecodeBudget(t *testing.T) {
	cases := map[string]struct {
		role   greeter.Role
		err    error
		budget uint64
	}{
		"variant": {
			role: &greeter.Admin{
				Level: 7,
			},
			budget: 1,
		},
		"unknown variant": {
			role: &greeter.UnknownRole{
				ConstructorID: 0xfefefefe,
				Payload:       make([]byte, 64),
			},
			budget: 64,
		},
		"error": {
			err: &greeter.NotFound{
				ID: 7,
			},
			budget: 1,
		},
		"unknown error": {
			err: &greeter.UnknownLookupError{
				ConstructorID: 0xfefefefe,
				Payload:       make([]byte, 64),
			},
			budget: 64,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			server, err := configuredServer(func(context.Context, greeter.GetUserRequest) (greeter.User, error) {
				return greeter.User{
					Role: tc.role,
				}, tc.err
			})
			if err != nil {
				t.Fatal(err)
			}
			a, b := net.Pipe()
			done := make(chan error, 1)
			go func() { done <- server.ServeConn(t.Context(), a) }()
			client, err := plug.NewClient(t.Context(), b, &greeter.Schema, plug.ClientOptions{
				Limits: plug.Limits{
					MaxDecodeBytes: tc.budget,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			_, err = greeter.NewClient(client).GetUser(t.Context(), greeter.GetUserRequest{
				ID: 1,
			})
			if err == nil || !strings.Contains(err.Error(), "allocation limit") {
				t.Fatalf("decode error = %v, want allocation limit", err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConcurrentCallsAreMultiplexed(t *testing.T) {
	releaseSlow := make(chan struct{})
	fastFinished := make(chan struct{})
	client := startGreeter(t, func(_ context.Context, request greeter.GetUserRequest) (greeter.User, error) {
		if request.ID == 1 {
			<-releaseSlow
		} else {
			close(fastFinished)
		}
		return greeter.User{
			ID:   request.ID,
			Name: "ok",
			Role: &greeter.Guest{},
		}, nil
	})

	type result struct {
		user greeter.User
		err  error
	}
	slow := make(chan result, 1)
	go func() {
		user, err := client.GetUser(t.Context(), greeter.GetUserRequest{
			ID: 1,
		})
		slow <- result{
			user: user,
			err:  err,
		}
	}()
	fast := make(chan result, 1)
	go func() {
		user, err := client.GetUser(t.Context(), greeter.GetUserRequest{
			ID: 2,
		})
		fast <- result{
			user: user,
			err:  err,
		}
	}()

	select {
	case <-fastFinished:
	case <-time.After(time.Second):
		t.Fatal("fast handler did not start")
	}
	select {
	case got := <-fast:
		if got.err != nil || got.user.ID != 2 {
			t.Fatalf("fast call = %#v, %v", got.user, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("fast response was blocked by slow response")
	}
	close(releaseSlow)
	got := <-slow
	if got.err != nil || got.user.ID != 1 {
		t.Fatalf("slow call = %#v, %v", got.user, got.err)
	}
}

func TestCompressedRPC(t *testing.T) {
	name := strings.Repeat("compressible ", 200)
	observed := make(chan plug.CallEvent, 1)
	server, err := configuredServerWithOptions(func(_ context.Context, request greeter.GetUserRequest) (greeter.User, error) {
		return greeter.User{
			ID:   request.ID,
			Name: name,
			Role: &greeter.Guest{},
		}, nil
	}, plug.ServerOptions{
		Compression: true,
		ObserveCall: func(_ context.Context, event plug.CallEvent) {
			observed <- event
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	serverConn, clientConn := net.Pipe()
	served := make(chan error, 1)
	go func() { served <- server.ServeConn(t.Context(), serverConn) }()
	client, err := plug.NewClient(t.Context(), clientConn, &greeter.Schema, plug.ClientOptions{
		Compression: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	user, err := greeter.NewClient(client).GetUser(t.Context(), greeter.GetUserRequest{
		ID: 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != 42 || user.Name != name {
		t.Fatalf("user = %#v", user)
	}
	select {
	case event := <-observed:
		if event.PayloadSize <= len(name) {
			t.Errorf("payload size = %d, want more than name size %d", event.PayloadSize, len(name))
		}
		if !event.Compressed {
			t.Error("result was not reported as compressed")
		}
		if event.CompressedSize == 0 || event.CompressedSize >= event.PayloadSize {
			t.Errorf("compressed size = %d, payload size = %d", event.CompressedSize, event.PayloadSize)
		}
		if event.CompressionSavings != event.PayloadSize-event.CompressedSize {
			t.Errorf("compression savings = %d, want %d", event.CompressionSavings, event.PayloadSize-event.CompressedSize)
		}
	case <-time.After(time.Second):
		t.Fatal("call was not observed")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
}

func TestServeConnDoesNotWaitForCanceledHandler(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	handlerDone := make(chan struct{})
	server, err := configuredServer(func(context.Context, greeter.GetUserRequest) (greeter.User, error) {
		close(started)
		<-release
		close(handlerDone)
		return greeter.User{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	serverConn, clientConn := net.Pipe()
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.ServeConn(t.Context(), serverConn)
	}()
	client, err := plug.NewClient(t.Context(), clientConn, &greeter.Schema, plug.ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	callDone := make(chan error, 1)
	go func() {
		_, err := greeter.NewClient(client).GetUser(t.Context(), greeter.GetUserRequest{
			ID: 1,
		})
		callDone <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("server waited for a handler that ignored cancellation")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := server.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Shutdown() = %v; want context.Canceled while the handler is active", err)
	}

	close(release)
	released = true
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("released handler did not finish")
	}
	if err := <-callDone; !errors.Is(err, plug.ErrClientClosed) {
		t.Fatalf("call error = %v", err)
	}
	if err := server.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestInspectAndSchemaMismatch(t *testing.T) {
	server, err := configuredServer(nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("inspect", func(t *testing.T) {
		serverConn, clientConn := net.Pipe()
		serveDone := make(chan error, 1)
		go func() { serveDone <- server.ServeConn(t.Context(), serverConn) }()
		remote, err := plug.Inspect(t.Context(), clientConn)
		if err != nil {
			t.Fatal(err)
		}
		if remote.Fingerprint != greeter.Schema.Fingerprint || remote.Source != greeter.Schema.Source {
			t.Fatalf("remote schema = %#v", remote)
		}
		if err := <-serveDone; err != nil {
			t.Fatal(err)
		}
	})

	t.Run("mismatch", func(t *testing.T) {
		serverConn, clientConn := net.Pipe()
		serveDone := make(chan error, 1)
		go func() { serveDone <- server.ServeConn(t.Context(), serverConn) }()
		other := greeter.Schema
		other.Fingerprint[0] ^= 0xff
		_, err := plug.NewClient(t.Context(), clientConn, &other, plug.ClientOptions{
			StrictSchema: true,
		})
		var mismatch *plug.SchemaMismatchError
		if !errors.As(err, &mismatch) {
			t.Fatalf("error = %v", err)
		}
		if mismatch.Remote.Source != greeter.Schema.Source {
			t.Fatalf("remote source missing from mismatch")
		}
		if err := <-serveDone; err != nil {
			t.Fatal(err)
		}
	})

	t.Run("compatible fingerprint", func(t *testing.T) {
		serverConn, clientConn := net.Pipe()
		serveDone := make(chan error, 1)
		go func() { serveDone <- server.ServeConn(t.Context(), serverConn) }()
		other := greeter.Schema
		other.Fingerprint[0] ^= 0xff
		client, err := plug.NewClient(t.Context(), clientConn, &other, plug.ClientOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err := client.Close(); err != nil && !errors.Is(err, plug.ErrClientClosed) {
			t.Fatal(err)
		}
		if err := <-serveDone; err != nil {
			t.Fatal(err)
		}
	})
}

func TestNativePermissions(t *testing.T) {
	server, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{
		Security: plug.Security{
			Authorize: func(context.Context, plug.Principal, string, plug.MethodInfo, any) error {
				return nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := greeter.RegisterHandlers(server, greeter.Handlers{
		GetUser:   func(context.Context, greeter.GetUserRequest) (greeter.User, error) { return greeter.User{}, nil },
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) { return nil, nil },
		Ping:      func(context.Context) error { return nil },
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
	if err := typedClient.Ping(t.Context()); err != nil {
		t.Fatalf("public ping: %v", err)
	}
	_, err = typedClient.GetUser(t.Context(), greeter.GetUserRequest{
		ID: 42,
	})
	assertRemoteError(t, err, plug.ErrorUnauthenticated, "authenticated principal")
	if err := client.Close(); err != nil && !errors.Is(err, plug.ErrClientClosed) {
		t.Fatal(err)
	}
	if err := <-serveDone; err != nil {
		t.Fatal(err)
	}
}

func startGreeter(
	t *testing.T,
	getUser func(context.Context, greeter.GetUserRequest) (greeter.User, error),
) *greeter.Client {
	t.Helper()
	server, err := configuredServer(getUser)
	if err != nil {
		t.Fatal(err)
	}
	serverConn, clientConn := net.Pipe()
	ctx, cancel := context.WithCancel(t.Context())
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.ServeConn(ctx, serverConn) }()
	client, err := plug.NewClient(ctx, clientConn, &greeter.Schema, plug.ClientOptions{})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("close client: %v", err)
		}
		cancel()
		select {
		case err := <-serveDone:
			if err != nil {
				t.Errorf("serve connection: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("server did not stop")
		}
	})
	return greeter.NewClient(client)
}

func configuredServer(
	getUser func(context.Context, greeter.GetUserRequest) (greeter.User, error),
) (*plug.Server, error) {
	return configuredServerWithOptions(getUser, plug.ServerOptions{})
}

func configuredServerWithOptions(
	getUser func(context.Context, greeter.GetUserRequest) (greeter.User, error),
	options plug.ServerOptions,
) (*plug.Server, error) {
	options.Security = permissiveSecurity()
	server, err := plug.NewServer(&greeter.Schema, options)
	if err != nil {
		return nil, err
	}
	if getUser == nil {
		getUser = func(_ context.Context, request greeter.GetUserRequest) (greeter.User, error) {
			return greeter.User{
				ID:   request.ID,
				Name: "default",
				Role: &greeter.Guest{},
			}, nil
		}
	}
	if err := greeter.RegisterHandlers(server, greeter.Handlers{
		GetUser: getUser,
		ListUsers: func(_ context.Context, request greeter.ListUsersRequest) ([]greeter.User, error) {
			users := make([]greeter.User, request.Limit)
			for i := range users {
				users[i] = greeter.User{
					ID:   greeter.UserID(i + 1),
					Name: "listed",
					Role: &greeter.Guest{},
				}
			}
			return users, nil
		},
		Ping: func(context.Context) error { return nil },
	}); err != nil {
		return nil, err
	}
	return server, nil
}

func permissiveSecurity() plug.Security {
	return plug.Security{
		Authenticate: func(context.Context, net.Conn) (plug.Principal, error) {
			return plug.Principal{
				Subject: "rpc-test",
			}, nil
		},
		Authorize: func(context.Context, plug.Principal, string, plug.MethodInfo, any) error {
			return nil
		},
	}
}

func assertRemoteError(t *testing.T, err error, code plug.ErrorCode, message string) {
	t.Helper()
	var remote *plug.RemoteError
	if !errors.As(err, &remote) || remote.Code != code || !strings.Contains(remote.Message, message) {
		t.Fatalf("error = %v, remote = %#v", err, remote)
	}
}

func TestConcurrentClientClose(t *testing.T) {
	client := startGreeter(t, nil)
	var wait sync.WaitGroup
	for range 10 {
		wait.Go(func() {
			if err := client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				t.Errorf("close client: %v", err)
			}
		})
	}
	wait.Wait()
}
