// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"strings"
	"testing"
)

func TestCompatibility(t *testing.T) {
	older := mustParse(t, `
package example version 1
permission item.read
type ID = i64
present id: ID name: string = Item
notFound id: ID = LookupError httpStatus 404
example.get id: ID -> Item throws LookupError requires item.read http GET "/v1/items/{id}"
`)
	cases := map[string]struct {
		source string
		want   string
	}{
		"add optional tails and declarations": {
			source: `
package example version 1
permission item.read
permission item.write
type ID = i64
present id: ID name: string note: string? = Item
notFound id: ID reason: string? = LookupError httpStatus 404
example.get id: ID locale: string? -> Item throws LookupError requires item.read http GET "/v1/items/{id}" http POST "/v1/get-item"
example.put item: Item -> Item requires item.write
`,
		},
		"add required field": {
			source: `
package example version 1
permission item.read
type ID = i64
present id: ID name: string revision: u64 = Item
notFound id: ID = LookupError httpStatus 404
example.get id: ID -> Item throws LookupError requires item.read http GET "/v1/items/{id}"
`,
			want: "added required field revision",
		},
		"change access": {
			source: `
package example version 1
permission item.read
type ID = i64
present id: ID name: string = Item
notFound id: ID = LookupError httpStatus 404
example.get id: ID -> Item throws LookupError public http GET "/v1/items/{id}"
`,
			want: "changed access requirement",
		},
		"change regular method to raw": {
			source: `
package example version 1
permission item.read
type ID = i64
present id: ID name: string = Item
notFound id: ID = LookupError httpStatus 404
example.get id: ID -> raw throws LookupError requires item.read
`,
			want: "changed required signature",
		},
		"change major": {
			source: `
package example version 2
permission item.read
type ID = i64
present id: ID name: string = Item
notFound id: ID = LookupError httpStatus 404
example.get id: ID -> Item throws LookupError requires item.read http GET "/v1/items/{id}"
`,
			want: "package generation changed",
		},
		"record becomes sum": {
			source: `
package example version 1
permission item.read
type ID = i64
present id: ID name: string = Item
deleted id: ID = Item
notFound id: ID = LookupError httpStatus 404
example.get id: ID -> Item throws LookupError requires item.read http GET "/v1/items/{id}"
`,
			want: "changed record Item into a sum",
		},
		"remove route": {
			source: `
package example version 1
permission item.read
type ID = i64
present id: ID name: string = Item
notFound id: ID = LookupError httpStatus 404
example.get id: ID -> Item throws LookupError requires item.read
`,
			want: "removed HTTP binding",
		},
		"change HTTP status (404 -> 0)": {
			source: `
package example version 1
permission item.read
type ID = i64
present id: ID name: string = Item
notFound id: ID = LookupError
example.get id: ID -> Item throws LookupError requires item.read http GET "/v1/items/{id}"
`,
			want: "changed constructor notFound HTTP status from 404 to 0",
		},
		"change HTTP status (0 -> 404)": {
			source: `
package example version 1
permission item.read
type ID = i64
present id: ID name: string = Item
notFound id: ID = LookupError httpStatus 404
example.get id: ID -> Item throws LookupError requires item.read http GET "/v1/items/{id}"
`,
			want: "changed constructor notFound HTTP status from 0 to 404",
		},
		"change HTTP status (404 -> 409)": {
			source: `
package example version 1
permission item.read
type ID = i64
present id: ID name: string = Item
notFound id: ID = LookupError httpStatus 409
example.get id: ID -> Item throws LookupError requires item.read http GET "/v1/items/{id}"
`,
			want: "changed constructor notFound HTTP status from 404 to 409",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// To test a transition from 0 to 404, we need to parse an 'older' schema without an HTTP status.
			// The original older has notFound with httpStatus 404.
			olderForTest := older
			if name == "change HTTP status (0 -> 404)" {
				olderForTest = mustParse(t, strings.ReplaceAll(
					`package example version 1
permission item.read
type ID = i64
present id: ID name: string = Item
notFound id: ID = LookupError
example.get id: ID -> Item throws LookupError requires item.read http GET "/v1/items/{id}"`, "\n", "\n"))
			}

			newer := mustParse(t, tc.source)
			err := CheckCompatibility(olderForTest, newer)
			if tc.want == "" && err != nil {
				t.Fatal(err)
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestOptionalTailKeepsWireIDs(t *testing.T) {
	older := mustParse(t, `
package example version 1
present id: i64 = Item
example.get id: i64 -> Item
`)
	newer := mustParse(t, `
package example version 1
present id: i64 note: string? = Item
example.get id: i64 locale: string? -> Item
`)
	if older.constructors[0].id != newer.constructors[0].id {
		t.Fatalf("constructor ID changed from %08x to %08x", older.constructors[0].id, newer.constructors[0].id)
	}
	if older.methods[0].id != newer.methods[0].id {
		t.Fatalf("method ID changed from %08x to %08x", older.methods[0].id, newer.methods[0].id)
	}
	if fingerprint(older) == fingerprint(newer) {
		t.Fatal("exact fingerprint did not change")
	}
}

func mustParse(t *testing.T, source string) *Schema {
	t.Helper()
	parsed, err := Parse("schema.plug", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
