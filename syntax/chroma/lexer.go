// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package chroma registers Chroma lexers for Plug schemas and PlugScript.
//
// [Lexer] handles the "plug" alias and .plug files; [Script] handles the "purl"
// and "plugscript" aliases and .purl files.
package chroma

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// Lexer tokenizes Plug schema files.
var Lexer = lexers.Register(chroma.MustNewLexer(
	&chroma.Config{
		Name:      "Plug",
		Aliases:   []string{"plug"},
		Filenames: []string{"*.plug"},
	},
	func() chroma.Rules {
		return chroma.Rules{
			"root": {
				{
					Pattern: `//[^\n]*`,
					Type:    chroma.CommentSingle,
				},
				{
					Pattern: `\s+`,
					Type:    chroma.Text,
				},
				{
					Pattern: `"`,
					Type:    chroma.StringDouble,
					Mutator: chroma.Push("string"),
				},
				{
					Pattern: chroma.Words(`\b`, `\b`,
						"category", "http", "httpStatus", "package", "permission", "public",
						"requires", "throws", "type", "version",
					),
					Type: chroma.Keyword,
				},
				{
					Pattern: chroma.Words(`\b`, `\b`,
						"bool", "bytes", "f64", "i32", "i64", "list", "raw", "string",
						"u32", "u64", "void",
					),
					Type: chroma.KeywordType,
				},
				{
					Pattern: `\b(?:DELETE|GET|PATCH|POST|PUT)\b`,
					Type:    chroma.NameBuiltin,
				},
				{
					Pattern: `\b[A-Za-z_][A-Za-z0-9_]*\.[A-Za-z_][A-Za-z0-9_]*\b`,
					Type:    chroma.NameFunction,
				},
				{
					Pattern: `\b[A-Za-z_][A-Za-z0-9_]*(?=\s*:)`,
					Type:    chroma.NameAttribute,
				},
				{
					Pattern: `\b[A-Z][A-Za-z0-9_]*\b`,
					Type:    chroma.NameClass,
				},
				{
					Pattern: `^[A-Za-z_][A-Za-z0-9_]*`,
					Type:    chroma.NameFunction,
				},
				{
					Pattern: `\b[0-9]+\b`,
					Type:    chroma.NumberInteger,
				},
				{
					Pattern: `->|[=?:<>]`,
					Type:    chroma.Operator,
				},
				{
					Pattern: `;`,
					Type:    chroma.Punctuation,
				},
				{
					Pattern: `[A-Za-z_][A-Za-z0-9_.]*`,
					Type:    chroma.Name,
				},
				{
					Pattern: `.`,
					Type:    chroma.Error,
				},
			},
			"string": {
				{
					Pattern: `"`,
					Type:    chroma.StringDouble,
					Mutator: chroma.Pop(1),
				},
				{
					Pattern: `\\.`,
					Type:    chroma.StringEscape,
				},
				{
					Pattern: `[^"\\\n]+`,
					Type:    chroma.StringDouble,
				},
				{
					Pattern: `\n`,
					Type:    chroma.Error,
					Mutator: chroma.Pop(1),
				},
			},
		}
	},
))
