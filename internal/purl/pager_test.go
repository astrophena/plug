// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"testing"
)

func TestOverflows(t *testing.T) {
	cases := map[string]struct {
		text          string
		width, height int
		want          bool
	}{
		"short": {
			"hello\n",
			80,
			24,
			false,
		},
		"lines": {
			"a\nb\nc\n",
			80,
			3,
			true,
		},
		"wrapped": {
			"12345678901",
			5,
			3,
			true,
		},
		"exact width": {
			"12345",
			5,
			3,
			false,
		},
		"wide runes": {
			"世界世界世界",
			5,
			3,
			true,
		},
		"tabs": {
			"\t\t\t",
			10,
			3,
			true,
		},
		"unknown size": {
			"hello\n",
			0,
			0,
			false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := overflows(tc.text, tc.width, tc.height); got != tc.want {
				t.Fatalf("overflows(%q, %d, %d) = %v, want %v", tc.text, tc.width, tc.height, got, tc.want)
			}
		})
	}
}

func TestPagerArgs(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		pager, disabled, term string
		want                  []string
	}{
		"arguments": {
			fmt.Sprintf("%q -test.run='Test Pager'", exe),
			"",
			"xterm",
			[]string{exe, "-test.run=Test Pager"},
		},
		"disabled": {
			exe,
			"1",
			"xterm",
			nil,
		},
		"dumb terminal": {
			exe,
			"",
			"dumb",
			nil,
		},
		"invalid quote": {
			"'unterminated",
			"",
			"xterm",
			nil,
		},
		"empty command": {
			" ",
			"",
			"xterm",
			nil,
		},
		"missing command": {
			"/purl-no-such-pager",
			"",
			"xterm",
			nil,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := pagerArgs(func(key string) string {
				switch key {
				case "PAGER":
					return tc.pager
				case "NO_PAGER":
					return tc.disabled
				case "TERM":
					return tc.term
				}
				return ""
			})
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("default", func(t *testing.T) {
		got := pagerArgs(func(string) string { return "" })
		path, err := exec.LookPath("less")
		if err != nil {
			if got != nil {
				t.Fatalf("got %q without less", got)
			}
			return
		}
		if want := []string{path, "-FRX"}; !slices.Equal(got, want) {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}
