// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plugimpl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"slices"
	"sync"
	"unicode/utf8"
)

const maxRetainedBufferSize = 64 * 1024

// DefaultMaxDecodeBytes bounds allocations made while decoding one payload.
const DefaultMaxDecodeBytes uint64 = 64 << 20

// Codec encodes and decodes values of type T.
type Codec[T any] struct {
	// Encode appends a value to an encoder.
	Encode func(*Encoder, T)
	// Decode reads a value from a decoder.
	Decode func(*Decoder) T
}

// ErrorCodec encodes and decodes a method's declared errors. Its zero value
// declares no errors.
type ErrorCodec struct {
	// Encode recognizes and encodes a declared error.
	Encode func(*Encoder, error) (constructorID uint32, ok bool)
	// Identify recognizes a declared error and returns its constructor ID
	// without encoding the payload.
	Identify func(error) (constructorID uint32, ok bool)
	// Decode reconstructs a declared error from its constructor ID.
	Decode func(*Decoder, uint32) error
}

// Encoder encodes schema values and records the first error.
type Encoder struct {
	data            []byte
	err             error
	maxListElements uint32
}

// Err returns the first encoding error.
func (e *Encoder) Err() error { return e.err }

// Data returns the encoded payload. The result aliases encoder storage.
func (e *Encoder) Data() []byte { return e.data }

// Fail records err if no earlier encoding error occurred.
func (e *Encoder) Fail(err error) {
	if e.err == nil {
		e.err = err
	}
}

// Unsupported records an unsupported generated sum variant.
func (e *Encoder) Unsupported(typeName string, value any) {
	e.Fail(fmt.Errorf("plug: unsupported %s variant %T", typeName, value))
}

// Bool encodes a canonical boolean.
func (e *Encoder) Bool(value bool) {
	if value {
		e.data = append(e.data, 1)
		return
	}
	e.data = append(e.data, 0)
}

// I32 encodes a signed 32-bit integer.
func (e *Encoder) I32(value int32) {
	e.data = binary.AppendVarint(e.data, int64(value))
}

// U32 encodes an unsigned 32-bit integer.
func (e *Encoder) U32(value uint32) {
	e.data = binary.AppendUvarint(e.data, uint64(value))
}

// I64 encodes a signed 64-bit integer.
func (e *Encoder) I64(value int64) {
	e.data = binary.AppendVarint(e.data, value)
}

// U64 encodes an unsigned 64-bit integer.
func (e *Encoder) U64(value uint64) {
	e.data = binary.AppendUvarint(e.data, value)
}

// F64 encodes a 64-bit floating point value.
func (e *Encoder) F64(value float64) {
	e.data = binary.LittleEndian.AppendUint64(e.data, math.Float64bits(value))
}

// String encodes a UTF-8 string.
func (e *Encoder) String(value string) {
	if e.err != nil {
		return
	}
	if !utf8.ValidString(value) {
		e.Fail(fmt.Errorf("plug: string is not valid UTF-8"))
		return
	}
	if uint64(len(value)) > math.MaxUint32 {
		e.Fail(fmt.Errorf("plug: string has %d bytes; maximum is %d", len(value), uint64(math.MaxUint32)))
		return
	}
	e.U32(uint32(len(value)))
	e.data = append(e.data, value...)
}

// Bytes encodes a length-prefixed byte sequence.
func (e *Encoder) Bytes(value []byte) {
	if e.err != nil {
		return
	}
	if uint64(len(value)) > math.MaxUint32 {
		e.Fail(fmt.Errorf("plug: byte sequence has %d bytes; maximum is %d", len(value), uint64(math.MaxUint32)))
		return
	}
	e.U32(uint32(len(value)))
	e.data = append(e.data, value...)
}

// Raw appends bytes without a length prefix. It is used to preserve an unknown
// constructor body across schema generations.
func (e *Encoder) Raw(value []byte) {
	if e.err == nil {
		e.data = append(e.data, value...)
	}
}

