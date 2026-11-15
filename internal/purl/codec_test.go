// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	bin "encoding/binary"
	"math"
	"math/big"
	"slices"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/plug"
	"go.astrophena.name/plug/example/greeter"
	"go.astrophena.name/plug/plugimpl"
)

func TestCodecs(t *testing.T) {
	s := newSession(nil, &greeter.Schema)
	cases := map[string]struct {
		v    value
		wire []byte
	}{
		"bool": {
			value{
				data: true,
			},
			[]byte{1},
		},
		"string": {
			value{
				data: "héllo",
			},
			[]byte{6, 'h', 0xc3, 0xa9, 'l', 'l', 'o'},
		},
		"bytes": {
			value{
				data: []byte{0, 255},
			},
			[]byte{2, 0, 255},
		},
		"i32": {
			integer(math.MinInt32),
			[]byte{0xff, 0xff, 0xff, 0xff, 0x0f},
		},
		"i64": {
			integer(math.MinInt64),
			[]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 1},
		},
		"u32": {
			integer(math.MaxUint32),
			[]byte{0xff, 0xff, 0xff, 0xff, 0x0f},
		},
		"u64": {
			value{
				data: new(big.Int).SetUint64(math.MaxUint64),
			},
			[]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 1},
		},
		"f64": {
			value{
				data: 1.5,
			},
			[]byte{0, 0, 0, 0, 0, 0, 0xf8, 0x3f},
		},
		"void": {},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := s.codec(plugimpl.PrimitiveType(name))
			data, err := plugimpl.Encode(c, tc.v, 10)
			if err != nil {
				t.Fatal(err)
			}
			testutil.AssertEqual(t, string(data), string(tc.wire))

			got, err := plugimpl.Decode(c, tc.wire, 10, 0)
			if err != nil {
				t.Fatal(err)
			}
			if !equal(got, tc.v) {
				t.Fatalf("decoded %v; want %v", got, tc.v)
			}
		})
	}
}

func TestOptionalTail(t *testing.T) {
	var (
		s = newSession(nil, &greeter.Schema)
		c = s.fieldsCodec([]plug.FieldInfo{
			{
				Name: "required",
				Type: plugimpl.PrimitiveType("i64"),
			},
			{
				Name: "first",
				Type: plugimpl.OptionalType(plugimpl.PrimitiveType("string")),
			},
			{
				Name: "last",
				Type: plugimpl.OptionalType(plugimpl.PrimitiveType("string")),
			},
		})
	)
	for name, tc := range map[string]struct {
		last value
		wire []byte
	}{
		"absent": {
			wire: []byte{2},
		},
		"gap": {
			value{
				data: "yes",
			},
			[]byte{2, 0, 1, 3, 'y', 'e', 's'},
		},
	} {
		t.Run(name, func(t *testing.T) {
			v := value{
				data: &record{
					fields: []field{
						{
							name:  "required",
							value: integer(1),
						},
						{
							name: "first",
						},
						{
							name:  "last",
							value: tc.last,
						},
					},
				},
			}
			data, err := plugimpl.Encode(c, v, 10)
			if err != nil {
				t.Fatal(err)
			}
			testutil.AssertEqual(t, data, tc.wire)

			got, err := plugimpl.Decode(c, tc.wire, 10, 0)
			if err != nil {
				t.Fatal(err)
			}
			if !equal(got, v) {
				t.Fatalf("decoded %v; want %v", got, v)
			}
		})
	}
}

func TestCodecLimitsAndMalformedValues(t *testing.T) {
	s := newSession(nil, &greeter.Schema)
	element := plug.TypeExpr{
		Kind: plug.TypeExprPrimitive,
		Name: "bool",
	}
	codec := s.codec(plug.TypeExpr{
		Kind:    plug.TypeExprList,
		Element: &element,
	})
	if _, err := plugimpl.Encode(codec, value{
		data: []value{
			{
				data: true,
			},
			{
				data: false,
			},
		},
	}, 1); err == nil {
		t.Fatal("ignored encode list limit")
	}
	if _, err := plugimpl.Decode(codec, []byte{2, 1, 0}, 1, 0); err == nil {
		t.Fatal("ignored decode list limit")
	}
	if _, err := plugimpl.Decode(codec, []byte{1, 2}, 10, 0); err == nil {
		t.Fatal("accepted malformed bool")
	}
	if _, err := plugimpl.Decode(codec, []byte{1}, 10, 0); err == nil {
		t.Fatal("accepted truncated list")
	}
	role := s.codec(plug.TypeExpr{
		Kind: plug.TypeExprNamed,
		Name: "Role",
	})
	if _, err := plugimpl.Decode(role, []byte{0, 0, 0, 0, 0}, 10, 0); err == nil {
		t.Fatal("accepted unknown constructor")
	}
	for _, name := range []string{"i64", "string", "bytes"} {
		codec := scalarCodec(name)
		if _, err := plugimpl.Decode(codec, []byte{0}, 10, 1); err == nil {
			t.Fatalf("%s ignored dynamic value allocation", name)
		}
	}
	guest := s.types["Role"].Constructors[0]
	payload := bin.LittleEndian.AppendUint32(nil, guest.ID)
	payload = append(payload, 0)
	if _, err := plugimpl.Decode(role, payload, 10, 1); err == nil {
		t.Fatal("ignored dynamic record allocation")
	}
}

func TestRawMethodsAreRejected(t *testing.T) {
	metadata := greeter.Schema
	metadata.Methods = slices.Clone(metadata.Methods)
	metadata.Methods[0].Response = plug.TypeExpr{
		Kind: plug.TypeExprPrimitive,
		Name: "raw",
	}
	s := newSession(nil, &metadata)
	_, err := evaluate(t, s, metadata.Methods[0].Name+"(id: 1)")
	if err == nil || !strings.Contains(err.Error(), "raw streams are not supported") {
		t.Fatal(err)
	}
}
