// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"errors"
	"testing"
)

func TestParseIDCollision(t *testing.T) {
	// These declarations share CRC32 0x07b59035 across constructor and method IDs.
	const source = `package x
cc5dc12d65ddd1f48 = Value
x.m65cb0099480b797a -> void public
`
	s, err := Parse("collision.plug", []byte(source))
	if s != nil {
		t.Fatal("Parse returned a schema with an ID collision")
	}
	var diagnostic *Error
	if !errors.As(err, &diagnostic) {
		t.Fatalf("Parse error = %v; want a schema diagnostic", err)
	}
	const want = `CRC32 collision between "x.cc5dc12d65ddd1f48 = Value" and "x.m65cb0099480b797a -> void" (0x07b59035)`
	if diagnostic.Message != want {
		t.Fatalf("diagnostic = %q; want %q", diagnostic.Message, want)
	}
	if diagnostic.Position != (Position{
		Filename: "collision.plug",
		Line:     3,
		Column:   1,
	}) {
		t.Fatalf("diagnostic position = %v; want collision.plug:3:1", diagnostic.Position)
	}
}
