// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"slices"
	"strings"
	"testing"

	"go.astrophena.name/plug/plugimpl"
)

func TestParse(t *testing.T) {
	source := `
// Service docs.
package example;

// Identifier docs.
type UserID = i64;

// User docs.
user
  // ID docs.
  id:UserID
  name:string
= User

missing id:UserID = LookupError

// Method docs.
example.lookup id:UserID -> User throws LookupError
`
	parsed, err := Parse("example.plug", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.packageName != "example" || parsed.documentation != "Service docs." {
		t.Fatalf("package = %q, docs = %q", parsed.packageName, parsed.documentation)
	}
	if got := parsed.constructors[0].fields[0].documentation; got != "ID docs." {
		t.Fatalf("field docs = %q", got)
	}
	if parsed.methods[0].errorType == nil || parsed.methods[0].errorType.name != "LookupError" {
		t.Fatalf("method error = %#v", parsed.methods[0].errorType)
	}
	if parsed.methods[0].name != "example.lookup" {
		t.Fatalf("method name = %q", parsed.methods[0].name)
	}
	formatted := formatSchema(parsed)
	if strings.Contains(formatted, ";") {
		t.Fatalf("formatted schema contains a semicolon:\n%s", formatted)
	}
	if !strings.Contains(formatted, "id: UserID") {
		t.Fatalf("formatted schema does not space field types:\n%s", formatted)
	}
	reparsed, err := Parse("formatted.plug", []byte(formatted))
	if err != nil {
		t.Fatalf("parse formatted schema: %v\n%s", err, formatted)
	}
	if fingerprint(parsed) != fingerprint(reparsed) {
		t.Fatalf("formatted fingerprint changed")
	}
}

func TestParseServiceAnnotations(t *testing.T) {
	parsed, err := Parse("service.plug", []byte(`
package example version 3
// Read access.
permission item.read
notFound id: i64 = LookupError httpStatus 404
example.get id: i64 -> string throws LookupError requires item.read http GET "/v3/items/{id}"
example.health -> void public http GET "/healthz"
`))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.major != 3 || parsed.permissions[0].name != "item.read" {
		t.Fatalf("schema = %#v", parsed)
	}
	method := parsed.methods[0]
	if method.permission != "item.read" || method.public || len(method.http) != 1 {
		t.Fatalf("method = %#v", method)
	}
	if parsed.constructors[0].httpStatus != 404 {
		t.Fatalf("error constructor = %#v", parsed.constructors[0])
	}
	formatted := formatSchema(parsed)
	want := "example.get id: i64 -> string\n  throws LookupError\n  requires item.read\n  http GET \"/v3/items/{id}\""
	if !strings.Contains(formatted, want) {
		t.Fatalf("formatted method:\n%s\nwant containing:\n%s", formatted, want)
	}
}

func TestParseRawMethod(t *testing.T) {
	parsed, err := Parse("raw.plug", []byte(`
package example version 1
failed reason: string = OpenError
// Opens a byte stream.
example.open port: u32 -> raw throws OpenError public
`))
	if err != nil {
		t.Fatal(err)
	}
	method := parsed.methods[0]
	if !isRaw(method.response) {
		t.Fatalf("response = %#v", method.response)
	}
	if got := methodText(method, false); got != "example.open port:u32 -> raw throws OpenError" {
		t.Fatalf("canonical method = %q", got)
	}
	formatted := formatSchema(parsed)
	if !strings.Contains(formatted, "example.open port: u32 -> raw\n  throws OpenError") {
		t.Fatalf("formatted schema:\n%s", formatted)
	}
	runtime := RuntimeSchema(parsed)
	if !plugimpl.MethodIsRaw(runtime.Methods[0]) {
		t.Fatalf("runtime method = %#v", runtime.Methods[0])
	}
}

func TestTypeAnnotations(t *testing.T) {
	parsed, err := Parse("types.plug", []byte(`
package example

// A way to authenticate a user.
type AuthMethod

// WebAuthn passkey.
passkey id: i64 publicKey: bytes = AuthMethod

// An ordinary password.
password hash: bytes = AuthMethod

example.authenticate method: AuthMethod -> void public
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.types) != 1 || parsed.types[0].name != "AuthMethod" {
		t.Fatalf("parsed types = %#v", parsed.types)
	}
	if parsed.types[0].documentation != "A way to authenticate a user." {
		t.Fatalf("type doc = %q", parsed.types[0].documentation)
	}
	formatted := formatSchema(parsed)
	if !strings.Contains(formatted, "// A way to authenticate a user.\ntype AuthMethod") {
		t.Fatalf("formatted output missing type annotation:\n%s", formatted)
	}
}

func TestCategories(t *testing.T) {
	parsed, err := Parse("categories.plug", []byte(`
package example

// Shared values.
category "Data model"

type UserID = i64

user id: UserID = User

// User operations.
category "Users"

// Gets the requested user.
example.getUser id: UserID -> string

example.listUsers -> list<string>

// Service diagnostics.
category "Service"

example.ping -> void
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.categories) != 3 {
		t.Fatalf("categories = %#v", parsed.categories)
	}
	dataModel := parsed.categories[0]
	if dataModel.name != "Data model" || !slices.Equal(dataModel.types, []string{"UserID", "User"}) || len(dataModel.methods) != 0 {
		t.Fatalf("data model category = %#v", dataModel)
	}
	users := parsed.categories[1]
	if users.name != "Users" || users.documentation != "User operations." || !slices.Equal(users.methods, []string{"example.getUser", "example.listUsers"}) {
		t.Fatalf("users category = %#v", users)
	}
	formatted := formatSchema(parsed)
	for _, want := range []string{
		"// Shared values.\ncategory \"Data model\"",
		"// User operations.\ncategory \"Users\"",
		"// Service diagnostics.\ncategory \"Service\"",
	} {
		if !strings.Contains(formatted, want) {
			t.Errorf("formatted schema does not contain %q:\n%s", want, formatted)
		}
	}
	reparsed, err := Parse("formatted.plug", []byte(formatted))
	if err != nil {
		t.Fatalf("parse formatted schema: %v\n%s", err, formatted)
	}
	equalNames := func(a, b []string) bool {
		a = slices.Clone(a)
		b = slices.Clone(b)
		slices.Sort(a)
		slices.Sort(b)
		return slices.Equal(a, b)
	}
	if !slices.EqualFunc(parsed.categories, reparsed.categories, func(a, b category) bool {
		return a.name == b.name && a.documentation == b.documentation && equalNames(a.types, b.types) && equalNames(a.methods, b.methods)
	}) {
		t.Fatalf("reparsed categories = %#v, want %#v", reparsed.categories, parsed.categories)
	}

	withoutCategories, err := Parse("uncategorized.plug", []byte(`
package example
type UserID = i64
user id: UserID = User
example.getUser id: UserID -> string
example.listUsers -> list<string>
example.ping -> void
`))
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint(parsed) != fingerprint(withoutCategories) {
		t.Fatal("categories changed schema fingerprint")
	}
	if parsed.constructors[0].id != withoutCategories.constructors[0].id {
		t.Fatal("category changed constructor ID")
	}
	for i := range parsed.methods {
		if parsed.methods[i].id != withoutCategories.methods[i].id {
			t.Fatalf("category changed method ID for %s", parsed.methods[i].name)
		}
	}

}

func TestCategoryKeywordIsContextual(t *testing.T) {
	for name, source := range map[string]string{
		"field and constructor": `package x
type CategoryValue = string
category category: CategoryValue = Record
x.run category: CategoryValue -> Record`,
		"type": `package x
type category = string
x.run value: category -> category`,
		"method": `package x
x.category -> void`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(name+".plug", []byte(source)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFingerprintIgnoresDocsAndDeclarationOrder(t *testing.T) {
	a, err := Parse("a.plug", []byte(`package x
// A.
foo value:string = Foo
x.bar value:i64 -> Foo
`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Parse("b.plug", []byte(`// Package docs.
package x
// Method docs.
x.bar value: i64 -> Foo
// Different constructor docs.
foo value: string = Foo
`))
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint(a) != fingerprint(b) {
		t.Fatalf("semantically equal schemas have different fingerprints")
	}
	if a.constructors[0].id != b.constructors[0].id || a.methods[0].id != b.methods[0].id {
		t.Fatalf("semantically equal declarations have different IDs")
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]struct {
		src  string
		want string
	}{
		"duplicate field": {
			src:  `package x; foo value:string value:i64 = Foo;`,
			want: "duplicate field",
		},
		"error reused as response": {
			src:  `package x; bad = Failure; x.run -> Failure throws Failure;`,
			want: "cannot be used as a response",
		},
		"invalid newtype": {
			src:  `package x; type Value = list;`,
			want: "must use a scalar primitive",
		},
		"void newtype": {
			src:  `package x; type Value = void;`,
			want: "must use a scalar primitive",
		},
		"void field": {
			src:  `package x; value empty:void = Value;`,
			want: "void is only valid",
		},
		"raw newtype": {
			src:  `package x; type Value = raw;`,
			want: "must use a scalar primitive",
		},
		"raw field": {
			src:  `package x; value stream:raw = Value; x.run -> Value;`,
			want: "raw is only valid",
		},
		"optional raw response": {
			src:  `package x; x.run -> raw?;`,
			want: "raw is only valid",
		},
		"raw list element": {
			src:  `package x; x.run -> list<raw>;`,
			want: "raw is only valid",
		},
		"raw HTTP binding": {
			src:  `package x; x.run -> raw http GET "/run";`,
			want: "cannot have an HTTP binding",
		},
		"removed unit primitive": {
			src:  `package x; x.ping -> unit;`,
			want: `unknown type "unit"`,
		},
		"primitive newtype name": {
			src:  `package x; type string = i64;`,
			want: "reserved for a primitive",
		},
		"error method field collision": {
			src:  `package x; failed error:string = Failure; x.run -> void throws Failure;`,
			want: "generated Error method",
		},
		"list optional element": {
			src:  `package x; foo values:list<string?> = Foo; x.run -> Foo;`,
			want: "list elements cannot be optional",
		},
		"recursive": {
			src:  `package x; node children:list<Node> = Node; x.run -> Node;`,
			want: "recursive type cycle",
		},
		"unknown type": {
			src:  `package x; foo value:Missing = Foo;`,
			want: "unknown type",
		},
		"generated declaration collision": {
			src:  `package x; type Schema = string;`,
			want: "generated Go declaration",
		},
		"client Close collision": {
			src:  `package x; x.close -> void public`,
			want: "generated Client.Close",
		},
		"missing constructor result": {
			src:  `package x; foo value:string =`,
			want: "constructor result type",
		},
		"undotted method": {
			src:  `package x; ping -> void`,
			want: "form group.name",
		},
		"multiply dotted method": {
			src:  `package x; x.admin.ping -> void`,
			want: "form group.name",
		},
		"dotted Go name collision": {
			src:  `package x; x.foo -> void; y.foo -> void`,
			want: "generated Go declaration",
		},
		"required after optional": {
			src:  `package x; value old:string? new:string = Value`,
			want: "follows an optional extension field",
		},
		"undeclared permission": {
			src:  `package x; x.run -> void requires item.read`,
			want: "undeclared permission",
		},
		"duplicate access": {
			src:  `package x; permission item.read; x.run -> void public requires item.read`,
			want: "access is declared more than once",
		},
		"unknown HTTP verb": {
			src:  `package x; x.run -> void http CONNECT "/run"`,
			want: "unsupported HTTP verb",
		},
		"non-scalar HTTP path": {
			src:  `package x; value text:string = Value; x.run value:Value -> void http GET "/run/{value}"`,
			want: "must be a scalar field",
		},
		"status on response": {
			src:  `package x; value text:string = Value httpStatus 400; x.run -> Value`,
			want: "only valid on declared error constructors",
		},
		"empty category": {
			src:  `package x; category ""; x.run -> void`,
			want: "category name is empty",
		},
		"padded category": {
			src:  `package x; category " Users "; x.run -> void`,
			want: "leading or trailing whitespace",
		},
		"category without declarations": {
			src:  `package x; category "Users"`,
			want: "contains no types or methods",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(name+".plug", []byte(tc.src))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(`package x
x.ping -> void
`))
	f.Add([]byte(`package x; value text:string? = Value; x.get -> Value;`))
	f.Fuzz(func(t *testing.T, source []byte) {
		if _, err := Parse("fuzz.plug", source); err != nil {
			return
		}
	})
}
