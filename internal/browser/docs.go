// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package browser

import (
	"go/doc/comment"
	"strings"
)

func (p page) docHTML(text string) string {
	printer := comment.Printer{
		HeadingLevel: 4,
		HeadingID:    func(*comment.Heading) string { return "" },
		DocLinkURL: func(link *comment.DocLink) string {
			if p.browser.hasMethod(link.Name) {
				return p.methodURL(link.Name)
			}
			return p.typeURL(link.Name)
		},
	}
	return string(printer.HTML(p.browser.docs.Parse(text)))
}

func (b *browser) docMarkdown(text string) string {
	printer := comment.Printer{
		HeadingLevel: 5,
		HeadingID:    func(*comment.Heading) string { return "" },
		DocLinkURL: func(link *comment.DocLink) string {
			if b.hasMethod(link.Name) {
				return "#method-" + link.Name
			}
			return "#type-" + link.Name
		},
	}
	return strings.TrimSuffix(string(printer.Markdown(b.docs.Parse(text))), "\n")
}

// markdownListDoc writes a description after a list item's label.
func (b *browser) markdownListDoc(sb *strings.Builder, text string) {
	if text == "" {
		sb.WriteByte('\n')
		return
	}
	sb.WriteString("\n\n")
	for line := range strings.SplitSeq(b.docMarkdown(text), "\n") {
		if line != "" {
			sb.WriteString("  ")
			// The Markdown printer uses tabs for code blocks. Expand them before
			// adding list indentation so they retain their four-space width.
			for strings.HasPrefix(line, "\t") {
				sb.WriteString("    ")
				line = line[1:]
			}
			sb.WriteString(line)
		}
		sb.WriteByte('\n')
	}
	sb.WriteByte('\n')
}
