# Plug syntax support

This directory contains syntax highlighting for Plug schemas and PlugScript.

- `chroma` registers `Lexer` for the `plug` alias and `*.plug` files, and
  `Script` for the `purl` and `plugscript` aliases and `*.purl` files. Import it
  when an application renders either language with Chroma. Purl uses these same
  lexers for its REPL and help.
- `vim` contains file detection and syntax files for both languages. Copy its
  contents into a Vim runtime path, or add `plug/syntax/vim` to Vim's
  `runtimepath`.

See the [schema reference](../docs/schema.md) and [Purl guide](../docs/purl.md)
for the languages.