// BeginRecord starts a length-delimited record body and returns a marker for
// EndValue.
func (e *Encoder) BeginRecord() int {
	e.data = append(e.data, 0)
	return len(e.data)
}

// BeginSum starts a length-delimited sum body with constructorID and returns a
// marker for EndValue.
func (e *Encoder) BeginSum(constructorID uint32) int {
	e.data = binary.LittleEndian.AppendUint32(e.data, constructorID)
	return e.BeginRecord()
}

// EndValue finishes a body started by BeginRecord or BeginSum.
func (e *Encoder) EndValue(bodyStart int) {
	if e.err != nil {
		return
	}
	if bodyStart < 1 || bodyStart > len(e.data) {
		e.Fail(fmt.Errorf("plug: invalid value marker %d", bodyStart))
		return
	}
	size := len(e.data) - bodyStart
	if uint64(size) > math.MaxUint32 {
		e.Fail(fmt.Errorf("plug: value body has %d bytes; maximum is %d", size, uint64(math.MaxUint32)))
		return
	}
	var prefix [binary.MaxVarintLen32]byte
	prefixSize := binary.PutUvarint(prefix[:], uint64(size))
	if prefixSize > 1 {
		oldSize := len(e.data)
		e.data = append(e.data, make([]byte, prefixSize-1)...)
		copy(e.data[bodyStart+prefixSize-1:], e.data[bodyStart:oldSize])
	}
	copy(e.data[bodyStart-1:bodyStart-1+prefixSize], prefix[:prefixSize])
}

// Decoder decodes a frame payload. The first error stops further decoding.
type Decoder struct {
	data            []byte
	offset          int
	limit           int
	err             error
	maxListElements uint32
	elementsLeft    uint32
	bytesLeft       uint64
}

// Reserve charges n bytes to the payload's allocation budget. It returns false
// and stops decoding when the budget is exhausted or an earlier error occurred.
func (d *Decoder) Reserve(n uint64) bool {
	if d.err != nil {
		return false
	}
	if n > d.bytesLeft {
		d.fail(errors.New("plug: decoded allocation limit exceeded"))
		return false
	}
	d.bytesLeft -= n
	return true
}

// Reserve charges space for n values of type T before their allocation.
func Reserve[T any](d *Decoder, n int) bool {
	size := uint64(reflect.TypeFor[T]().Size())
	if n < 0 || size > 0 && uint64(n) > d.bytesLeft/size {
		d.fail(errors.New("plug: decoded allocation limit exceeded"))
		return false
	}
	return d.Reserve(uint64(n) * size)
}

// Err returns the first decoding error.
func (d *Decoder) Err() error { return d.err }

// Done verifies that decoding succeeded and consumed the complete payload.
func (d *Decoder) Done() error {
	if d.err != nil {
		return d.err
	}
	if d.offset != len(d.data) {
		return fmt.Errorf("plug: payload has %d trailing bytes", len(d.data)-d.offset)
	}
	return nil
}

// UnknownConstructor records an unknown sum constructor ID.
func (d *Decoder) UnknownConstructor(typeName string, id uint32) {
	d.fail(fmt.Errorf("plug: unknown %s constructor 0x%08x", typeName, id))
}

// Bool decodes a canonical boolean.
func (d *Decoder) Bool() bool {
	value := d.take(1)
	if value == nil {
		return false
	}
	switch value[0] {
	case 0:
		return false
	case 1:
		return true
	default:
		d.fail(fmt.Errorf("plug: invalid boolean tag %d", value[0]))
		return false
	}
}

// I32 decodes a signed 32-bit integer.
func (d *Decoder) I32() int32 {
	value := d.varint()
	if d.err != nil {
		return 0
	}
	if value < math.MinInt32 || value > math.MaxInt32 {
		d.fail(fmt.Errorf("plug: signed integer %d overflows i32", value))
		return 0
	}
	return int32(value)
}

