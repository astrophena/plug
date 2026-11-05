// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"go.astrophena.name/plug"
)

func TestHTTPClientBindingOrderDoesNotAffectOutput(t *testing.T) {
	const schemaStart = `
package test
item id: i64 = Item
test.get id: i64 -> Item public
`
	first, err := parse("first.plug", []byte(schemaStart+`  http POST "/lookup"
  http GET "/items/{id}"
`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := parse("second.plug", []byte(schemaStart+`  http GET "/items/{id}"
  http POST "/lookup"
`))
	if err != nil {
		t.Fatal(err)
	}

	generators := map[string]func(plug.Schema) ([]byte, error){
		"Go":         GoHTTPClient,
		"TypeScript": TypeScript,
	}
	for name, generate := range generators {
		t.Run(name, func(t *testing.T) {
			got, err := generate(first)
			if err != nil {
				t.Fatal(err)
			}
			want, err := generate(second)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatal("reordering HTTP bindings changed generated client")
			}
			if !bytes.Contains(got, []byte("/items/")) {
				t.Fatal("generated client did not choose canonical GET binding")
			}
		})
	}
}

func TestGeneratePreservesLeadingInitialisms(t *testing.T) {
	schema, err := parse("initialisms.plug", []byte(`
package test version 1

type APIError
sqlError message:string = APIError
otherError = APIError

result httpPath:string = Result
test.run -> Result throws APIError public
`))
	if err != nil {
		t.Fatal(err)
	}

	source, err := Go(schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"type SQLError struct", "HTTPPath string"} {
		if !strings.Contains(string(source), want) {
			t.Errorf("generated source does not contain %q:\n%s", want, source)
		}
	}
}

func TestFormatDocLines(t *testing.T) {
	cases := map[string]struct {
		doc  string
		want []string
	}{
		"absent": {
			want: []string{"Widget is generated."},
		},
		"source name": {
			doc:  "widget describes a value",
			want: []string{"Widget describes a value"},
		},
		"Go name": {
			doc:  "Widget describes a value.",
			want: []string{"Widget describes a value."},
		},
		"free prose": {
			doc:  "A value used by callers.",
			want: []string{"Widget is generated.", "", "A value used by callers."},
		},
		"name prefix": {
			doc:  "widgetry is useful.",
			want: []string{"Widget is generated.", "", "widgetry is useful."},
		},
		"paragraphs": {
			doc:  "widget describes a value.\n\nFurther details.\n\n    use(value)",
			want: []string{"Widget describes a value.", "", "Further details.", "", "    use(value)"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := formatDocLines("Widget", "widget", "is generated.", tc.doc)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got %#v; want %#v", got, tc.want)
			}
		})
	}
}

func TestGenerateSingleConstructorError(t *testing.T) {
	schema, err := parse("single.plug", []byte(`
package single version 1

failed reason: string = PingError
single.ping -> void throws PingError public
`))
	if err != nil {
		t.Fatal(err)
	}
	source, err := Go(schema)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(source)
	if strings.Contains(generated, "UnknownPingError") {
		t.Fatalf("single-constructor error generated an unknown sum variant:\n%s", generated)
	}
	if strings.Contains(generated, `"bytes"`) {
		t.Fatalf("single-constructor error generated an unused bytes import:\n%s", generated)
	}
	if !strings.Contains(generated, `dec.UnknownConstructor("PingError", id)`) {
		t.Fatalf("single-constructor error does not reject an unknown constructor:\n%s", generated)
	}
}
