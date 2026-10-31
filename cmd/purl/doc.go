// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Purl explores and calls Plug services.

PlugScript is Purl's expression language for calling Plug RPC methods and
working with their results. It combines values, bindings, functions, and
pipelines. Use it at the interactive prompt or in .purl scripts.

# Connect

	$ purl [FLAGS] ENDPOINT

An endpoint can be an absolute Unix socket path, a TCP host:port address
(numeric port), or an http, https, ws, or wss URL. IPv6 needs brackets:
[::1]:9000. The unix:// and tcp:// URL forms also work.

	$ purl /run/greeter/plug.sock
	$ purl localhost:9000
	$ purl https://overseer.home.astrophena.name/plug
	$ purl wss://overseer.home.astrophena.name/plug

Socket paths and TCP addresses connect to native Plug listeners. Overseer
accepts Plug connections through HTTP Upgrade at its /plug URL.

Purl reads the server's schema and opens an interactive prompt. HTTPS and WSS
check server certificates. WebSocket sends keep-alive pings for idle
connections. -H adds an Upgrade request header. Raw methods are not supported.

The service examples below use Overseer. Your server may have different
methods; Tab and :help show what is available without making calls.

# Values

PlugScript supports arithmetic, strings, booleans, nil, lists, and records:

	1 + 2
	"hello"
	true
	nil
	[1, 2, 3]
	{name: "Alice", active: true}

Lists use square brackets and zero-based indexes. Records use curly braces;
read their fields with a dot or string index:

	let names = ["Alice", "Bob"]
	names[0] // "Alice"
	let person = {name: "Alice", active: true}
	person.name // "Alice"
	person["name"] // "Alice"

nil means a missing value; null is accepted as an alias.

Use +, -, *, /, and % for arithmetic; ==, !=, <, <=, >, and >= for comparisons.
&&, ||, and ! work on booleans. && and || stop as soon as the answer is known.
Integers stay exact, even when they are larger than a machine integer.
// starts a comment; block comments use slash-star and star-slash.

# Bindings

let binds an expression. It runs on first use and caches its value or error,
including cancellation:

	let channels = overseer.listChannels()
	channels // Makes the call.
	count(channels) // Reuses the result.

A binding can contain a mutation, so first use can change server state.

Reusing a name leaves existing bindings unchanged:

	let x = 1
	let saved = x
	let x = 2
	saved

saved is 1. Bindings retain the scope in which they were defined.

# RPC calls

Enter a method name to read its documentation; add parentheses to call it:

	overseer.listChannels
	overseer.listChannels()

RPC arguments are named:

	overseer.getChannel(channelID: channels[0].identity.id)

Use :help overseer.getChannel to inspect the arguments. This example requires
at least one channel.

Purl reconnects before the next call after detecting a lost connection. It
keeps bindings and requires the same schema. Calls can change server state;
failed calls are never retried automatically.

# Functions

Use => to define a function. Each call evaluates its body:

	let twice = x => x * 2
	twice(3)
	let add = (a, b) => a + b
	add(2, 3)

For fresh server data, use a function with no arguments:

	let refresh = () => overseer.listChannels()
	refresh()
	refresh()

Each refresh() makes a new RPC call. Functions retain their defining scope.

Bindings capture older scopes. Pass a function to itself for recursion:

	let done = (self, n) => n == 0 || self(self, n - 1)
	done(done, 100000)

Tail calls reuse the evaluator's stack, including calls through pipelines and
short-circuit boolean checks. This example runs beyond the ordinary 256-level
evaluation limit. Other nested evaluation is still limited. The expression
timeout and Ctrl-C still stop evaluation.

# Pipelines

A pipeline passes its left value as the first argument to the next function:

	[1, 2, 3] | map(x => x * 2) | filter(x => x > 2)

This produces [4, 6]. map transforms items; filter keeps matches.
x | f(y) means f(x, y). Omit parentheses when there are no other arguments:

	channels | count
	channels | take(3) | map(c => c.identity.id)

Pipelines process values already fetched; they do not stream or fetch more
pages. Function arguments are evaluated before the function runs.

# Builtins

Use :help to list builtins or :help NAME for one builtin:

	let rows = [{name: "Alice", team: "ops", size: 3}, {name: "Bob", team: "ops", size: 5}]
	rows | find(r => startsWith(r.name, "Al"))
	rows | any(r => contains(r.name, "ob"))
	rows | all(r => endsWith(r.name, "e"))
	rows | map(r => r.size) | sum
	rows | uniqueBy(r => r.team)
	rows | groupBy(r => r.team) | map(g => {team: g.key, people: count(g.items)})

find returns the first match or nil. any and all stop when the result is known.
uniqueBy keeps the first item for each key. groupBy returns {key, items}
records in first-seen order. Keys use PlugScript equality. sum adds numbers
and returns 0 for an empty list. String matching is case-sensitive.

age(timestamp) returns whole seconds since an RFC 3339 timestamp; future times
have negative ages. assert stops evaluation with a message if its condition
is false:

	assert(count(channels) == 1, "Expected exactly one channel")

A successful assertion returns nil. Both arguments are evaluated first.

# Results and display

_ holds the complete last successful result. Save it before another successful
expression replaces it:

	rows
	let result = _
	result[0]
	result[0].name

The prompt displays lists of similarly shaped records as a table when they
fit. The # column gives the list index. Long cells may be shortened; use _[0]
or a saved result to read a complete row. Mixed shapes and narrow terminals
use ordinary record display. Formatting makes no extra calls and leaves the
data unchanged.

Long output uses $PAGER, or less -FRX when available. NO_PAGER disables paging.
Without a pager, large results and tables are shortened. Timestamp strings in
REPL results include a relative time such as "3 hours ago". Those annotations
are only for display.

# Scripts

Save expressions in a .purl file, such as channels.purl:

	let channels = overseer.listChannels()
	channels | map(c => c.identity.id)

Run it with flags before the endpoint:

	$ purl -f channels.purl https://overseer.home.astrophena.name/plug
	$ purl -json -f channels.purl https://overseer.home.astrophena.name/plug
	$ purl -e 'overseer.getStatus()' https://overseer.home.astrophena.name/plug

Scripts also accept stdin and stop at the first error. They use ordinary value
display with no tables or pager. -json writes complete JSON, using null for
nil and omitting colors and timestamp annotations.
parseJSON and toJSON convert values; bytes and text convert UTF-8 strings.

# REPL keys and history

Tab completes methods and arguments without making calls. :help explores the
language and API, :schema shows the schema, and :vars lists bindings.
Ctrl-R searches history. Ctrl-C cancels input or evaluation. Ctrl-D and :quit
exit. NO_COLOR disables highlighting, which otherwise uses your terminal's
palette.

History lives at $XDG_STATE_HOME/purl/history, or
$HOME/.local/state/purl/history, with mode 0600. It may contain secrets.
Start input with a space to exclude it, or use -no-history to disable history.
Credentials in flags may also appear in shell history or process listings.
*/
package main

import (
	_ "embed"

	"go.astrophena.name/base/cli"
)

//go:embed doc.go
var doc []byte

func init() { cli.SetDocComment(doc) }
