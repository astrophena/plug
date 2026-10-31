// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"flag"
	"reflect"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
)

var update = flag.Bool("update", false, "update schema golden files")

func TestDocumentation(t *testing.T) {
	testutil.RunGolden(t, "testdata/*.txtar", func(t *testing.T, path string) []byte {
		ar, err := txtar.ParseFile(path)
		if err != nil {
			t.Fatal(err)
		}
		source := string(ar.Files[0].Data)
		parsed := mustParse(t, source)
		formatted := formatSchema(parsed)
		reparsed := mustParse(t, formatted)
		if got := formatSchema(reparsed); got != formatted {
			t.Fatalf("formatting is not idempotent:\n%s", got)
		}
		metadata := RuntimeSchema(parsed)
		if !reflect.DeepEqual(RuntimeSchema(reparsed), metadata) {
			t.Fatal("documentation changed after formatting")
		}
		crlf := mustParse(t, strings.ReplaceAll(source, "\n", "\r\n"))
		if !reflect.DeepEqual(RuntimeSchema(crlf), metadata) {
			t.Fatal("CRLF changed documentation")
		}
		var padded strings.Builder
		for line := range strings.SplitSeq(source, "\n") {
			padded.WriteString(line)
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				padded.WriteString(" \t ")
			}
			padded.WriteByte('\n')
		}
		if !reflect.DeepEqual(RuntimeSchema(mustParse(t, padded.String())), metadata) {
			t.Fatal("trailing whitespace changed documentation")
		}
		var undocumented strings.Builder
		for line := range strings.SplitSeq(source, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "//") {
				undocumented.WriteString(line + "\n")
			}
		}
		withoutDocs := RuntimeSchema(mustParse(t, undocumented.String()))
		if metadata.Family != withoutDocs.Family || metadata.Fingerprint != withoutDocs.Fingerprint {
			t.Fatal("documentation changed schema identity")
		}
		for i, method := range metadata.Methods {
			if method.ID != withoutDocs.Methods[i].ID {
				t.Fatalf("documentation changed method %s ID", method.Name)
			}
		}
		for i, typ := range metadata.Types {
			for j, constructor := range typ.Constructors {
				if constructor.ID != withoutDocs.Types[i].Constructors[j].ID {
					t.Fatalf("documentation changed constructor %s ID", constructor.Name)
				}
			}
		}
		return []byte(formatted)
	}, *update)
}
