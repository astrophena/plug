// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"go.astrophena.name/plug/example/greeter"
)

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

func TestCancellationIsCached(t *testing.T) {
	server, calls, waiting := testServer(t)
	endpoint := serveTest(t, server, "tcp", "127.0.0.1:0")
	s, err := connect(t.Context(), endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.client.Close()
	if _, err := evaluate(t, s, "let pending = greeter.getUser(id: -1)"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	statements, err := parse("pending")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := s.execute(ctx, statements[0])
		done <- err
	}()
	select {
	case <-waiting:
		cancel()
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if _, err := evaluate(t, s, "pending"); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if calls.Load() != 1 {
		t.Fatal("canceled call was retried")
	}
	if _, err := evaluate(t, s, "greeter.ping()"); err != nil {
		t.Fatal(err)
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

func TestBindingLookupSnapshots(t *testing.T) {
	s := newSession(nil, &greeter.Schema)
	for _, tc := range []struct {
		source string
		want   string
	}{
		{
			"count([1])",
			"1",
		},
		{
			"let old = count; let saved = () => count([1]); let count = x => 99; count([1])",
			"99",
		},
		{
			"old([1]) + saved()",
			"2",
		},
	} {
		v, err := evaluate(t, s, tc.source)
		if err != nil || format(v, 0) != tc.want {
			t.Fatalf("%s = %s, %v; want %s", tc.source, format(v, 0), err, tc.want)
		}
	}
	if _, err := evaluate(t, s, "missing"); err == nil {
		t.Fatal("resolved an unknown name")
	}
	v, err := evaluate(t, s, "let missing = 7; missing")
	if err != nil || format(v, 0) != "7" {
		t.Fatalf("new binding = %s, %v", format(v, 0), err)
	}
}

func TestLiteralAndSumOwnership(t *testing.T) {
	for source, want := range map[string]string{
		"let unused = 1e10000; 42": "42",
		"false && 1e10000":         "false",
		"let n = () => 18446744073709551615; -n(); n() + 1; n()":        "18446744073709551615",
		"let xs = [18446744073709551615, 1]; xs | sum; xs | sum; xs[0]": "18446744073709551615",
		"((a,b,c,d,e,f,g,h,i) => a+i)(1,2,3,4,5,6,7,8,9)":               "10",
	} {
		t.Run(source, func(t *testing.T) {
			v, err := evaluate(t, newSession(nil, &greeter.Schema), source)
			if err != nil || format(v, 0) != want {
				t.Fatalf("got %s, %v, want %s", format(v, 0), err, want)
			}
		})
	}
}

func TestTailCalls(t *testing.T) {
	for name, source := range map[string]string{
		"self":     "let loop = (self, n) => n == 0 || self(self, n - 1); loop(loop, %d)",
		"mutual":   "let even = (even, odd, n) => n == 0 || odd(even, odd, n - 1); let odd = (even, odd, n) => n != 0 && even(even, odd, n - 1); even(even, odd, %d)",
		"pipeline": "let loop = (self, n) => n == 0 || (self | (f => f(f, n - 1))); loop(loop, %d)",
		"callback": "let loop = (self, n) => n == 0 || self(self, n - 1); [%d] | all(n => loop(loop, n))",
	} {
		t.Run(name, func(t *testing.T) {
			s := newSession(nil, &greeter.Schema)
			v, err := evaluate(t, s, fmt.Sprintf(source, 4*maxDepth))
			if err != nil || v.data != true {
				t.Fatalf("tail call = %v, %v", v.data, err)
			}
			if s.depth != 0 {
				t.Fatalf("evaluation retained depth %d", s.depth)
			}
		})
	}
}

func TestTailCallStack(t *testing.T) {
	s := newSession(nil, &greeter.Schema)
	var depths []int
	s.bind("probe", ready(value{
		data: &function{
			name: "probe",
			invoke: func(context.Context, []value) (value, error) {
				var pcs [128]uintptr
				depths = append(depths, runtime.Callers(0, pcs[:]))
				return value{
					data: true,
				}, nil
			},
		},
	}))
	_, err := evaluate(t, s, fmt.Sprintf("let loop = (self, n) => (n == 0 && probe()) || self(self, n - 1); loop(loop, 0); loop(loop, %d)", 4*maxDepth))
	if err != nil {
		t.Fatal(err)
	}
	if len(depths) != 2 || depths[1] > depths[0]+4 || depths[1] == 128 {
		t.Fatalf("tail calls grew the Go stack: %v", depths)
	}
}

func TestNonTailCallDepth(t *testing.T) {
	s := newSession(nil, &greeter.Schema)
	_, err := evaluate(t, s, fmt.Sprintf("let loop = (self, n) => n == 0 || !self(self, n - 1); loop(loop, %d)", 4*maxDepth))
	if err == nil || !strings.Contains(err.Error(), "evaluation is too deeply nested") {
		t.Fatalf("non-tail recursion = %v", err)
	}
	if s.depth != 0 {
		t.Fatalf("evaluation retained depth %d", s.depth)
	}
}

func TestTailCallCancellation(t *testing.T) {
	s := newSession(nil, &greeter.Schema)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var calls int
	s.bind("step", ready(value{
		data: &function{
			name: "step",
			invoke: func(context.Context, []value) (value, error) {
				calls++
				if calls == 3 {
					cancel()
				}
				return value{
					data: true,
				}, nil
			},
		},
	}))
	stmts, err := parse("let loop = self => step() && self(self); loop(loop)")
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range stmts {
		_, _, err = s.execute(ctx, st)
		if err != nil {
			break
		}
	}
	if !errors.Is(err, context.Canceled) || calls != 3 || s.depth != 0 {
		t.Fatalf("canceled tail calls: %v, calls=%d, depth=%d", err, calls, s.depth)
	}
}

func TestTailCallClosureCapture(t *testing.T) {
	s := newSession(nil, &greeter.Schema)
	v, err := evaluate(t, s, "let keep = x => () => x; let forward = (f, x) => f(x); let one = forward(keep, 1); one(); let two = forward(keep, 2); two(); one() + two()")
	if err != nil || format(v, 0) != "3" {
		t.Fatalf("captured values = %v, %v", v.data, err)
	}
}

func TestTailCallErrors(t *testing.T) {
	for source, want := range map[string]string{
		"false || 1":                     "1:7: operator \"||\" requires bool",
		"true && 1":                      "1:6: operator \"&&\" requires bool",
		"false || (true && 1)":           "1:16: operator \"&&\" requires bool",
		"let f = x => x;\nfalse || f(1)": "2:7: operator \"||\" requires bool",
		"let f = () => missing;\nf()":    "1:15: unknown name \"missing\"",
		"let f = () => 1 / 0;\nf()":      "1:17: division by zero",
	} {
		t.Run(source, func(t *testing.T) {
			_, err := evaluate(t, newSession(nil, &greeter.Schema), source)
			if err == nil || err.Error() != want {
				t.Fatalf("error = %v, want %q", err, want)
			}
		})
	}
}

func TestCallArgumentOrder(t *testing.T) {
	for source, tc := range map[string]struct {
		want  string
		calls int
	}{
		"pair(next(), next())": {
			want:  "12",
			calls: 2,
		},
		"pair(b: next(), a: next())": {
			want:  "21",
			calls: 2,
		},
		"next() | pair(b: next())": {
			want:  "12",
			calls: 2,
		},
		"pair(a: next(), a: next())": {},
		"pair(missing: next())":      {},
		"pair(a: next())":            {},
		"next() | pair(missing: next())": {
			calls: 1,
		},
	} {
		t.Run(source, func(t *testing.T) {
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
			v, err := evaluate(t, s, "let pair = (a, b) => a * 10 + b; "+source)
			if calls != tc.calls || (err != nil) != (tc.want == "") || err == nil && format(v, 0) != tc.want {
				t.Fatalf("argument evaluation: %s, %v, calls=%d", format(v, 0), err, calls)
			}
		})
	}
}
