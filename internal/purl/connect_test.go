// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
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
