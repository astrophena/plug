// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Plug generates RPC bindings, checks schemas, and serves API documentation.

# Generate bindings

	$ plug generate [-check] [-http-client|-ts|-json] -o OUTPUT SCHEMA.plug

Generate native Go bindings by default. Use -http-client for a standalone Go
HTTP client, -ts for TypeScript types and an HTTP client, or -json for the
resolved schema:

	$ plug generate -o greeter_plug.go schema.plug
	$ plug generate -http-client -o client.go schema.plug
	$ plug generate -ts -o client.ts schema.plug
	$ plug generate -json -o schema.json schema.plug

Choose one target. The Go HTTP client uses only the standard library. JSON
includes resolved types, wire IDs, and handshake hashes.

Generation replaces the output atomically. Add -check to report missing or
stale output without writing:

	$ plug generate -check -o greeter_plug.go schema.plug

# Browse documentation

	$ plug doc [-addr ADDRESS] SCHEMA.plug

Serve HTML reference pages and a Markdown download at /index.md. The default
address is localhost:8080. An absolute path selects a Unix socket:

	$ plug doc -addr /tmp/plug-doc.sock schema.plug

Ctrl-C stops the server. Restart it after schema changes.

# Check compatibility

	$ plug check OLD.plug NEW.plug

Check whether the new schema is an additive replacement for the old one.
Changes to required fields, access rules, or existing declarations can break
compatibility. Optional extension fields and new declarations may be added.

# Lint schemas

	$ plug lint SCHEMA.plug

Report unused permissions and types and missing documentation comments.
Successful checks print nothing.

# Inspect a server

	$ plug inspect [-network NETWORK] ADDRESS

Print the wire version, schema family, fingerprint, and canonical source.
Inspection makes no method calls and uses a native Plug connection. The
default network is unix:

	$ plug inspect /run/greeter/plug.sock
	$ plug inspect -network tcp localhost:9000

Transport authentication still applies.

# Check staged schemas

	$ plug pre-commit [-dir DIRECTORY] [-no-color]

Lint tracked .plug files from the Git index and check compatibility against
HEAD. Unstaged edits do not affect the result. A higher major version in the
same package starts a new compatibility family.

The directory defaults to the current directory. Diagnostics go to stderr.
Use -no-color to disable color.
*/
package main

import (
	_ "embed"

	"go.astrophena.name/base/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }
