// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"context"
	"errors"
	"math"
	"math/big"
	"testing"

	"go.astrophena.name/plug/example/greeter"
)

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

func TestGroupEquality(t *testing.T) {
	s := newSession(nil, &greeter.Schema)
	v, err := evaluate(t, s, `[
        [9223372036854775808, 9223372036854775808.0],
        [nil, nil],
        [true, true],
        ["same", "same"],
        [bytes("same"), bytes("same")],
        [{a: 1}, {a: 1.0}],
        [[1], [1.0]],
        [1e308 * 1e308, 1e308 * 1e308],
        [-1e308 * 1e308, -1e308 * 1e308],
        [0.5, 0.5]
    ]`)
	if err != nil {
		t.Fatal(err)
	}
	pairs := v.data.([]value)
	var (
		keys []value
		xs   []value
	)
	for i := range 40 {
		keys = append(keys, integer(int64(i)))
		xs = append(xs, integer(int64(i)))
	}
	for _, pair := range pairs {
		first := pair.data.([]value)[0]
		keys = append(keys, first)
		xs = append(xs, first)
	}
	for i := range 40 {
		xs = append(xs, value{
			data: float64(i),
		})
	}
	for _, pair := range pairs {
		xs = append(xs, pair.data.([]value)[1])
	}
	xs = append(xs, value{
		data: math.Copysign(0, -1),
	})

	for _, name := range []string{"uniqueBy", "groupBy"} {
		t.Run(name, func(t *testing.T) {
			fn := &function{
				params: []string{"x"},
				invoke: func(_ context.Context, args []value) (value, error) { return args[0], nil },
			}
			v, err := groupList(t.Context(), name, xs, fn)
			if err != nil {
				t.Fatal(err)
			}
			got := v.data.([]value)
			if len(got) != len(keys) {
				t.Fatalf("got %d keys, want %d", len(got), len(keys))
			}
			for i, v := range got {
				if name == "groupBy" {
					r := v.data.(*record)
					key, _ := r.lookup("key")
					items, _ := r.lookup("items")
					n := 2
					if i == 0 {
						n++
					}
					if len(items.data.([]value)) != n {
						t.Fatalf("group %d has wrong size", i)
					}
					v = key
				}
				if !equal(v, keys[i]) {
					t.Fatalf("key %d changed value or order", i)
				}
			}
			if !equal(xs[0], integer(0)) || len(xs) != 2*len(keys)+1 {
				t.Fatal("changed the input")
			}
		})
	}
}

func TestIndexedGroupCancellation(t *testing.T) {
	for _, name := range []string{"uniqueBy", "groupBy"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			xs := make([]value, 40)
			for i := range xs {
				xs[i] = integer(int64(i))
			}
			fn := &function{
				params: []string{"x"},
				invoke: func(_ context.Context, args []value) (value, error) {
					if args[0].data.(*big.Int).Int64() == 39 {
						cancel()
					}
					return args[0], nil
				},
			}
			if _, err := groupList(ctx, name, xs, fn); !errors.Is(err, context.Canceled) {
				t.Fatalf("last callback cancellation = %v", err)
			}
		})
	}
}

func TestJSONTextEncoding(t *testing.T) {
	const text = "\"\\\n\t\x00<>&\u2028\u2029\xff"
	v := value{
		data: &record{
			ctor: "tag",
			fields: []field{
				{name: text, value: value{data: text}},
				{name: "bytes", value: value{data: []byte{0, 1, 2}}},
			},
		},
	}
	const want = `{"type":"tag","\"\\\n\t\u0000\u003c\u003e\u0026\u2028\u2029�":"\"\\\n\t\u0000\u003c\u003e\u0026\u2028\u2029�","bytes":"AAEC"}`
	if got, err := marshalJSON(v); err != nil || got != want {
		t.Fatalf("JSON = %q, %v; want %q", got, err, want)
	}
}