// U32 decodes an unsigned 32-bit integer.
func (d *Decoder) U32() uint32 {
	value := d.uvarint()
	if d.err != nil {
		return 0
	}
	if value > math.MaxUint32 {
		d.fail(fmt.Errorf("plug: unsigned integer %d overflows u32", value))
		return 0
	}
	return uint32(value)
}

// I64 decodes a signed 64-bit integer.
func (d *Decoder) I64() int64 { return d.varint() }

// U64 decodes an unsigned 64-bit integer.
func (d *Decoder) U64() uint64 { return d.uvarint() }

// F64 decodes a 64-bit floating point value.
func (d *Decoder) F64() float64 {
	value := d.take(8)
	if value == nil {
		return 0
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(value))
}

// String decodes and validates a UTF-8 string.
func (d *Decoder) String() string {
	value := d.Bytes()
	if !utf8.Valid(value) {
		d.fail(fmt.Errorf("plug: string is not valid UTF-8"))
		return ""
	}
	if !d.Reserve(uint64(len(value))) {
		return ""
	}
	return string(value)
}

// Bytes decodes a length-prefixed byte sequence. The result aliases the frame
// payload and remains valid after decoding.
func (d *Decoder) Bytes() []byte {
	length := d.U32()
	if d.err != nil {
		return nil
	}
	return d.take(int(length))
}

// BeginRecord bounds the decoder to a length-delimited record body and
// returns a marker for FinishValue.
func (d *Decoder) BeginRecord() int {
	outerLimit := d.limit
	size := d.U32()
	if d.err != nil {
		return outerLimit
	}
	if uint64(size) > uint64(d.limit-d.offset) {
		d.fail(io.ErrUnexpectedEOF)
		return outerLimit
	}
	d.limit = d.offset + int(size)
	return outerLimit
}

// BeginSum reads a constructor ID, bounds the decoder to its body, and returns
// the constructor ID and a marker for FinishValue.
func (d *Decoder) BeginSum() (uint32, int) {
	value := d.take(4)
	if value == nil {
		return 0, d.limit
	}
	return binary.LittleEndian.Uint32(value), d.BeginRecord()
}

// FinishValue restores the outer bound and ignores extension bytes that were
// not understood by the local schema.
func (d *Decoder) FinishValue(outerLimit int) {
	if outerLimit < d.limit || outerLimit > len(d.data) {
		d.fail(fmt.Errorf("plug: invalid value marker %d", outerLimit))
		return
	}
	if d.err == nil {
		d.offset = d.limit
	}
	d.limit = outerLimit
}

// Remaining returns the unread bytes in the current bounded value.
func (d *Decoder) Remaining() int {
	if d.err != nil {
		return 0
	}
	return d.limit - d.offset
}

// RemainingBytes returns and consumes unread bytes. The result aliases the
// frame payload.
func (d *Decoder) RemainingBytes() []byte {
	if d.err != nil {
		return nil
	}
	value := d.data[d.offset:d.limit]
	d.offset = d.limit
	return value
}

// CopyRemaining consumes unread bytes and returns an owned copy.
func (d *Decoder) CopyRemaining() []byte {
	value := d.RemainingBytes()
	if !d.Reserve(uint64(len(value))) {
		return nil
	}
	return bytes.Clone(value)
}

// IgnoreRemaining consumes extension bytes without interpreting them.
func (d *Decoder) IgnoreRemaining() {
	if d.err == nil {
		d.offset = d.limit
	}
}

func (d *Decoder) varint() int64 {
	if d.err != nil {
		return 0
	}
	value, size := binary.Varint(d.data[d.offset:d.limit])
	switch {
	case size == 0:
		d.fail(io.ErrUnexpectedEOF)
		return 0
	case size < 0:
		d.fail(fmt.Errorf("plug: signed varint overflows 64 bits"))
		return 0
	default:
		d.offset += size
		return value
	}
}

