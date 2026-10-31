// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"go.astrophena.name/plug/example/greeter"
)

func TestTailCalls(t *testing.T) {
	for name, source := range map[string]string{
		"self":     "let loop = (self, n) => n == 0 || self(self, n - 1); loop(loop, 100000)",
		"mutual":   "let even = (even, odd, n) => n == 0 || odd(even, odd, n - 1); let odd = (even, odd, n) => n != 0 && even(even, odd, n - 1); even(even, odd, 100000)",
		"pipeline": "let loop = (self, n) => n == 0 || (self | (f => f(f, n - 1))); loop(loop, 100000)",
		"callback": "let loop = (self, n) => n == 0 || self(self, n - 1); [100000] | all(n => loop(loop, n))",
	} {
		t.Run(name, func(t *testing.T) {
			s := newSession(nil, &greeter.Schema)
			v, err := evaluate(t, s, source)
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
	_, err := evaluate(t, s, "let loop = (self, n) => (n == 0 && probe()) || self(self, n - 1); loop(loop, 0); loop(loop, 100000)")
	if err != nil {
		t.Fatal(err)
	}
	if len(depths) != 2 || depths[1] > depths[0]+4 || depths[1] == 128 {
		t.Fatalf("tail calls grew the Go stack: %v", depths)
	}
}

func TestNonTailCallDepth(t *testing.T) {
	s := newSession(nil, &greeter.Schema)
	_, err := evaluate(t, s, "let loop = (self, n) => n == 0 || !self(self, n - 1); loop(loop, 100000)")
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
				if calls == 1000 {
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
	if !errors.Is(err, context.Canceled) || calls != 1000 || s.depth != 0 {
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
