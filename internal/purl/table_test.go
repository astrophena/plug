// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/txtar"
	"go.astrophena.name/plug/example/greeter"

	"github.com/rivo/uniseg"
)

func TestRecordTables(t *testing.T) {
	testutil.RunGolden(t, "testdata/tables/*.txtar", func(t *testing.T, path string) []byte {
		archive, err := txtar.ParseFile(path)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
		s := newSession(nil, &greeter.Schema)
		var b strings.Builder
		for _, file := range archive.Files {
			v, err := evaluate(t, s, string(file.Data))
			if err != nil {
				t.Fatal(err)
			}
			before, jsonErr := marshalJSON(v)
			text, ok := recordTable(v, 80, 0, now)
			if !ok {
				text = formatAt(v, 0, now) + "\n"
			} else {
				if uncolor(highlightTable(text)) != text {
					t.Fatal("table highlighting changed text")
				}
				body, _, _ := strings.Cut(text, "\n\n")
				for line := range strings.SplitSeq(body, "\n") {
					if uniseg.StringWidth(line) > 80 {
						t.Fatalf("table exceeds terminal width: %q", line)
					}
				}
			}
			if after, err := marshalJSON(v); jsonErr == nil && (err != nil || after != before) {
				t.Fatal("table changed the value or its JSON representation")
			}
			b.WriteString("-- " + file.Name + " --\n" + text)
		}
		return []byte(b.String())
	}, *update)
}

func TestTableClipping(t *testing.T) {
	for _, text := range []string{"東京京都", "éééé", "👩‍💻👩‍💻", "👍🏽👍🏽", "🇪🇺🇪🇺"} {
		got, clipped := clipCell(text, 3)
		if !utf8.ValidString(got) || uniseg.StringWidth(got) > 3 || !clipped || !strings.HasSuffix(got, "…") {
			t.Fatalf("bad clipped cell: %q -> %q", text, got)
		}
	}
}

func TestTableLimitsAndSafety(t *testing.T) {
	v, err := evaluate(t, newSession(nil, &greeter.Schema), `[{id: 1, title: "safe"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := recordTable(v, 12, 0, time.Time{}); ok {
		t.Fatal("used a table in a narrow terminal")
	}
	r := v.data.([]value)[0].data.(*record)
	r.fields[1].name = "title\x1b[2J\n\t"
	r.fields[1].value.data = strings.Repeat("é", 1000) + "\x1b[2J\n\t"
	v.data = make([]value, 1000)
	for i := range v.data.([]value) {
		v.data.([]value)[i] = value{
			data: r,
		}
	}
	text, ok := recordTable(v, 80, 1024, time.Time{})
	if !ok || !strings.Contains(text, "shown)") || !strings.Contains(text, "cells shortened") {
		t.Fatal("large table was not visibly shortened")
	}
	if len(text) > 1224 || !utf8.ValidString(text) || strings.ContainsAny(text, "\x1b\t\x00") {
		t.Fatalf("unsafe or unbounded table: %q", text)
	}
}