func (d *Decoder) uvarint() uint64 {
	if d.err != nil {
		return 0
	}
	value, size := binary.Uvarint(d.data[d.offset:d.limit])
	switch {
	case size == 0:
		d.fail(io.ErrUnexpectedEOF)
		return 0
	case size < 0:
		d.fail(fmt.Errorf("plug: unsigned varint overflows 64 bits"))
		return 0
	default:
		d.offset += size
		return value
	}
}

func (d *Decoder) take(size int) []byte {
	if d.err != nil {
		return nil
	}
	if size < 0 || size > d.limit-d.offset {
		d.fail(io.ErrUnexpectedEOF)
		return nil
	}
	value := d.data[d.offset : d.offset+size]
	d.offset += size
	return value
}

func (d *Decoder) fail(err error) {
	if d.err == nil {
		d.err = err
	}
}

// LastPresent returns the index of the last present optional field, or -1.
func LastPresent(present ...bool) int {
	for i, p := range slices.Backward(present) {
		if p {
			return i
		}
	}
	return -1
}

// OptionalCodec returns the optional form of codec.
func OptionalCodec[T any](codec Codec[T]) Codec[Option[T]] {
	return Codec[Option[T]]{
		Encode: func(e *Encoder, value Option[T]) {
			inner, ok := value.Value()
			e.Bool(ok)
			if ok {
				codec.Encode(e, inner)
			}
		},
		Decode: func(d *Decoder) Option[T] {
			if d.Remaining() == 0 {
				return None[T]()
			}
			if !d.Bool() {
				return None[T]()
			}
			return Some(codec.Decode(d))
		},
	}
}

// ListCodec returns a list codec with elements encoded by codec.
func ListCodec[T any](codec Codec[T]) Codec[[]T] {
	return Codec[[]T]{
		Encode: func(e *Encoder, values []T) {
			if uint64(len(values)) > uint64(e.maxListElements) {
				e.Fail(fmt.Errorf("plug: list has %d elements; maximum is %d", len(values), e.maxListElements))
				return
			}
			e.U32(uint32(len(values)))
			for _, value := range values {
				codec.Encode(e, value)
			}
		},
		Decode: func(d *Decoder) []T {
			length := d.U32()
			if d.err != nil {
				return nil
			}
			if length > d.maxListElements {
				d.fail(fmt.Errorf("plug: list has %d elements; maximum is %d", length, d.maxListElements))
				return nil
			}
			if length > d.elementsLeft {
				d.fail(fmt.Errorf("plug: total list elements exceed %d", d.maxListElements))
				return nil
			}
			d.elementsLeft -= length

			values := make([]T, 0)
			for range length {
				value := codec.Decode(d)
				if d.err != nil {
					return nil
				}
				if len(values) == cap(values) {
					n := min(uint64(length), max(1, uint64(cap(values))*2))
					if !Reserve[T](d, int(n)) {
						return nil
					}

					grown := make([]T, len(values), int(n))
					copy(grown, values)
					values = grown
				}
				values = append(values, value)
			}
			return values
		},
	}
}

var (
	// BoolCodec encodes schema bool values.
	BoolCodec = Codec[bool]{
		Encode: (*Encoder).Bool,
		Decode: (*Decoder).Bool,
	}

	// BytesCodec encodes schema bytes values.
	BytesCodec = Codec[[]byte]{
		Encode: (*Encoder).Bytes,
		Decode: (*Decoder).Bytes,
	}

	// F64Codec encodes schema f64 values.
	F64Codec = Codec[float64]{
		Encode: (*Encoder).F64,
		Decode: (*Decoder).F64,
	}

	// I32Codec encodes schema i32 values.
	I32Codec = Codec[int32]{
		Encode: (*Encoder).I32,
		Decode: (*Decoder).I32,
	}

	// I64Codec encodes schema i64 values.
	I64Codec = Codec[int64]{
		Encode: (*Encoder).I64,
		Decode: (*Decoder).I64,
	}

	// StringCodec encodes schema string values.
	StringCodec = Codec[string]{
		Encode: (*Encoder).String,
		Decode: (*Decoder).String,
	}

	// U32Codec encodes schema u32 values.
	U32Codec = Codec[uint32]{
		Encode: (*Encoder).U32,
		Decode: (*Decoder).U32,
	}

	// U64Codec encodes schema u64 values.
	U64Codec = Codec[uint64]{
		Encode: (*Encoder).U64,
		Decode: (*Decoder).U64,
	}

	// VoidCodec encodes the zero-byte schema void value.
	VoidCodec = Codec[Void]{
		Encode: func(*Encoder, Void) {},
		Decode: func(*Decoder) Void { return Void{} },
	}
)

