// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plugimpl

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

const maxListElements uint32 = 1 << 20

func TestTruncatedListAllocation(t *testing.T) {
	codec := ListCodec(Codec[[4096]byte]{
		Decode: func(d *Decoder) [4096]byte {
			d.Bool()
			return [4096]byte{}
		},
	})
	payload := binary.AppendUvarint(nil, 1024)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := Decode(codec, payload, maxListElements, 0)
	runtime.ReadMemStats(&after)
	if err == nil {
		t.Fatal("accepted a list without its elements")
	}
	if n := after.TotalAlloc - before.TotalAlloc; n > 1<<20 {
		t.Fatalf("truncated list allocated %d bytes", n)
	}
}

func TestDecodeBudgets(t *testing.T) {
	codec := ListCodec(StringCodec)
	payload := mustEncode(t, codec, []string{"first", "second"}, maxListElements)
	if _, err := Decode(codec, payload, maxListElements, 16); err == nil {
		t.Fatal("accepted values exceeding the allocation budget")
	}
	values, err := Decode(codec, payload, maxListElements, 128)
	if err != nil || !reflect.DeepEqual(values, []string{"first", "second"}) {
		t.Fatalf("decode within budget = %v, %v", values, err)
	}

	if _, err := Decode(StringCodec, []byte{2, 'o', 'k'}, 0, 1); err == nil {
		t.Fatal("string exceeded the allocation budget")
	}
	if value, err := Decode(StringCodec, []byte{2, 'o', 'k'}, 0, 2); err != nil || value != "ok" {
		t.Fatalf("decoder retained an old budget or error: %q, %v", value, err)
	}

	lists := ListCodec(ListCodec(VoidCodec))
	if _, err := Decode(lists, []byte{2, 1, 1}, 3, 128); err == nil {
		t.Fatal("nested lists exceeded the total element budget")
	}
	if _, err := Decode(lists, []byte{2, 1, 1}, 4, 128); err != nil {
		t.Fatal(err)
	}

	optional := ListCodec(OptionalCodec(VoidCodec))
	if values, err := Decode(optional, []byte{3}, 3, 16); err != nil || len(values) != 3 {
		t.Fatalf("zero-byte elements = %v, %v", values, err)
	}
	if values, err := Decode(codec, []byte{0}, 0, 1); err != nil || values == nil || len(values) != 0 {
		t.Fatalf("empty list = %v, %v", values, err)
	}
}

func TestPrimitiveCodecs(t *testing.T) {
	cases := map[string]func(*testing.T){
		"bool":   func(t *testing.T) { roundTrip(t, BoolCodec, true) },
		"bytes":  func(t *testing.T) { roundTrip(t, BytesCodec, []byte{0, 1, 2}) },
		"f64":    func(t *testing.T) { roundTrip(t, F64Codec, math.Pi) },
		"i32":    func(t *testing.T) { roundTrip(t, I32Codec, int32(-42)) },
		"i64":    func(t *testing.T) { roundTrip(t, I64Codec, int64(-1<<40)) },
		"string": func(t *testing.T) { roundTrip(t, StringCodec, "hello, мир") },
		"u32":    func(t *testing.T) { roundTrip(t, U32Codec, uint32(42)) },
		"u64":    func(t *testing.T) { roundTrip(t, U64Codec, uint64(1<<63)) },
		"void":   func(t *testing.T) { roundTrip(t, VoidCodec, Void{}) },
	}
	for name, run := range cases {
		t.Run(name, run)
	}
}

