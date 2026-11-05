// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Plug generates RPC bindings, checks schemas, and serves API documentation.

# Generate bindings

	$ plug generate [-check] [-http-client|-ts|-json] -o OUTPUT SCHEMA.plug

The default output is native Go bindings. Choose -http-client for a standalone
Go HTTP client, -ts for TypeScript types and a fetch client, or -json for resolved
types, wire IDs, and schema hashes. The Go HTTP client uses only the standard
library.

Generation replaces the output atomically. Add -check to report missing or
stale output without writing.

# Browse documentation

	$ plug doc [-addr ADDRESS] SCHEMA.plug

Serve HTML reference pages and a Markdown download at /index.md. The default
address is localhost:8080. An absolute path selects a Unix socket:

	$ plug doc -addr /tmp/plug-doc.sock schema.plug

Ctrl-C stops the server. Restart it after schema changes.

# Check compatibility

	$ plug check OLD.plug NEW.plug

Check whether the new schema is an additive replacement for the old one.
Add declarations, constructors to existing sums, and optional fields. Required
fields, access rules, and existing HTTP bindings must stay unchanged.

# Lint schemas

	$ plug lint SCHEMA.plug

Report unused permissions and types and missing documentation comments.
Successful checks print nothing.

# Inspect a server

	$ plug inspect [-network NETWORK] ADDRESS

Print the wire version, schema hashes, and source over a native connection.
The default network is unix:

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