var (
	encoderPool = sync.Pool{
		New: func() any {
			return &Encoder{
				data: make([]byte, 0, 256),
			}
		},
	}
	decoderPool = sync.Pool{
		New: func() any { return &Decoder{} },
	}
)

// PooledEncoder returns a reset encoder from the shared pool.
func PooledEncoder(maxListElements uint32) *Encoder {
	e := encoderPool.Get().(*Encoder)
	e.data = e.data[:0]
	e.err = nil
	e.maxListElements = maxListElements
	return e
}

// ReleaseEncoder returns e to the shared pool when its buffer is small enough.
func ReleaseEncoder(e *Encoder) {
	if cap(e.data) <= maxRetainedBufferSize {
		encoderPool.Put(e)
	}
}

// Encode encodes value and returns an owned payload.
func Encode[T any](codec Codec[T], value T, maxListElements uint32) ([]byte, error) {
	e, err := EncodePooled(codec, value, maxListElements)
	if err != nil {
		return nil, err
	}
	data := bytes.Clone(e.data)
	ReleaseEncoder(e)
	return data, nil
}

// EncodePooled encodes value into a pooled encoder.
func EncodePooled[T any](codec Codec[T], value T, maxListElements uint32) (*Encoder, error) {
	if codec.Encode == nil {
		return nil, errors.New("plug: codec has no encoder")
	}
	e := PooledEncoder(maxListElements)
	codec.Encode(e, value)
	if err := e.Err(); err != nil {
		ReleaseEncoder(e)
		return nil, err
	}
	return e, nil
}

// EncodeError encodes value when it is declared by codec.
func EncodeError(codec ErrorCodec, value error, maxListElements uint32) (uint32, *Encoder, bool) {
	e := PooledEncoder(maxListElements)
	id, ok := codec.Encode(e, value)
	if !ok {
		ReleaseEncoder(e)
		return 0, nil, false
	}
	return id, e, true
}

func pooledDecoder(payload []byte, maxListElements uint32, maxBytes uint64) *Decoder {
	if maxBytes == 0 {
		maxBytes = DefaultMaxDecodeBytes
	}
	d := decoderPool.Get().(*Decoder)
	d.data = payload
	d.offset = 0
	d.limit = len(payload)
	d.err = nil
	d.maxListElements = maxListElements
	d.elementsLeft = maxListElements
	d.bytesLeft = maxBytes
	return d
}

func releaseDecoder(d *Decoder) {
	d.data = nil
	decoderPool.Put(d)
}

// Decode decodes a complete payload. A zero maxBytes uses DefaultMaxDecodeBytes.
func Decode[T any](codec Codec[T], payload []byte, maxListElements uint32, maxBytes uint64) (T, error) {
	var zero T
	if codec.Decode == nil {
		return zero, errors.New("plug: codec has no decoder")
	}
	d := pooledDecoder(payload, maxListElements, maxBytes)
	defer releaseDecoder(d)
	value := codec.Decode(d)
	if err := d.Done(); err != nil {
		return zero, err
	}
	return value, nil
}

// DecodeError decodes a complete application-error payload.
func DecodeError(codec ErrorCodec, payload []byte, id, maxListElements uint32, maxBytes uint64) (error, error) {
	d := pooledDecoder(payload, maxListElements, maxBytes)
	defer releaseDecoder(d)

	value := codec.Decode(d, id)
	return value, d.Done()
}
