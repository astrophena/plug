// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"bytes"
	"fmt"
	"strings"

	"go.astrophena.name/plug/internal/doc"
)

type emitter struct {
	buf    bytes.Buffer
	indent string
	docs   *doc.Parser
}

func (e *emitter) line(format string, values ...any) {
	fmt.Fprintf(&e.buf, format+"\n", values...)
}

func (e *emitter) doc(name, sourceName, fallback, documentation string) {
	e.comments("", formatDocLines(name, sourceName, fallback, e.docs.Comment(documentation)))
}

func (e *emitter) docIndented(name, sourceName, fallback, documentation string) {
	e.comments(e.indent, formatDocLines(name, sourceName, fallback, e.docs.Comment(documentation)))
}

func (e *emitter) comments(indent string, lines []string) {
	// JavaScript also treats Unicode line separators as comment terminators.
	split := strings.NewReplacer("\r\n", "\n", "\r", "\n", "\u2028", "\n", "\u2029", "\n")
	for _, line := range lines {
		for line := range strings.SplitSeq(split.Replace(line), "\n") {
			if line == "" {
				e.line("%s//", indent)
			} else if strings.HasPrefix(line, "\t") {
				e.line("%s//%s", indent, line)
			} else {
				e.line("%s// %s", indent, line)
			}
		}
	}
}

func (g *goGenerator) writeGoPackageDoc(title string) {
	g.doc("Package "+g.schema.Package, g.schema.Package, "contains generated "+title+".", g.schema.Documentation)
}

func formatDocLines(name, sourceName, fallback, doc string) []string {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return []string{name + " " + fallback}
	}

	lines := strings.Split(doc, "\n")
	if strings.HasPrefix(lines[0], sourceName+" ") {
		lines[0] = name + lines[0][len(sourceName):]
		return lines
	}
	if strings.HasPrefix(lines[0], name+" ") {
		return lines
	}
	return append([]string{name + " " + fallback, ""}, lines...)
}
