// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/plug"
	"go.astrophena.name/plug/plugimpl"
)

var update = flag.Bool("update", false, "update generator golden files")

func TestGenerateGolden(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("testdata", "generate.plug"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := parse("generate.plug", source)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		path     string
		generate func(plug.Schema) ([]byte, error)
	}{
		"native Go": {
			path:     "generate.go.golden",
			generate: Go,
		},
		"Go HTTP": {
			path:     "generate_http.go.golden",
			generate: GoHTTPClient,
		},
		"TypeScript": {
			path:     "generate.ts.golden",
			generate: TypeScript,
		},
		"JSON": {
			path:     "generate.json.golden",
			generate: JSON,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			before := plugimpl.CopySchema(&s)
			got, err := tc.generate(s)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(s, *before) {
				t.Fatal("generator changed its input schema")
			}

			if *update {
				if err := os.WriteFile(filepath.Join("testdata", tc.path), got, 0644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(filepath.Join("testdata", tc.path))
			if err != nil {
				t.Fatal(err)
			}
			testutil.AssertEqual(t, string(got), string(want))
		})
	}
}
