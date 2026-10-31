// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"bytes"
	"context"
	bin "encoding/binary"
	"errors"
	"math"
	"math/big"
	"slices"
	"strings"
	"testing"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/example/greeter"
	"go.astrophena.name/plug/plugimpl"
)

func TestScalarCodecs(t *testing.T) {
	s := newSession(nil, &greeter.Schema)
	cases := map[string]struct {
		value  value
		encode func(*plugimpl.Encoder)
	}{
		"bool": {
			value{
				data: true,
			},
			func(e *plugimpl.Encoder) { e.Bool(true) },
		},
		"string": {
			value{
				data: "héllo",
			},
			func(e *plugimpl.Encoder) { e.String("héllo") },
		},
		"bytes": {
			value{
				data: []byte{0, 255},
			},
			func(e *plugimpl.Encoder) { e.Bytes([]byte{0, 255}) },
		},
		"i32": {
			integer(math.MinInt32),
			func(e *plugimpl.Encoder) { e.I32(math.MinInt32) },
		},
		"i64": {
			integer(math.MinInt64),
			func(e *plugimpl.Encoder) { e.I64(math.MinInt64) },
		},
		"u32": {
			integer(math.MaxUint32),
			func(e *plugimpl.Encoder) { e.U32(math.MaxUint32) },
		},
		"u64": {
			value{
				data: new(big.Int).SetUint64(math.MaxUint64),
			},
			func(e *plugimpl.Encoder) { e.U64(math.MaxUint64) },
		},
		"f64": {
			value{
				data: 1.5,
			},
			func(e *plugimpl.Encoder) { e.F64(1.5) },
		},
		"void": {
			value{},
			func(*plugimpl.Encoder) {},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			codec := s.codec(plug.TypeExpr{
				Kind: plug.TypeExprPrimitive,
				Name: name,
			})
			got, err := plugimpl.Encode(codec, tc.value, 10)
			if err != nil {
				t.Fatal(err)
			}
			encoder := plugimpl.PooledEncoder(10)
			defer plugimpl.ReleaseEncoder(encoder)
			tc.encode(encoder)
			if !bytes.Equal(got, encoder.Data()) {
				t.Fatalf("got %x, want %x", got, encoder.Data())
			}
			decoded, err := plugimpl.Decode(codec, got, 10, 0)
			if err != nil || !equal(decoded, tc.value) {
				t.Fatalf("decoded %v: %v", decoded, err)
			}
		})
	}
}

func TestOptionalTailEncoding(t *testing.T) {
	s := newSession(nil, &greeter.Schema)
	fields := []plug.FieldInfo{
		{
			Name: "required",
			Type: plug.TypeExpr{
				Kind: plug.TypeExprPrimitive,
				Name: "i64",
			},
		},
		{
			Name: "first",
			Type: plug.TypeExpr{
				Kind:     plug.TypeExprPrimitive,
				Name:     "string",
				Optional: true,
			},
		},
		{
			Name: "last",
			Type: plug.TypeExpr{
				Kind:     plug.TypeExprPrimitive,
				Name:     "string",
				Optional: true,
			},
		},
	}
	codec := s.fieldsCodec(fields)
	for _, last := range []value{
		{},
		{
			data: "yes",
		},
	} {
		r := &record{
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
					value: last,
				},
			},
		}
		payload, err := plugimpl.Encode(codec, value{
			data: r,
		}, 10)
		if err != nil {
			t.Fatal(err)
		}
		encoder := plugimpl.PooledEncoder(10)
		encoder.I64(1)
		if last.data != nil {
			encoder.Bool(false)
			encoder.Bool(true)
			encoder.String("yes")
		}
		want := bytes.Clone(encoder.Data())
		plugimpl.ReleaseEncoder(encoder)
		if !bytes.Equal(payload, want) {
			t.Fatalf("got %x, want %x", payload, want)
		}
		v, err := plugimpl.Decode(codec, payload, 10, 0)
		if err != nil || !equal(v, value{
			data: r,
		}) {
			t.Fatalf("got %v, %v", v, err)
		}
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
