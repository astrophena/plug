// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
)

func TestGenerateGoHTTPClientStdlibOnly(t *testing.T) {
	source, err := os.ReadFile("testdata/generate.plug")
	if err != nil {
		t.Fatal(err)
	}
	s, err := parse("generate.plug", source)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := GoHTTPClient(s)
	if err != nil {
		t.Fatal(err)
	}

	file, err := goparser.ParseFile(token.NewFileSet(), "client.go", generated, goparser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if strings.Contains(path, ".") {
			t.Errorf("unexpected non-standard library import: %s", path)
		}
	}
}

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
