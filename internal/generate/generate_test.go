// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"flag"
	"os"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/plug"
	"go.astrophena.name/plug/internal/schema"
	"go.astrophena.name/plug/plugimpl"
)

var update = flag.Bool("update", false, "update generated fixtures")

func TestGolden(t *testing.T) {
	s := readSchema(t, "testdata/generate.plug")
	for name, gen := range map[string]func(plug.Schema) ([]byte, error){
		"generate.go":      Go,
		"generate_http.go": GoHTTPClient,
		"generate.ts":      TypeScript,
		"generate.json":    JSON,
	} {
		t.Run(name, func(t *testing.T) {
			before := plugimpl.CopySchema(&s)
			got, err := gen(s)
			if err != nil {
				t.Fatal(err)
			}

			testutil.AssertEqual(t, s, *before)
			snapshot(t, "testdata/"+name+".golden", got)
		})
	}
}

func TestComments(t *testing.T) {
	var e emitter
	e.comments("  ", []string{"a\nb\rc\r\nd\u2028e\u2029f"})

	testutil.AssertEqual(t, e.buf.String(), "  // a\n  // b\n  // c\n  // d\n  // e\n  // f\n")
}

func TestDocLines(t *testing.T) {
	cases := map[string]struct {
		doc  string
		want []string
	}{
		"absent": {
			want: []string{"Widget is generated."},
		},
		"source name": {
			doc:  "widget describes a value.\n\nFurther details.",
			want: []string{"Widget describes a value.", "", "Further details."},
		},
		"free prose": {
			doc:  "A value used by callers.",
			want: []string{"Widget is generated.", "", "A value used by callers."},
		},
		"name prefix": {
			doc:  "widgetry is useful.",
			want: []string{"Widget is generated.", "", "widgetry is useful."},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := formatDocLines("Widget", "widget", "is generated.", tc.doc)
			testutil.AssertEqual(t, got, tc.want)
		})
	}
}

func mustParse(t *testing.T, src string) plug.Schema {
	t.Helper()

	s, err := schema.Parse("test.plug", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return schema.RuntimeSchema(s)
}

func readSchema(t *testing.T, path string) plug.Schema {
	t.Helper()

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return mustParse(t, string(src))
}

func snapshot(t *testing.T, path string, got []byte) {
	t.Helper()

	if *update {
		if err := os.WriteFile(path, got, 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	testutil.AssertEqual(t, string(got), string(want))
}