func TestCompactCodecEncoding(t *testing.T) {
	limits := maxListElements
	cases := map[string]struct {
		encode func() []byte
		want   []byte
	}{
		"i32 negative": {
			encode: func() []byte { return mustEncode(t, I32Codec, int32(-1), limits) },
			want:   []byte{1},
		},
		"i64 positive": {
			encode: func() []byte { return mustEncode(t, I64Codec, int64(63), limits) },
			want:   []byte{126},
		},
		"u32 boundary": {
			encode: func() []byte { return mustEncode(t, U32Codec, uint32(128), limits) },
			want:   []byte{0x80, 0x01},
		},
		"u64 small": {
			encode: func() []byte { return mustEncode(t, U64Codec, uint64(127), limits) },
			want:   []byte{0x7f},
		},
		"string length": {
			encode: func() []byte { return mustEncode(t, StringCodec, "x", limits) },
			want:   []byte{1, 'x'},
		},
		"list count": {
			encode: func() []byte { return mustEncode(t, ListCodec(VoidCodec), []Void{{}, {}}, limits) },
			want:   []byte{2},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.encode(); !bytes.Equal(got, tc.want) {
				t.Fatalf("encoded value = %x, want %x", got, tc.want)
			}
		})
	}
}

func TestDelimitedValueEncoding(t *testing.T) {
	encoder := &Encoder{
		maxListElements: maxListElements,
	}
	body := encoder.BeginRecord()
	encoder.Raw(bytes.Repeat([]byte{'x'}, 128))
	encoder.EndValue(body)
	if err := encoder.Err(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoder.Data()[:2], []byte{0x80, 0x01}) {
		t.Fatalf("record length = %x, want 8001", encoder.Data()[:2])
	}

	encoder.data = encoder.data[:0]
	body = encoder.BeginSum(0x12345678)
	encoder.EndValue(body)
	want := []byte{0x78, 0x56, 0x34, 0x12, 0}
	if !bytes.Equal(encoder.Data(), want) {
		t.Fatalf("encoded sum = %x, want %x", encoder.Data(), want)
	}
}

