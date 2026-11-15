// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"strings"
	"testing"
)

func TestParseErrors(t *testing.T) {
	cases := map[string]struct {
		src  string
		want string
	}{
		"missing result": {
			`package x; item id: i64 =`,
			"constructor result type",
		},
		"duplicate access": {
			`package x; permission item.read; x.get -> void public requires item.read`,
			"access is declared more than once",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse("schema.plug", []byte(tc.src))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse() = %v; want %q", err, tc.want)
			}
		})
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte("package x\nx.ping -> void\n"))
	f.Add([]byte("package x; item text: string? = Item; x.get -> Item;"))
	f.Fuzz(func(t *testing.T, src []byte) {
		_, _ = Parse("fuzz.plug", src)
	})
}

func mustParse(t *testing.T, src string) *Schema {
	t.Helper()

	s, err := Parse("schema.plug", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
