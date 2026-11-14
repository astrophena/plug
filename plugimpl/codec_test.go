// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plugimpl

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"go.astrophena.name/base/testutil"
)

const maxListElements uint32 = 1 << 20

func TestDecodeBudgets(t *testing.T) {
	var (
		c    = ListCodec(StringCodec)
		want = []string{"first", "second"}
		data = mustEncode(t, c, want)
	)
	if _, err := Decode(c, data, maxListElements, 16); err == nil {
		t.Fatal("accepted values exceeding the allocation budget")
	}
	got, err := Decode(c, data, maxListElements, 128)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, got, want)

	if _, err := Decode(StringCodec, []byte{2, 'o', 'k'}, 0, 1); err == nil {
		t.Fatal("string exceeded the allocation budget")
	}
	v, err := Decode(StringCodec, []byte{2, 'o', 'k'}, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, v, "ok")

	lists := ListCodec(ListCodec(VoidCodec))
	if _, err := Decode(lists, []byte{2, 1, 1}, 3, 128); err == nil {
		t.Fatal("nested lists exceeded the total element budget")
	}
	if _, err := Decode(lists, []byte{2, 1, 1}, 4, 128); err != nil {
		t.Fatal(err)
	}

	optional := ListCodec(OptionalCodec(VoidCodec))
	values, err := Decode(optional, []byte{3}, 3, 16)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, values, []Option[Void]{{}, {}, {}})

	got, err = Decode(c, []byte{0}, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, got, []string{})
}

func TestCodecs(t *testing.T) {
	cases := map[string]func(*testing.T){
		"bool":  func(t *testing.T) { checkCodec(t, BoolCodec, true, []byte{1}) },
		"bytes": func(t *testing.T) { checkCodec(t, BytesCodec, []byte{0, 1, 2}, []byte{3, 0, 1, 2}) },
		"f64": func(t *testing.T) {
			checkCodec(t, F64Codec, math.Pi, []byte{0x18, 0x2d, 0x44, 0x54, 0xfb, 0x21, 9, 0x40})
		},
		"i32": func(t *testing.T) { checkCodec(t, I32Codec, int32(-1), []byte{1}) },
		"i64": func(t *testing.T) {
			checkCodec(t, I64Codec, int64(-1<<40), []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0x3f})
		},
		"string": func(t *testing.T) {
			checkCodec(t, StringCodec, "hello, мир", append([]byte{13}, []byte("hello, мир")...))
		},
		"u32": func(t *testing.T) { checkCodec(t, U32Codec, uint32(128), []byte{0x80, 1}) },
		"u64": func(t *testing.T) {
			checkCodec(t, U64Codec, uint64(1<<63), []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 1})
		},
		"void": func(t *testing.T) { checkCodec(t, VoidCodec, Void{}, nil) },
		"list": func(t *testing.T) { checkCodec(t, ListCodec(I32Codec), []int32{1, 2, 3}, []byte{3, 2, 4, 6}) },
		"optional": func(t *testing.T) {
			checkCodec(t, OptionalCodec(StringCodec), Some("value"), []byte{1, 5, 'v', 'a', 'l', 'u', 'e'})
		},
	}
	for name, run := range cases {
		t.Run(name, run)
	}
}

func TestDelimitedValueEncoding(t *testing.T) {
	e := &Encoder{
		maxListElements: maxListElements,
	}
	body := e.BeginRecord()
	e.Raw(bytes.Repeat([]byte{'x'}, 128))
	e.EndValue(body)
	if err := e.Err(); err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, e.Data()[:2], []byte{0x80, 0x01})

	e.data = e.data[:0]
	body = e.BeginSum(0x12345678)
	e.EndValue(body)
	want := []byte{0x78, 0x56, 0x34, 0x12, 0}
	testutil.AssertEqual(t, e.Data(), want)
}

func TestOptionalTailCompatibility(t *testing.T) {
	type record struct {
		ID    int64
		Note  Option[string]
		Label Option[string]
	}
	old := Codec[int64]{
		Encode: func(e *Encoder, id int64) {
			body := e.BeginRecord()
			e.I64(id)
			e.EndValue(body)
		},
		Decode: func(d *Decoder) int64 {
			body := d.BeginRecord()
			id := d.I64()
			d.FinishValue(body)
			return id
		},
	}
	next := Codec[record]{
		Encode: func(e *Encoder, v record) {
			body := e.BeginRecord()
			e.I64(v.ID)
			last := LastPresent(v.Note.Present(), v.Label.Present())
			if last >= 0 {
				OptionalCodec(StringCodec).Encode(e, v.Note)
			}
			if last >= 1 {
				OptionalCodec(StringCodec).Encode(e, v.Label)
			}
			e.EndValue(body)
		},
		Decode: func(d *Decoder) (v record) {
			body := d.BeginRecord()
			v.ID = d.I64()
			v.Note = OptionalCodec(StringCodec).Decode(d)
			v.Label = OptionalCodec(StringCodec).Decode(d)
			d.FinishValue(body)
			return v
		},
	}
	var (
		data = mustEncode(t, old, 42)
		want = record{
			ID: 42,
		}
	)
	testutil.AssertEqual(t, mustEncode(t, next, want), data)

	got, err := Decode(next, data, maxListElements, 0)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, got, want)

	id, err := Decode(old, mustEncode(t, next, record{
		ID:   42,
		Note: Some("extension"),
	}), maxListElements, 0)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, id, int64(42))

	checkCodec(t, next, record{
		ID:    42,
		Label: Some(""),
	}, []byte{4, 84, 0, 1, 0})
}

func TestCodecFailures(t *testing.T) {
	cases := map[string]struct {
		run  func() error
		want string
	}{
		"invalid bool": {
			run: func() error {
				_, err := Decode(BoolCodec, []byte{2}, 0, 0)
				return err
			},
			want: "invalid boolean",
		},
		"invalid UTF-8": {
			run: func() error {
				_, err := Decode(StringCodec, []byte{1, 0xff}, 0, 0)
				return err
			},
			want: "not valid UTF-8",
		},
		"encode invalid UTF-8": {
			run: func() error {
				_, err := Encode(StringCodec, string([]byte{0xff}), 0)
				return err
			},
			want: "not valid UTF-8",
		},
		"list limit": {
			run: func() error {
				_, err := Decode(ListCodec(VoidCodec), []byte{2}, 1, 0)
				return err
			},
			want: "maximum is 1",
		},
		"trailing data": {
			run: func() error {
				_, err := Decode(VoidCodec, []byte{1}, 0, 0)
				return err
			},
			want: "trailing bytes",
		},
		"truncated": {
			run: func() error {
				_, err := Decode(U64Codec, []byte{0x80}, 0, 0)
				return err
			},
			want: "unexpected EOF",
		},
		"truncated list": {
			run: func() error {
				_, err := Decode(ListCodec(U64Codec), []byte{2, 1}, maxListElements, 0)
				return err
			},
			want: "unexpected EOF",
		},
		"bounded record": {
			run: func() error {
				codec := Codec[Void]{
					Decode: func(d *Decoder) Void {
						body := d.BeginRecord()
						d.U64()
						d.FinishValue(body)
						return Void{}
					},
				}
				_, err := Decode(codec, []byte{1, 0x80, 0}, 0, 0)
				return err
			},
			want: "unexpected EOF",
		},
		"i32 overflow": {
			run: func() error {
				_, err := Decode(I32Codec, binary.AppendVarint(nil, int64(math.MaxInt32)+1), 0, 0)
				return err
			},
			want: "overflows i32",
		},
		"u32 overflow": {
			run: func() error {
				_, err := Decode(U32Codec, binary.AppendUvarint(nil, uint64(math.MaxUint32)+1), 0, 0)
				return err
			},
			want: "overflows u32",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.run()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func checkCodec[T any](t *testing.T, c Codec[T], v T, want []byte) {
	t.Helper()

	data := mustEncode(t, c, v)
	testutil.AssertEqual(t, string(data), string(want))

	got, err := Decode(c, want, maxListElements, 0)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, got, v)
}

func mustEncode[T any](t *testing.T, c Codec[T], v T) []byte {
	t.Helper()
	data, err := Encode(c, v, maxListElements)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func FuzzDecoder(f *testing.F) {
	f.Add([]byte{1, 'x'})
	f.Fuzz(func(t *testing.T, data []byte) {
		value, err := Decode(StringCodec, data, 100, 0)
		if err == nil && !utf8.ValidString(value) {
			t.Fatalf("decoded invalid UTF-8: %q", value)
		}
	})
}
