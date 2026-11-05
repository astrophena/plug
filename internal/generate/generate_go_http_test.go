// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"os"
	"path/filepath"
	"testing"

	"go.astrophena.name/base/testutil"
)

func TestGoHTTPClientFixtures(t *testing.T) {
	cases := map[string]struct {
		schema string
		output string
	}{
		"greeter": {
			schema: filepath.Join("..", "..", "example", "greeter", "schema.plug"),
			output: filepath.Join("internal", "greeter", "client.go"),
		},
		"JSON contract": {
			schema: filepath.Join("internal", "jsoncontract", "schema.plug"),
			output: filepath.Join("internal", "jsoncontract", "client.go"),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			content, err := os.ReadFile(tc.schema)
			if err != nil {
				t.Fatal(err)
			}
			s, err := parse(tc.schema, content)
			if err != nil {
				t.Fatal(err)
			}
			got, err := GoHTTPClient(s)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(tc.output)
			if err != nil {
				t.Fatal(err)
			}
			testutil.AssertEqual(t, string(got), string(want))
		})
	}
}
