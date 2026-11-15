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
	var s plug.Schema
	for _, name := range []string{"User", "value"} {
		s.Types = append(s.Types, plug.TypeInfo{
			Name: name,
		})
	}
	for _, name := range []string{"users.get", "users.Get", "Users.get", "Users.Get"} {
		s.Methods = append(s.Methods, plug.MethodInfo{
			Name: name,
		})
	}
	p := NewParser(&s)
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
		testutil.AssertEqual(t, Normalize(normalized), normalized)

		var (
			buf    bytes.Buffer
			parsed = p.Parse(text)
		)
		fmt.Fprintf(&buf, "-- normalized --\n%s\n", normalized)
		fmt.Fprintf(&buf, "-- generated comment --\n%s\n", p.Comment(text))
		fmt.Fprintf(&buf, "-- HTML --\n%s", printer.HTML(parsed))
		fmt.Fprintf(&buf, "-- Markdown --\n%s", printer.Markdown(parsed))
		return buf.Bytes()
	}, *update)
}
