// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package doc

import (
	"bytes"
	"flag"
	"fmt"
	"go/doc/comment"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
	"go.astrophena.name/plug"
)

var update = flag.Bool("update", false, "update documentation golden files")

func TestDocumentation(t *testing.T) {
	p := NewParser(&plug.Schema{
		Types: []plug.TypeInfo{
			{
				Name: "User",
			},
			{
				Name: "value",
			},
		},
		Methods: []plug.MethodInfo{
			{
				Name: "users.get",
			},
			{
				Name: "users.Get",
			},
			{
				Name: "Users.get",
			},
			{
				Name: "Users.Get",
			},
		},
	})
	printer := comment.Printer{
		HeadingID:  func(*comment.Heading) string { return "" },
		DocLinkURL: func(link *comment.DocLink) string { return "/schema/" + link.Name },
	}
	testutil.RunGolden(t, "testdata/*.txtar", func(t *testing.T, path string) []byte {
		ar, err := txtar.ParseFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(ar.Files[0].Data)
		normalized := Normalize(text)
		if got := Normalize(normalized); got != normalized {
			t.Fatalf("normalization is not idempotent:\n%s", got)
		}
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "-- normalized --\n%s\n", normalized)
		fmt.Fprintf(&buf, "-- generated comment --\n%s\n", p.Comment(text))
		fmt.Fprintf(&buf, "-- HTML --\n%s", printer.HTML(p.Parse(text)))
		fmt.Fprintf(&buf, "-- Markdown --\n%s", printer.Markdown(p.Parse(text)))
		return buf.Bytes()
	}, *update)
}
