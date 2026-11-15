// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"flag"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
)

var update = flag.Bool("update", false, "update schema golden files")

func TestFormat(t *testing.T) {
	testutil.RunGolden(t, "testdata/*.txtar", func(t *testing.T, path string) []byte {
		ar, err := txtar.ParseFile(path)
		if err != nil {
			t.Fatal(err)
		}

		var (
			s    = mustParse(t, string(ar.Files[0].Data))
			want = RuntimeSchema(s)
			got  = RuntimeSchema(mustParse(t, want.Source))
		)
		testutil.AssertEqual(t, got, want)

		return []byte(want.Source)
	}, *update)
}
