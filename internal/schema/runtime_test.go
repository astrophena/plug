// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"os"
	"reflect"
	"testing"

	"go.astrophena.name/plug/example/greeter"
)

func TestRuntimeSchemaMatchesGeneratedSchema(t *testing.T) {
	source, err := os.ReadFile("../../example/greeter/schema.plug")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse("schema.plug", source)
	if err != nil {
		t.Fatal(err)
	}
	got := RuntimeSchema(parsed)
	if !reflect.DeepEqual(got, greeter.Schema) {
		t.Fatalf("runtime schema differs from generated schema:\ngot:  %#v\nwant: %#v", got, greeter.Schema)
	}
}

func TestRuntimeSchemaOwnership(t *testing.T) {
	const source = `
package test
category "Users"
type ZID = i64
item id: ZID labels: list<string> = Item
test.z labels: list<string> -> Item public
  http POST "/z"
  http GET "/z"
test.a -> Item public
`
	parsed := mustParse(t, source)
	want := mustParse(t, source)
	metadata := RuntimeSchema(parsed)
	if !reflect.DeepEqual(parsed, want) {
		t.Fatal("RuntimeSchema changed parsed declarations")
	}

	metadata.Categories[0].Types[0] = "Changed"
	metadata.Methods[1].HTTP[0].Path = "/changed"
	metadata.Types[1].Constructors[0].Fields[1].Type.Element.Name = "bytes"
	if !reflect.DeepEqual(RuntimeSchema(parsed), RuntimeSchema(want)) {
		t.Fatal("changing metadata changed the parsed schema")
	}
}
