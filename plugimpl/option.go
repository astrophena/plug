// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plugimpl

import (
	"bytes"
	"encoding/json/v2"
)

// Option holds a value that may be absent. Its zero value is None.
type Option[T any] struct {
	value T
	set   bool
}

// Some returns an Option containing value.
func Some[T any](value T) Option[T] {
	return Option[T]{
		value: value,
		set:   true,
	}
}

// None returns an absent Option.
func None[T any]() Option[T] { return Option[T]{} }

// Value returns the contained value and whether it is present.
func (o Option[T]) Value() (T, bool) { return o.value, o.set }

// Present reports whether the option contains a value.
func (o Option[T]) Present() bool { return o.set }

// IsZero reports whether the option is absent for encoders that honor Go's
// zero-value convention.
func (o Option[T]) IsZero() bool { return !o.set }

// Or returns the contained value when present or fallback otherwise.
func (o Option[T]) Or(fallback T) T {
	if o.set {
		return o.value
	}
	return fallback
}

// OrZero returns the contained value when present or the zero value of T.
func (o Option[T]) OrZero() T { return o.value }

// MarshalJSON encodes None as null and Some as its contained value.
func (o Option[T]) MarshalJSON() ([]byte, error) {
	if !o.set {
		return []byte("null"), nil
	}
	return json.Marshal(o.value)
}

// UnmarshalJSON decodes null as None and any other value as Some.
func (o *Option[T]) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*o = None[T]()
		return nil
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*o = Some(value)
	return nil
}

// Void is the zero-byte value used by methods without a response body.
type Void struct{}
