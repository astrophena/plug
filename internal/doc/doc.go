// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package doc formats Plug documentation using Go doc comment syntax.
package doc

import (
	"go/doc/comment"
	"strings"

	"go.astrophena.name/plug"
)

// Normalize returns the standard Go formatting of text without comment markers
// or a trailing newline.
func Normalize(text string) string {
	var parser comment.Parser
	return printComment(parser.Parse(text))
}

func printComment(d *comment.Doc) string {
	var printer comment.Printer
	return strings.TrimSuffix(string(printer.Comment(d)), "\n")
}

// Parser resolves documentation references to schema types and methods.
// Unknown references remain literal text. Explicit URL links take precedence.
type Parser struct {
	names    map[string]bool
	prefixes map[string]bool
}

// NewParser returns a parser for s's type and method names.
func NewParser(s *plug.Schema) *Parser {
	p := &Parser{
		names:    make(map[string]bool, len(s.Types)+len(s.Methods)),
		prefixes: make(map[string]bool),
	}
	for _, typ := range s.Types {
		p.names[typ.Name] = true
	}
	for _, method := range s.Methods {
		p.names[method.Name] = true
		group, _, _ := strings.Cut(method.Name, ".")
		p.prefixes[group] = true
	}
	return p
}

// Parse parses text. Resolved DocLink nodes store the complete schema name in
// Name, with empty ImportPath and Recv fields.
func (p *Parser) Parse(text string) *comment.Doc { return p.parse(text, false) }

// Comment formats text with schema references lowered to plain names.
// Explicit URL links and preformatted text retain their syntax.
func (p *Parser) Comment(text string) string { return printComment(p.parse(text, true)) }

func (p *Parser) parse(text string, plain bool) *comment.Doc {
	parser := comment.Parser{
		LookupPackage: func(name string) (string, bool) {
			return name, p.names[name] || p.prefixes[name]
		},
		LookupSym: func(recv, name string) bool {
			if recv != "" {
				name = recv + "." + name
			}
			return p.names[name]
		},
	}
	d := parser.Parse(text)
	p.rewriteBlocks(d.Content, plain)
	return d
}

func (p *Parser) rewriteBlocks(blocks []comment.Block, plain bool) {
	for _, block := range blocks {
		switch block := block.(type) {
		case *comment.Paragraph:
			block.Text = p.rewriteText(block.Text, plain)
		case *comment.List:
			for _, item := range block.Items {
				p.rewriteBlocks(item.Content, plain)
			}
		}
	}
}

func (p *Parser) rewriteText(text []comment.Text, plain bool) []comment.Text {
	var result []comment.Text
	for _, part := range text {
		link, ok := part.(*comment.DocLink)
		if !ok {
			result = append(result, part)
			continue
		}
		// Without a Words map, doc links contain only plain text.
		var name strings.Builder
		for _, part := range link.Text {
			name.WriteString(string(part.(comment.Plain)))
		}
		switch {
		case !p.names[name.String()]:
			result = append(result, comment.Plain("["+name.String()+"]"))
		case plain:
			result = append(result, link.Text...)
		default:
			link.ImportPath, link.Recv, link.Name = "", "", name.String()
			result = append(result, link)
		}
	}
	return result
}
