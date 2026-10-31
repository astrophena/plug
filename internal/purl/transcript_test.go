// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"flag"
	"fmt"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
)

var update = flag.Bool("update", false, "update transcript golden files")

func TestTranscripts(t *testing.T) {
	testutil.RunGolden(t, "testdata/*.txtar", func(t *testing.T, path string) []byte {
		archive, err := txtar.ParseFile(path)
		if err != nil {
			t.Fatal(err)
		}
		s, _ := testSession(t)
		var output strings.Builder
		for _, file := range archive.Files {
			fmt.Fprintf(&output, "-- %s --\n", file.Name)
			if err := new(Command).script(t.Context(), s, strings.NewReader(string(file.Data)), &output); err != nil {
				fmt.Fprintf(&output, "error: %v\n", err)
			}
		}
		return []byte(output.String())
	}, *update)
}
