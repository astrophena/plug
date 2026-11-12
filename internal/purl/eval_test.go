// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/example/greeter"
)

func testServer(t *testing.T) (*plug.Server, *atomic.Int64, <-chan struct{}) {
	t.Helper()
	return newTestServer(t, true)
}

func newTestServer(t *testing.T, compression bool) (*plug.Server, *atomic.Int64, <-chan struct{}) {
	t.Helper()
	calls := new(atomic.Int64)
	waiting := make(chan struct{})
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
	endpoint := serveTest(t, server, "tcp", "127.0.0.1:0")
	s, err := connect(t.Context(), endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.client.Close(); err != nil {
			t.Error(err)
		}
	})
	return s, calls
}

func evaluate(t *testing.T, s *session, source string) (value, error) {
	t.Helper()
	statements, err := parse(source)
	if err != nil {
		return value{}, err
	}
	var v value
	for _, st := range statements {
		v, _, err = s.execute(t.Context(), st)
		if err != nil {
			return value{}, err
		}
	}
	return v, nil
}

func TestExpressions(t *testing.T) {
	cases := map[string]struct {
		source, want string
	}{
		"precedence": {
			"1 + 2 * 3",
			"7",
		},
		"negative": {
			"-5 + 2",
			"-3",
		},
		"division": {
			"7 / 2",
			"3",
		},
		"floats": {
			"7 / 2.0",
			"3.5",
		},
		"large integer": {
			"18446744073709551615 + 1",
			"18446744073709551616",
		},
		"mixed comparison": {
			"9007199254740993 > 9007199254740992.0",
			"true",
		},
		"short circuit": {
			"false && missing() || true",
			"true",
		},
		"lambda": {
			"let f = (a, b) => a + b; f(b: 2, a: 3)",
			"5",
		},
		"closure": {
			"let x = 1; let f = () => x; let x = 2; f()",
			"1",
		},
		"snapshot": {
			"let x = 1; let y = x; let x = 2; y",
			"1",
		},
		"last value": {
			"3; let saved = _; 4; saved",
			"3",
		},
		"pipe": {
			"[1, 2, 3] | map(x => x * 2) | filter(x => x > 2) | take(1)",
			"[4]",
		},
		"bare pipe": {
			"[1, 2, 3] | count",
			"3",
		},
		"sort": {
			"[3, 1, 2, 1] | sortBy(x => x)",
			"[1, 1, 2, 3]",
		},
		"list index": {
			"[10, 20][1]",
			"20",
		},
		"record": {
			"{a: 1, b: [2]}.b[0]",
			"2",
		},
		"JSON keys": {
			`parseJSON("{\"odd key\": 42}")["odd key"]`,
			"42",
		},
		"nil equality": {
			"nil == null",
			"true",
		},
		"JSON encoding": {
			`toJSON({a: [1, null, true]})`,
			`"{\"a\":[1,null,true]}"`,
		},
		"unicode": {
			`count("héllo")`,
			"5",
		},
		"multiline": {
			"[\n1,\n2,\n] |\nmap(x => x + 1)",
			"[2, 3]",
		},
		"next line pipe": {
			"[1,2]\n | take(1)",
			"[1]",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := newSession(nil, &greeter.Schema)
			v, err := evaluate(t, s, tc.source)
			if err != nil {
				t.Fatal(err)
			}
			if got := format(v, 0); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestEvaluationErrors(t *testing.T) {
	cases := map[string]string{
		"1 / 0":                          "division by zero",
		"[1][-1]":                        "index out of range",
		"1()":                            "not callable",
		"1 | count()":                    "expected a list",
		"count()":                        "missing argument",
		"count(1, 2)":                    "too many positional",
		"count(items: [], items: [])":    "duplicate argument",
		"{x: 1, x: 2}":                   "duplicate field",
		"1 && true":                      "requires bool",
		"false || 1":                     "requires bool",
		"[1] | filter(x => x)":           "must return bool",
		`parseJSON("{} {}")`:             "trailing data",
		`parseJSON("{\"x\":1,\"x\":2}")`: "duplicate JSON field",
		"greeter.getUser(1)":             "unknown argument",
		"greeter.getUser()":              "missing argument",
		"greeter.getUser(id: 9223372036854775808)": "out of range",
		"greeter.listUsers(limit: -1)":             "out of range",
	}
	for source, want := range cases {
		t.Run(source, func(t *testing.T) {
			_, err := evaluate(t, newSession(nil, &greeter.Schema), source)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("got %v, want %q", err, want)
			}
		})
	}
}

func TestDynamicCalls(t *testing.T) {
	s, calls := testSession(t)
	for _, source := range []string{
		"let user = greeter.getUser(id: 42)",
		"let fetch = () => greeter.getUser(id: 42)",
	} {
		if _, err := evaluate(t, s, source); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("defining a binding sent a request")
	}
	for _, source := range []string{"user.name", "user.name", "fetch().name", "fetch().name"} {
		v, err := evaluate(t, s, source)
		if err != nil {
			t.Fatal(err)
		}
		if v.data != "Alice" {
			t.Fatalf("got %v", v.data)
		}
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("got %d calls, want 3", got)
	}
	v, err := evaluate(t, s, "greeter.listUsers(limit: 2) | map(u => u.name)")
	if err != nil {
		t.Fatal(err)
	}
	if got := format(v, 0); got != `["Alice", "Bob"]` {
		t.Fatal(got)
	}
	v, err = evaluate(t, s, "greeter.ping()")
	if err != nil {
		t.Fatal(err)
	}
	if got := format(v, 0); got != "void" {
		t.Fatal(got)
	}
}

func TestCachedApplicationError(t *testing.T) {
	s, calls := testSession(t)
	if _, err := evaluate(t, s, "123; let missing = greeter.getUser(id: 0)"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		_, err := evaluate(t, s, "missing")
		var application *appError
		if !errors.As(err, &application) {
			t.Fatalf("got %v", err)
		}
		if got := application.value.data.(*record).ctor; got != "notFound" {
			t.Fatal(got)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("got %d calls, want 1", got)
	}
	v, err := evaluate(t, s, "_")
	if err != nil || format(v, 0) != "123" {
		t.Fatalf("last result: %v, %v", v, err)
	}
}

func TestPipeEvaluatesOnce(t *testing.T) {
	s := newSession(nil, &greeter.Schema)
	var calls int
	s.bind("next", ready(value{
		data: &function{
			name: "next",
			invoke: func(context.Context, []value) (value, error) {
				calls++
				return integer(int64(calls)), nil
			},
		},
	}))
	v, err := evaluate(t, s, "next() | (x => x + x)")
	if err != nil || format(v, 0) != "2" || calls != 1 {
		t.Fatalf("got %v, %v, calls=%d", v, err, calls)
	}
}
