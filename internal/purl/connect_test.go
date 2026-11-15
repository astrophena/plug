// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"go.astrophena.name/base/version"
	"go.astrophena.name/plug"
	"go.astrophena.name/plug/example/greeter"
	"go.astrophena.name/plug/plugupgrade"
)

func TestReconnect(t *testing.T) {
	s, calls := testSession(t)
	if _, err := evaluate(t, s, "let ping = greeter.ping; let user = greeter.getUser(id: 1); user"); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		client := s.client
		if err := client.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := evaluate(t, s, "ping()"); err != nil {
			t.Fatal(err)
		}
		if s.client == client {
			t.Fatal("kept the closed client")
		}
		if v, err := evaluate(t, s, "user.name"); err != nil || format(v, 0) != `"Alice"` {
			t.Fatalf("saved binding = %v, %v", v, err)
		}
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("calls = %d; want 3", got)
	}
}

func TestReconnectFailure(t *testing.T) {
	s, calls := testSession(t)
	if err := s.client.Close(); err != nil {
		t.Fatal(err)
	}
	var (
		dial = s.dial
		want = errors.New("offline")
	)
	s.dial = func(context.Context) (net.Conn, error) { return nil, want }
	if _, err := evaluate(t, s, "greeter.ping()"); !errors.Is(err, want) {
		t.Fatalf("call = %v; want %v", err, want)
	}
	if calls.Load() != 0 {
		t.Fatal("called server after reconnect failed")
	}

	s.dial = dial
	if _, err := evaluate(t, s, "greeter.ping()"); err != nil {
		t.Fatal(err)
	}
}

func TestDisconnectDuringCall(t *testing.T) {
	server, calls, waiting := testServer(t)
	endpoint := serveTest(t, server, "tcp", "127.0.0.1:0")
	s, err := connect(t.Context(), endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.client.Close() })

	done := make(chan error, 1)
	go func() {
		_, err := evaluate(t, s, "greeter.getUser(id: -1)")
		done <- err
	}()
	<-waiting
	if err := s.client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("call succeeded after disconnect")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d; want 1 (no retry)", got)
	}
	if _, err := evaluate(t, s, "greeter.ping()"); err != nil {
		t.Fatal(err)
	}
}

func TestDialerRejectsUserAgentOverride(t *testing.T) {
	for _, scheme := range []string{"https", "wss"} {
		if _, err := dialer(scheme+"://example.test/plug", http.Header{"User-Agent": []string{"custom"}}); !errors.Is(err, errOverrideUA) {
			t.Fatalf("got %v; want %v", err, errOverrideUA)
		}
	}
}

func TestConnectWithoutServerCompression(t *testing.T) {
	server, calls, _ := newTestServer(t, false)
	endpoint := serveTest(t, server, "tcp", "127.0.0.1:0")
	s, err := connect(t.Context(), endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.client.Close() })

	if _, err := evaluate(t, s, "greeter.ping()"); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d; want 1", got)
	}
}

