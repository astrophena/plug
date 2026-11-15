// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"testing"

	"go.astrophena.name/plug"
)

func TestFixtures(t *testing.T) {
	cases := map[string]struct {
		schema string
		path   string
		gen    func(plug.Schema) ([]byte, error)
	}{
		"HTTP client": {
			"../../example/greeter/schema.plug",
			"internal/greeter/client.go",
			GoHTTPClient,
		},
		"JSON client": {
			"internal/jsoncontract/schema.plug",
			"internal/jsoncontract/client.go",
			GoHTTPClient,
		},
		"HTTP routes": {
			"internal/fields/schema.plug",
			"internal/fields/bindings.go",
			Go,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := tc.gen(readSchema(t, tc.schema))
			if err != nil {
				t.Fatal(err)
			}

			snapshot(t, tc.path, got)
		})
	}
}
