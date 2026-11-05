// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rivo/uniseg"
)

// recordTable displays a list of records without changing its values.
func recordTable(v value, width, limit int, now time.Time) (string, bool) {
	xs, ok := v.data.([]value)
	if !ok || len(xs) == 0 {
		return "", false
	}
	first, ok := xs[0].data.(*record)
	if !ok || len(first.fields) == 0 || len(first.fields) > 8 {
		return "", false
	}
	for _, x := range xs {
		r, ok := x.data.(*record)
		if !ok || r.ctor != first.ctor || len(r.fields) != len(first.fields) {
			return "", false
		}
		for _, f := range first.fields {
			if _, ok := r.lookup(f.name); !ok {
				return "", false
			}
		}
	}

	indexWidth := max(1, len(strconv.Itoa(len(xs)-1)))
	cellWidth := min(32, (width-indexWidth-2*len(first.fields))/len(first.fields))
	if cellWidth < 8 {
		return "", false
	}
	n := len(xs)
	if limit > 0 {
		rowWidth := indexWidth + (cellWidth+2)*len(first.fields)
		n = min(n, max(1, limit/(rowWidth+1)-4))
	}
	var (
		rows      = make([][]string, 0, n)
		header    = []string{"#"}
		widths    = []int{indexWidth}
		shortened bool
	)
	for _, f := range first.fields {
		name := f.name
		if len(name) > 1024 {
			return "", false
		}
		if !identifier(name) {
			name = strconv.QuoteToGraphic(name)
		}
		text, clipped := clipCell(name, cellWidth)
		shortened = shortened || clipped
		header = append(header, text)
		widths = append(widths, uniseg.StringWidth(text))
	}
	for i, x := range xs[:n] {
		row := []string{strconv.Itoa(i)}
		for j, f := range first.fields {
			v, _ := x.data.(*record).lookup(f.name)
			text, clipped := tableCell(v, cellWidth, now)
			shortened = shortened || clipped
			row = append(row, text)
			widths[j+1] = max(widths[j+1], uniseg.StringWidth(text))
		}
		rows = append(rows, row)
	}

	var b strings.Builder
	writeRow(&b, header, widths)
	divider := make([]string, len(widths))
	for i, width := range widths {
		divider[i] = strings.Repeat("-", width)
	}
	writeRow(&b, divider, widths)
	shown := 0
	for _, row := range rows {
		var line strings.Builder
		writeRow(&line, row, widths)
		if limit > 0 && b.Len()+line.Len() > limit {
			break
		}
		b.WriteString(line.String())
		shown++
	}
	word := "rows"
	if len(xs) == 1 {
		word = "row"
	}
	fmt.Fprintf(&b, "\n%d %s", len(xs), word)
	if shown < len(xs) {
		fmt.Fprintf(&b, " (%d shown)", shown)
	}
	if shortened {
		b.WriteString("; cells shortened")
	}
	b.WriteString(". Use _[0] for a full row.\n")
	return b.String(), true
}

func writeRow(b *strings.Builder, cells []string, widths []int) {
	for i, cell := range cells {
		b.WriteString(cell)
		if i+1 < len(cells) {
			b.WriteString(strings.Repeat(" ", widths[i]-uniseg.StringWidth(cell)+2))
		}
	}
	b.WriteByte('\n')
}

func tableCell(v value, width int, now time.Time) (string, bool) {
	f := formatter{
		limit: 1024,
		now:   now,
	}
	f.value(v, 1)
	lines := strings.Split(f.b.String(), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	text := strings.Join(lines, " ")
	if f.truncated {
		text += "…"
	}
	text, clipped := clipCell(text, width)
	return text, clipped || f.truncated
}

func clipCell(text string, width int) (string, bool) {
	if uniseg.StringWidth(text) <= width {
		return text, false
	}
	var (
		g    = uniseg.NewGraphemes(text)
		used int
		end  int
	)
	for g.Next() {
		if used+g.Width() > width-1 {
			break
		}
		used += g.Width()
		_, end = g.Positions()
	}
	return text[:end] + "…", true
}

func highlightTable(text string) string {
	header, rest, _ := strings.Cut(text, "\n")
	return "\x1b[34m" + clean(header) + "\x1b[39m\n" + clean(rest)
}
