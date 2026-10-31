# Plug syntax support

This directory contains syntax highlighting for Plug schemas and PlugScript.

- `chroma` registers `Lexer` for the `plug` alias and `*.plug` files, and
  `Script` for the `purl` and `plugscript` aliases and `*.purl` files. Import it
  when an application renders either language with Chroma. Purl uses these same
  lexers for its REPL and help.
- `vim` contains file detection and syntax files for both languages. Copy its
  contents into a Vim runtime path, or add `plug/syntax/vim` to Vim's
  `runtimepath`.

Schemas follow [`../docs/schema.md`](../docs/schema.md). PlugScript is described
in [`../cmd/purl/doc.go`](../cmd/purl/doc.go). Run a saved script with:

```sh
$ purl -f channels.purl https://overseer.home.astrophena.name/plug
```

A `channels.purl` script can contain bindings, calls, and pipelines:

```purl
let channels = overseer.listChannels()
channels | map(c => c.identity.id)
```
