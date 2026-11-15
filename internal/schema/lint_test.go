// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"slices"
	"testing"

	"go.astrophena.name/base/testutil"
)

func TestLint(t *testing.T) {
	cases := map[string]struct {
		src  string
		want []string
	}{
		"documented": {
			src: `
// A test service
package x
// Read permission
permission item.read
// Item operations
category "Items"
// An identifier
type ID = i64
// A response
item
  // The identifier
  id: ID
= Item
// Lookup failures
type Failure
// The item does not exist
missing = Failure
// Returns an item
x.get
  // The requested identifier
  id: ID
-> Item throws Failure requires item.read
`,
		},
		"unused": {
			src: `
// A test service.
package x
// An unused permission.
permission item.read
// An unused type.
type ID = i64
// Checks the service.
x.ping -> void
`,
			want: []string{
				`newtype "ID" is declared but never referenced`,
				`permission "item.read" is declared but never required by any method`,
			},
		},
		"undocumented": {
			src: `package x; permission item.read; x.get -> void requires item.read`,
			want: []string{
				`method "x.get" is missing a documentation comment`,
				`package "x" is missing a documentation comment`,
				`permission "item.read" is missing a documentation comment`,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var got []string
			for _, err := range Lint(mustParse(t, tc.src)) {
				got = append(got, err.(*Error).Message)
			}
			slices.Sort(got)

			testutil.AssertEqual(t, got, tc.want)
		})
	}
}