func TestDerivedCodecs(t *testing.T) {
	limits := uint32(10)
	option := OptionalCodec(StringCodec)
	encoded, err := Encode(option, Some("value"), limits)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(option, encoded, limits, 0)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := decoded.Value(); !ok || value != "value" {
		t.Fatalf("decoded option = %q, %t", value, ok)
	}

	list := ListCodec(I32Codec)
	values, err := Decode(list, mustEncode(t, list, []int32{1, 2, 3}, limits), limits, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 3 || values[2] != 3 {
		t.Fatalf("decoded list = %v", values)
	}
}

func TestOptionalTailCompatibility(t *testing.T) {
	type oldRecord struct {
		ID int64
	}
	type newRecord struct {
		ID    int64
		Note  Option[string]
		Label Option[string]
	}
	oldCodec := Codec[oldRecord]{
		Encode: func(encoder *Encoder, value oldRecord) {
			body := encoder.BeginRecord()
			encoder.I64(value.ID)
			encoder.EndValue(body)
		},
		Decode: func(decoder *Decoder) (value oldRecord) {
			body := decoder.BeginRecord()
			value.ID = decoder.I64()
			decoder.FinishValue(body)
			return value
		},
	}
	newCodec := Codec[newRecord]{
		Encode: func(encoder *Encoder, value newRecord) {
			body := encoder.BeginRecord()
			encoder.I64(value.ID)
			last := LastPresent(value.Note.Present(), value.Label.Present())
			if last >= 0 {
				OptionalCodec(StringCodec).Encode(encoder, value.Note)
			}
			if last >= 1 {
				OptionalCodec(StringCodec).Encode(encoder, value.Label)
			}
			encoder.EndValue(body)
		},
		Decode: func(decoder *Decoder) (value newRecord) {
			body := decoder.BeginRecord()
			value.ID = decoder.I64()
			value.Note = OptionalCodec(StringCodec).Decode(decoder)
			value.Label = OptionalCodec(StringCodec).Decode(decoder)
			decoder.FinishValue(body)
			return value
		},
	}
	limits := maxListElements
	oldEncoded := mustEncode(t, oldCodec, oldRecord{
		ID: 42,
	}, limits)
	newNoneEncoded := mustEncode(t, newCodec, newRecord{
		ID: 42,
	}, limits)
	if !bytes.Equal(oldEncoded, newNoneEncoded) {
		t.Fatalf("absent optional tail encoded as %x, want %x", newNoneEncoded, oldEncoded)
	}
	fromOld, err := Decode(newCodec, mustEncode(t, oldCodec, oldRecord{
		ID: 42,
	}, limits), limits, 0)
	if err != nil {
		t.Fatal(err)
	}
	if fromOld.ID != 42 || fromOld.Note.Present() || fromOld.Label.Present() {
		t.Fatalf("new decoder read old value as %#v", fromOld)
	}
	fromNew, err := Decode(oldCodec, mustEncode(t, newCodec, newRecord{
		ID:   42,
		Note: Some("extension"),
	}, limits), limits, 0)
	if err != nil {
		t.Fatal(err)
	}
	if fromNew.ID != 42 {
		t.Fatalf("old decoder read new value as %#v", fromNew)
	}

	gap := newRecord{
		ID:    42,
		Label: Some(""),
	}
	encoded := mustEncode(t, newCodec, gap, limits)
	if want := []byte{4, 84, 0, 1, 0}; !bytes.Equal(encoded, want) {
		t.Fatalf("optional gap encoded as %x, want %x", encoded, want)
	}
	roundTrip(t, newCodec, gap)
}

func TestCodecFailures(t *testing.T) {
	cases := map[string]struct {
		decode func() error
		want   string
	}{
		"invalid bool": {
			decode: func() error {
				_, err := Decode(BoolCodec, []byte{2}, 0, 0)
				return err
			},
			want: "invalid boolean",
		},
		"invalid UTF-8": {
			decode: func() error {
				_, err := Decode(StringCodec, []byte{1, 0xff}, 0, 0)
				return err
			},
			want: "not valid UTF-8",
		},
		"encode invalid UTF-8": {
			decode: func() error {
				_, err := Encode(StringCodec, string([]byte{0xff}), 0)
				return err
			},
			want: "not valid UTF-8",
		},
		"list limit": {
			decode: func() error {
				_, err := Decode(ListCodec(VoidCodec), []byte{2}, 1, 0)
				return err
			},
			want: "maximum is 1",
		},
		"trailing data": {
			decode: func() error {
				_, err := Decode(VoidCodec, []byte{1}, 0, 0)
				return err
			},
			want: "trailing bytes",
		},
		"truncated": {
			decode: func() error {
				_, err := Decode(U64Codec, []byte{0x80}, 0, 0)
				return err
			},
			want: "unexpected EOF",
		},
		"bounded record": {
			decode: func() error {
				codec := Codec[Void]{
					Decode: func(decoder *Decoder) Void {
						body := decoder.BeginRecord()
						decoder.U64()
						decoder.FinishValue(body)
						return Void{}
					},
				}
				_, err := Decode(codec, []byte{1, 0x80, 0}, 0, 0)
				return err
			},
			want: "unexpected EOF",
		},
		"i32 overflow": {
			decode: func() error {
				_, err := Decode(I32Codec, binary.AppendVarint(nil, int64(math.MaxInt32)+1), 0, 0)
				return err
			},
			want: "overflows i32",
		},
		"u32 overflow": {
			decode: func() error {
				_, err := Decode(U32Codec, binary.AppendUvarint(nil, uint64(math.MaxUint32)+1), 0, 0)
				return err
			},
			want: "overflows u32",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.decode()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func roundTrip[T any](t *testing.T, codec Codec[T], value T) {
	t.Helper()
	limits := maxListElements
	encoded := mustEncode(t, codec, value, limits)
	decoded, err := Decode(codec, encoded, limits, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, value) {
		t.Fatalf("decoded = %v, want %v", decoded, value)
	}
}

func mustEncode[T any](t *testing.T, codec Codec[T], value T, limits uint32) []byte {
	t.Helper()
	encoded, err := Encode(codec, value, limits)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
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
