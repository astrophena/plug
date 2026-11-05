// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"bytes"
	"encoding/json/v2"
	"slices"
	"testing"
)

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
	users := document.Categories[0]
	if users.Documentation != "This category groups user operations." || !slices.Equal(users.Types, []string{"Choice", "User"}) || !slices.Equal(users.Methods, []string{"test.get"}) {
		t.Fatalf("category metadata = %#v", users)
	}
}

func TestGenerateJSONEmptyCollections(t *testing.T) {
	cases := map[string]string{
		"empty schema": "package empty",
		"declarations": `package empty

type ID = i32
category "Types"
item = Item
category "Methods"
empty.scalars b: bool f: f64 i: i32 u: u64 -> void`,
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := JSON(mustParse(t, source))
			if err != nil {
				t.Fatal(err)
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