func TestUpgradeConnection(t *testing.T) {
	t.Parallel()

	for _, scheme := range []string{"http", "https", "ws", "wss"} {
		t.Run(scheme, func(t *testing.T) {
			t.Parallel()

			server, _, _ := testServer(t)
			upgrade, err := plugupgrade.NewServer(server, plugupgrade.Options{})
			if err != nil {
				t.Fatal(err)
			}
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/plug" || r.Header.Get("Authorization") != "Bearer test" {
					http.Error(w, "denied", http.StatusForbidden)
					return
				}
				if got, want := r.Header.Get("User-Agent"), version.UserAgent(); got != want {
					http.Error(w, fmt.Sprintf("User-Agent = %q; want %q", got, want), http.StatusBadRequest)
					return
				}
				upgrade.ServeHTTP(w, r)
			})
			web := httptest.NewUnstartedServer(handler)
			web.Config.ErrorLog = log.New(io.Discard, "", 0)
			if scheme == "https" || scheme == "wss" {
				web.StartTLS()
			} else {
				web.Start()
			}
			t.Cleanup(web.Close)
			t.Cleanup(func() {
				if err := upgrade.Shutdown(context.Background()); err != nil {
					t.Error(err)
				}
			})
			endpoint := web.URL
			if scheme == "ws" || scheme == "wss" {
				endpoint = strings.Replace(endpoint, "http", "ws", 1)
			}
			s, err := connect(t.Context(), endpoint+"/plug", http.Header{"Authorization": []string{"Bearer test"}})
			if scheme == "https" || scheme == "wss" {
				if err == nil {
					s.client.Close()
					t.Fatal("HTTPS accepted an untrusted certificate")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.client.Close() })
			for range 2 {
				if _, err := evaluate(t, s, "greeter.getUser(id: 1)"); err != nil {
					t.Fatal(err)
				}
				if err := s.client.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestUnixConnection(t *testing.T) {
	server, _, _ := testServer(t)
	// Keep socket paths below sockaddr_un's small platform-dependent limit.
	dir, err := os.MkdirTemp("", "purl-")
	if err != nil {
		t.Fatal(err)
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "s%?# ")
	serveTest(t, server, "unix", path)
	u := url.URL{
		Scheme: "unix",
		Path:   path,
	}
	for name, endpoint := range map[string]string{"path": path, "URL": u.String()} {
		t.Run(name, func(t *testing.T) {
			s, err := connect(t.Context(), endpoint, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer s.client.Close()
			if _, err := evaluate(t, s, "greeter.ping()"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeEndpoints(t *testing.T) {
	for _, endpoint := range []string{
		"/run/overseer/socket",
		"localhost:9000",
		"127.0.0.1:9000",
		"[::1]:9000",
		"[fe80::1%eth0]:9000",
	} {
		t.Run(endpoint, func(t *testing.T) {
			if _, err := dialer(endpoint, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := dialer(endpoint, http.Header{"Authorization": []string{"Bearer test"}}); err == nil {
				t.Fatal("accepted HTTP headers for a native endpoint")
			}
		})
	}
}

func TestInvalidEndpoints(t *testing.T) {
	for _, endpoint := range []string{
		"",
		"localhost",
		"localhost:",
		"localhost:-1",
		"localhost:65536",
		"localhost:9000/path",
		"localhost:9000?query",
		"::1:9000",
		"/",
		"tcp://host",
		"tcp://host:9000/path",
		"tcp://host:9000?q=x",
		"unix://run/path",
		"unix:///",
		"unix:relative",
		"https:///plug",
		"https://user:password@example.test/plug",
		"https://example.test/plug#fragment",
	} {
		t.Run(endpoint, func(t *testing.T) {
			if _, err := dialer(endpoint, nil); err == nil {
				t.Fatal("accepted invalid endpoint")
			}
		})
	}
}

func testServer(t *testing.T) (*plug.Server, *atomic.Int64, <-chan struct{}) {
	t.Helper()
	return newTestServer(t, true)
}

func newTestServer(t *testing.T, compression bool) (*plug.Server, *atomic.Int64, <-chan struct{}) {
	t.Helper()
	var (
		calls   = new(atomic.Int64)
		waiting = make(chan struct{})
	)
	s, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{
		Compression: compression,
		Security: plug.Security{
			Authenticate: func(context.Context, net.Conn) (plug.Principal, error) {
				return plug.Principal{
					Subject: "tester",
				}, nil
			},
			Authorize: func(context.Context, plug.Principal, string, plug.MethodInfo, any) error { return nil },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := greeter.RegisterHandlers(s, greeter.Handlers{
		Ping: func(context.Context) error {
			calls.Add(1)
			return nil
		},
		GetUser: func(ctx context.Context, req greeter.GetUserRequest) (greeter.User, error) {
			calls.Add(1)
			if req.ID == 0 {
				return greeter.User{}, &greeter.NotFound{
					ID: req.ID,
				}
			}
			if req.ID == -1 {
				close(waiting)
				<-ctx.Done()
				return greeter.User{}, ctx.Err()
			}
			return greeter.User{
				ID:     req.ID,
				Name:   "Alice",
				Labels: []string{"home", "work"},
				Role: &greeter.Admin{
					Level: 3,
				},
				Email: plug.Some("alice@example.test"),
			}, nil
		},
		ListUsers: func(ctx context.Context, req greeter.ListUsersRequest) ([]greeter.User, error) {
			calls.Add(1)
			return []greeter.User{
				{
					ID:     1,
					Name:   "Alice",
					Labels: []string{},
					Role:   &greeter.Guest{},
				},
				{
					ID:     2,
					Name:   "Bob",
					Labels: []string{"work"},
					Role: &greeter.Admin{
						Level: 2,
					},
				},
			}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	return s, calls, waiting
}

func serveTest(t *testing.T, server *plug.Server, network, address string) string {
	t.Helper()
	listener, err := net.Listen(network, address)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
		if err := server.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if network == "unix" {
		return "unix://" + listener.Addr().String()
	}
	return "tcp://" + listener.Addr().String()
}

func testSession(t *testing.T) (*session, *atomic.Int64) {
	t.Helper()
	server, calls, _ := testServer(t)
	var (
		wg sync.WaitGroup
		s  = newSession(nil, &greeter.Schema)
	)
	s.dial = func(context.Context) (net.Conn, error) {
		c, peer := net.Pipe()
		wg.Go(func() { server.ServeConn(t.Context(), peer) })
		return c, nil
	}
	t.Cleanup(func() {
		if s.client != nil {
			s.client.Close()
		}
		if err := server.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
		wg.Wait()
	})
	if err := s.reconnect(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s, calls
}
