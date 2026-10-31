// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/example/greeter"
)

func benchContext(b *testing.B) context.Context {
	if c, ok := any(b).(interface{ Context() context.Context }); ok {
		return c.Context()
	}
	return context.Background()
}

func permissiveSecurityBench() plug.Security {
	return plug.Security{
		Authenticate: func(context.Context, net.Conn) (plug.Principal, error) {
			return plug.Principal{
				Subject: "rpc-bench",
			}, nil
		},
		Authorize: func(context.Context, plug.Principal, string, plug.MethodInfo, any) error {
			return nil
		},
	}
}

func setupServerBench(b *testing.B) *plug.Server {
	b.Helper()
	server, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{
		Security: permissiveSecurityBench(),
	})
	if err != nil {
		b.Fatal(err)
	}

	if err := greeter.RegisterHandlers(server, greeter.Handlers{
		GetUser: func(_ context.Context, request greeter.GetUserRequest) (greeter.User, error) {
			return greeter.User{
				ID:     request.ID,
				Name:   "bench-user",
				Email:  plug.Some("bench@example.test"),
				Labels: []string{"benchmark"},
				Role:   &greeter.Guest{},
			}, nil
		},
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) {
			return nil, nil
		},
		Ping: func(context.Context) error { return nil },
	}); err != nil {
		b.Fatal(err)
	}
	return server
}

func setupRoundTripBench(b *testing.B) *plug.Client {
	b.Helper()
	server := setupServerBench(b)
	serverConn, clientConn := net.Pipe()
	ctx, cancel := context.WithCancel(benchContext(b))
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.ServeConn(ctx, serverConn) }()
	client, err := plug.NewClient(ctx, clientConn, &greeter.Schema, plug.ClientOptions{})
	if err != nil {
		cancel()
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			b.Errorf("close client: %v", err)
		}
		cancel()
		select {
		case err := <-serveDone:
			if err != nil {
				b.Errorf("serve connection: %v", err)
			}
		case <-time.After(time.Second):
			b.Error("server did not stop")
		}
	})
	return client
}

func BenchmarkRoundTrip(b *testing.B) {
	client := greeter.NewClient(setupRoundTripBench(b))
	ctx := benchContext(b)
	req := greeter.GetUserRequest{
		ID: 42,
	}
	b.ReportAllocs()

	for b.Loop() {
		_, err := client.GetUser(ctx, req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPingRoundTrip(b *testing.B) {
	client := greeter.NewClient(setupRoundTripBench(b))
	ctx := benchContext(b)
	b.ReportAllocs()

	for b.Loop() {
		if err := client.Ping(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParallelRoundTrip(b *testing.B) {
	client := greeter.NewClient(setupRoundTripBench(b))
	ctx := benchContext(b)
	req := greeter.GetUserRequest{
		ID: 42,
	}
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := client.GetUser(ctx, req)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
