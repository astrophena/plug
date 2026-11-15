// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"strings"
	"testing"
)

func TestCompatibility(t *testing.T) {
	cases := map[string]struct {
		old  string
		next string
		want string
	}{
		"additions": {
			`package x
permission item.read
item id: i64 = Item
missing = Failure httpStatus 404
x.get id: i64 -> Item throws Failure requires item.read http GET "/items/{id}"
`,
			`package x
permission item.read
permission item.write
item id: i64 note: string? = Item
missing reason: string? = Failure httpStatus 404
x.get id: i64 locale: string? -> Item throws Failure requires item.read
  http GET "/items/{id}"
  http POST "/get"
x.put item: Item -> Item requires item.write
`,
			"",
		},
		"required field": {
			`package x; item id: i64 = Item`,
			`package x; item id: i64 revision: u64 = Item`,
			"added required field revision",
		},
		"access": {
			`package x; permission item.read; x.get -> void requires item.read`,
			`package x; permission item.read; x.get -> void public`,
			"changed access requirement",
		},
		"raw response": {
			`package x; x.open -> string`,
			`package x; x.open -> raw`,
			"changed required signature",
		},
		"major version": {
			`package x version 1`,
			`package x version 2`,
			"package generation changed",
		},
		"record to sum": {
			`package x; present id: i64 = Item`,
			`package x; present id: i64 = Item; deleted id: i64 = Item`,
			"changed record Item into a sum",
		},
		"removed route": {
			`package x; x.ping -> void http GET "/ping"`,
			`package x; x.ping -> void`,
			"removed HTTP binding",
		},
		"removed status": {
			`package x; missing = Failure httpStatus 404; x.get -> void throws Failure`,
			`package x; missing = Failure; x.get -> void throws Failure`,
			"changed constructor missing HTTP status from 404 to 0",
		},
		"added status": {
			`package x; missing = Failure; x.get -> void throws Failure`,
			`package x; missing = Failure httpStatus 404; x.get -> void throws Failure`,
			"changed constructor missing HTTP status from 0 to 404",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var (
				old  = mustParse(t, tc.old)
				next = mustParse(t, tc.next)
			)
			err := CheckCompatibility(old, next)
			if tc.want == "" && err != nil {
				t.Fatal(err)
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("CheckCompatibility() = %v; want %q", err, tc.want)
			}
		})
	}
}
