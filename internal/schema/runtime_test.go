// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"os"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/plug/example/greeter"
)

func TestRuntimeSchema(t *testing.T) {
	src, err := os.ReadFile("../../example/greeter/schema.plug")
	if err != nil {
		t.Fatal(err)
	}

	s := mustParse(t, string(src))
	testutil.AssertEqual(t, RuntimeSchema(s), greeter.Schema)
}

func TestRuntimeOwnership(t *testing.T) {
	const src = `
package x
category "Items"
type ID = i64
item id: ID labels: list<string> = Item
x.z labels: list<string> -> Item public http POST "/z" http GET "/z"
x.a -> Item public
`
	var (
		s    = mustParse(t, src)
		want = RuntimeSchema(mustParse(t, src))
		got  = RuntimeSchema(s)
	)
	got.Categories[0].Types[0] = "Changed"
	got.Methods[1].HTTP[0].Path = "/changed"
	got.Types[1].Constructors[0].Fields[1].Type.Element.Name = "bytes"

	testutil.AssertEqual(t, RuntimeSchema(s), want)
}
