// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"
	"reflect"
	"testing"

	"go.astrophena.name/plug/example/greeter"
)

func TestGenerateJSONIdentity(t *testing.T) {
	source, err := os.ReadFile("../../example/greeter/schema.plug")
	if err != nil {
		t.Fatal(err)
	}
	s, err := parse("greeter.plug", source)
	if err != nil {
		t.Fatal(err)
	}
	data, err := JSON(s)
	if err != nil {
		t.Fatal(err)
	}
	var document jsonSchema
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.FormatVersion != 1 || document.ProtocolVersion != 1 {
		t.Fatalf("unexpected export versions: %d, %d", document.FormatVersion, document.ProtocolVersion)
	}
	if document.Package != greeter.Schema.Package || document.Major != greeter.Schema.Major {
		t.Fatal("exported schema namespace differs from generated schema")
	}
	if document.Family != fmt.Sprintf("%x", greeter.Schema.Family) || document.Fingerprint != fmt.Sprintf("%x", greeter.Schema.Fingerprint) {
		t.Fatal("exported hashes differ from generated schema")
	}

	var (
		methods      = make(map[string]uint32)
		constructors = make(map[string]uint32)
	)
	for _, method := range document.Methods {
		methods[method.Name] = method.ID
	}
	for _, typ := range document.Types {
		for _, constructor := range typ.Constructors {
			constructors[constructor.Name] = constructor.ID
		}
	}
	if len(methods) != len(greeter.Schema.Methods) {
		t.Fatal("exported method count differs from generated schema")
	}
	for _, method := range greeter.Schema.Methods {
		if id, ok := methods[method.Name]; !ok || id != method.ID {
			t.Errorf("method %s ID = %d; want %d", method.Name, id, method.ID)
		}
	}
	for _, typ := range greeter.Schema.Types {
		for _, constructor := range typ.Constructors {
			if id, ok := constructors[constructor.Name]; !ok || id != constructor.ID {
				t.Errorf("constructor %s ID = %d; want %d", constructor.Name, id, constructor.ID)
			}
			delete(constructors, constructor.Name)
		}
	}
	if len(constructors) != 0 {
		t.Errorf("unexpected exported constructors: %v", constructors)
	}
}

func TestGenerateJSONDeterministic(t *testing.T) {
	first := mustParse(t, `
package test version 2
permission test.z
permission test.a
type Token = bytes
type UserID = i64
// This category groups user operations.
category "Users"
type Choice
right token: Token = Choice
left = Choice
user z: UserID a: string note: string? = User
test.get z: UserID a: string? -> User? requires test.a
  http POST "/lookup"
  http GET "/users/{z}"
category "Control"
test.ping -> void public
`)
	second, err := parse("/another/path/schema.plug", []byte(`
package test version 2;
type UserID = i64
type Token = bytes
permission test.a
permission test.z
// This category groups user operations.
category "Users"
test.get z: UserID a: string? -> User? requires test.a
  http GET "/users/{z}"
  http POST "/lookup"
user z: UserID a: string note: string? = User
left = Choice
right token: Token = Choice
type Choice
category "Control"
test.ping -> void public
`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := JSON(first)
	if err != nil {
		t.Fatal(err)
	}
	want, err := JSON(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("source path or declaration order changed JSON output")
	}

	var document jsonSchema
	if err := json.Unmarshal(got, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Categories) != 2 || document.Categories[0].Name != "Users" || document.Categories[1].Name != "Control" {
		t.Fatalf("category order changed: %v", document.Categories)
	}
	if document.Categories[0].Documentation != "This category groups user operations." || !reflect.DeepEqual(document.Categories[0].Types, []string{"Choice", "User"}) || !reflect.DeepEqual(document.Categories[0].Methods, []string{"test.get"}) {
		t.Fatalf("unexpected category metadata: %#v", document.Categories[0])
	}
	for _, typ := range document.Types {
		if typ.Name != "User" {
			continue
		}
		var names []string
		for _, field := range typ.Constructors[0].Fields {
			names = append(names, field.Name)
		}
		if !reflect.DeepEqual(names, []string{"z", "a", "note"}) {
			t.Fatalf("field wire order changed: %v", names)
		}
	}
}

func TestGenerateJSONEmptyCollections(t *testing.T) {
	cases := map[string]string{
		"empty schema":     "package empty",
		"newtype":          "package empty\ntype ID = i32",
		"constructor":      "package empty\nitem = Item\nempty.ping -> void",
		"empty members":    "package empty\ncategory \"Types\"\nitem = Item\ncategory \"Methods\"\nempty.ping -> void",
		"primitive fields": "package empty\nempty.scalars b: bool f: f64 i: i32 u: u64 -> void",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := JSON(mustParse(t, source))
			if err != nil {
				t.Fatal(err)
			}
			if !jsontext.Value(data).IsValid() || !bytes.HasSuffix(data, []byte("\n")) {
				t.Fatal("export is not valid newline-terminated JSON")
			}
			if bytes.Contains(data, []byte("null")) {
				t.Fatalf("empty collection encoded as null:\n%s", data)
			}
			if !bytes.Contains(data, []byte(`"documentation": ""`)) || !bytes.Contains(data, []byte(`"permissions": []`)) {
				t.Fatal("missing documentation or empty permissions were omitted")
			}
		})
	}
}
