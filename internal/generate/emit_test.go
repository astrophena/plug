// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import "testing"

func TestComments(t *testing.T) {
	for name, sep := range map[string]string{
		"newline":             "\n",
		"return":              "\r",
		"CRLF":                "\r\n",
		"line separator":      "\u2028",
		"paragraph separator": "\u2029",
	} {
		t.Run(name, func(t *testing.T) {
			var e emitter
			e.comments("  ", []string{"safe" + sep + "throw new Error();"})
			const want = "  // safe\n  // throw new Error();\n"
			if got := e.buf.String(); got != want {
				t.Fatalf("comments = %q; want %q", got, want)
			}
		})
	}
}
