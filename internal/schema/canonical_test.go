// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"errors"
	"testing"

	"go.astrophena.name/base/testutil"
)

func TestIdentity(t *testing.T) {
	base := mustParse(t, `package x
type ID = i64
item id: ID = Item
x.get id: ID -> Item
`)
	cases := map[string]struct {
		src  string
		same bool
	}{
		"presentation": {
			`// Package docs.
package x version 1
category "Calls"
// Method docs.
x.get id: ID -> Item public
category "Data"
// Constructor docs.
item id: ID = Item
type ID = i64
`,
			true,
		},
		"optional tails": {
			`package x
type ID = i64
item id: ID note: string? = Item
x.get id: ID locale: string? -> Item
`,
			false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := mustParse(t, tc.src)

			testutil.AssertEqual(t, s.constructors[0].id, base.constructors[0].id)
			testutil.AssertEqual(t, s.methods[0].id, base.methods[0].id)
			testutil.AssertEqual(t, fingerprint(s) == fingerprint(base), tc.same)
		})
	}
}

func TestIDCollision(t *testing.T) {
	// These declarations share CRC32 0x07b59035.
	_, err := Parse("collision.plug", []byte(`package x
cc5dc12d65ddd1f48 = Value
x.m65cb0099480b797a -> void public
`))
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("Parse() = %v; want a schema diagnostic", err)
	}

	testutil.AssertEqual(t, e.Message, `CRC32 collision between "x.cc5dc12d65ddd1f48 = Value" and "x.m65cb0099480b797a -> void" (0x07b59035)`)
	testutil.AssertEqual(t, e.Position, Position{
		Filename: "collision.plug",
		Line:     3,
		Column:   1,
	})
}
