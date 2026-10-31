// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"go.astrophena.name/plug/example/greeter"
)

func TestHelperErrors(t *testing.T) {
	for source, want := range map[string]string{
		`find(1, x => true)`:          "expected a list",
		`any([], 1)`:                  "expected a function",
		`all([], (a, b) => true)`:     "one argument",
		`find([1], x => 1)`:           "predicate must return bool",
		`any([1], x => 1)`:            "predicate must return bool",
		`all([1], x => 1)`:            "predicate must return bool",
		`uniqueBy([1], x => () => x)`: "key must support equality",
		`groupBy([1], x => () => x)`:  "key must support equality",
		`sortBy([1, "two"], x => x)`:  "cannot compare",
		`sum(1)`:                      "expected a list",
		`sum([1, "two"])`:             "expected numbers",
		`contains(1, "a")`:            "expected two strings",
		`startsWith("a", nil)`:        "expected two strings",
		`endsWith(false, "a")`:        "expected two strings",
		`age(1)`:                      "timestamp string",
		`age("not a timestamp")`:      "age:",
		`assert(1, "stop")`:           "bool condition",
		`assert(true, nil)`:           "string message",
	} {
		t.Run(source, func(t *testing.T) {
			_, err := evaluate(t, newSession(nil, &greeter.Schema), source)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("got %v, want %q", err, want)
			}
		})
	}
}

func TestHelperCallbacks(t *testing.T) {
	for name, want := range map[string]string{
		"map":      "[1, 2, 3]",
		"filter":   "[2]",
		"sortBy":   "[1, 2, 3]",
		"find":     "2",
		"any":      "true",
		"all":      "false",
		"uniqueBy": "[1, 2, 3]",
		"groupBy":  "3",
	} {
		t.Run(name, func(t *testing.T) {
			s := newSession(nil, &greeter.Schema)
			var calls []int64
			s.bind("probe", ready(value{
				data: &function{
					name:   "probe",
					params: []string{"x"},
					invoke: func(ctx context.Context, args []value) (value, error) {
						x, _ := number(args[0])
						calls = append(calls, int64(x))
						switch name {
						case "filter", "find", "any":
							return value{
								data: x == 2,
							}, nil
						case "all":
							return value{
								data: x < 2,
							}, nil
						}
						return args[0], nil
					},
				},
			}))
			source := fmt.Sprintf("%s([1, 2, 3], probe)", name)
			visits := []int64{1, 2}
			if name != "find" && name != "any" && name != "all" {
				visits = append(visits, 3)
			}
			if name == "groupBy" {
				source += " | count"
			}
			v, err := evaluate(t, s, source)
			if err != nil || format(v, 0) != want || !slices.Equal(calls, visits) {
				t.Fatalf("got %s, %v, visits %v", format(v, 0), err, calls)
			}
		})
	}
}

func TestHelperCancellation(t *testing.T) {
	for _, name := range []string{"map", "filter", "sortBy", "find", "any", "all", "uniqueBy", "groupBy"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fn := value{
				data: &function{
					params: []string{"x"},
					invoke: func(context.Context, []value) (value, error) {
						cancel()
						return value{
							data: name == "all",
						}, nil
					},
				},
			}
			_, err := builtins()[name].invoke(ctx, []value{
				{
					data: []value{integer(1), integer(2)},
				},
				fn,
			})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("got %v, want cancellation", err)
			}
		})
	}
}

func TestAge(t *testing.T) {
	s := newSession(nil, &greeter.Schema)
	for _, stamp := range []string{"0001-01-01T00:00:00Z", "2026-07-01T09:00:00.123456789+02:00", "9999-12-31T23:59:59Z"} {
		t.Run(stamp, func(t *testing.T) {
			parsed, err := time.Parse(time.RFC3339Nano, stamp)
			if err != nil {
				t.Fatal(err)
			}
			before := time.Now().Unix() - parsed.Unix() - 1
			v, err := evaluate(t, s, fmt.Sprintf("age(%q)", stamp))
			after := time.Now().Unix() - parsed.Unix() + 1
			seconds, ok := number(v)
			if err != nil || !ok || seconds < float64(before) || seconds > float64(after) {
				t.Fatalf("age = %v, %v; want [%d, %d]", v.data, err, before, after)
			}
		})
	}
}
