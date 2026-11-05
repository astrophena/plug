// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Purl calls Plug services and evaluates PlugScript expressions.

	$ purl [FLAGS] ENDPOINT
	$ purl -e 'greeting.greet(name: "Ada")' 127.0.0.1:9000
	$ purl -f script.purl -json 127.0.0.1:9000

Without -e or -f, a terminal opens the interactive prompt; other input is read as
a script. Use -f - to read a script from stdin. Flags must precede the endpoint.

# Endpoints

Use an absolute Unix socket path, a TCP host:port address, or an http, https,
ws, or wss URL. Bare TCP addresses need a numeric port; bracket IPv6 addresses.
The unix:// and tcp:// forms also work. HTTP URLs must point to a native Plug
Upgrade endpoint. HTTPS and WSS verify server certificates. -H adds a request
header. Raw methods are not supported.

# Interactive use

Tab completes methods and arguments. :help shows methods, types, and builtins;
:schema shows the schema; :vars lists bindings; :quit exits. Ctrl-R searches
history, Ctrl-C cancels input or evaluation, and Ctrl-D exits.

Long output uses $PAGER or less -FRX. NO_PAGER disables paging, and NO_COLOR
disables highlighting. -json writes complete JSON without tables or annotations.

History is stored with mode 0600 at $XDG_STATE_HOME/purl/history, falling back to
$HOME/.local/state/purl/history. It may contain secrets. Start input with a space
to exclude it, or use -no-history. Credentials in flags can also appear in shell
history or process listings.

PlugScript syntax and examples:
https://github.com/astrophena/plug/blob/master/docs/purl.md
*/
package main

import (
	_ "embed"

	"go.astrophena.name/base/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }
