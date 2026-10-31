// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package chroma

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// Script tokenizes PlugScript files and REPL input, including unfinished input.
var Script = lexers.Register(chroma.MustNewLexer(
	&chroma.Config{
		Name:      "PlugScript",
		Aliases:   []string{"purl", "plugscript"},
		Filenames: []string{"*.purl"},
	},
	func() chroma.Rules {
		return chroma.Rules{
			"root": {
				{
					Pattern: `//[^\n]*`,
					Type:    chroma.CommentSingle,
				},
				{
					Pattern: `(?s)/\*.*?(?:\*/|\z)`,
					Type:    chroma.CommentMultiline,
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
					Pattern: `\blet\b`,
					Type:    chroma.KeywordDeclaration,
				},
				{
					Pattern: `\b(?:true|false|nil|null)\b`,
					Type:    chroma.KeywordConstant,
				},
				{
					Pattern: `[\p{L}_][\p{L}\p{Nd}_]*(?=\s*:)`,
					Type:    chroma.NameAttribute,
				},
				{
					Pattern: chroma.Words(`(?<!\.)\b`, `\b`, "map", "filter", "take", "count", "sortBy", "keys", "parseJSON", "toJSON", "bytes", "text", "find", "any", "all", "contains", "startsWith", "endsWith", "uniqueBy", "groupBy", "sum", "age", "assert"),
					Type:    chroma.NameBuiltin,
				},
				{
					Pattern: `\b0[xX][\da-fA-F_]+(?:\.[\da-fA-F_]*)?[pP][+-]?[\d_]+`,
					Type:    chroma.NumberFloat,
				},
				{
					Pattern: `(?:\b\d[\d_]*\.[\d_]*|\.[\d_]+)(?:[eE][+-]?[\d_]+)?|\b\d[\d_]*[eE][+-]?[\d_]+`,
					Type:    chroma.NumberFloat,
				},
				{
					Pattern: `\b0[xX][\da-fA-F_]+`,
					Type:    chroma.NumberHex,
				},
				{
					Pattern: `\b0[bB][01_]+`,
					Type:    chroma.NumberBin,
				},
				{
					Pattern: `\b0[oO][0-7_]+`,
					Type:    chroma.NumberOct,
				},
				{
					Pattern: `\b\d[\d_]*`,
					Type:    chroma.NumberInteger,
				},
				{
					Pattern: `[\p{L}_][\p{L}\p{Nd}_]*(?:\.[\p{L}_][\p{L}\p{Nd}_]*)*(?=\s*\()`,
					Type:    chroma.NameFunction,
				},
				{
					Pattern: `[\p{L}_][\p{L}\p{Nd}_]*`,
					Type:    chroma.Name,
				},
				{
					Pattern: `=>|==|!=|<=|>=|&&|\|\||[+*/%!=<>|\-]`,
					Type:    chroma.Operator,
				},
				{
					Pattern: `[()\[\]{},:;.]`,
					Type:    chroma.Punctuation,
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
					Pattern: `\\$`,
					Type:    chroma.StringEscape,
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
