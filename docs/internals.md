# Internals

Applications use `plug` and their generated service package.

| Package            | Owns                                               |
| ------------------ | -------------------------------------------------- |
| `plug`             | Connections, calls, authentication, shutdown       |
| Generated bindings | Service types, handlers, clients                   |
| `plugimpl`         | Shared schema metadata, codecs, method descriptors |
| `plughttp`         | JSON routes and schema browser                     |
| `plugupgrade`      | Native connections over HTTP Upgrade or WebSocket  |

`plugimpl` is outside `internal` so generated packages elsewhere in the module
can import it. It does not depend on `plug` or register runtime callbacks.

## Generation

The CLI parses and validates a schema, resolves its metadata, then passes it to
the Go, Go HTTP, TypeScript, or JSON generator:

```text
schema.Parse → schema.RuntimeSchema → generate
```

Parsing assigns wire IDs and rejects collisions. The family hash identifies the
package and major version. The fingerprint identifies the wire contract;
documentation and categories do not affect it.

Generated code exports the service API and keeps codecs, IDs, and descriptors
private. HTTP clients choose the first binding in canonical verb/path order;
servers expose every binding. Output is deterministic and committed with its
schema.

## Calls

`NewClient` and `NewServer` validate the schema and keep independent copies.
`Server.Schema` also returns a copy.

A generated client method calls `plug.Call`, which checks the descriptor,
encodes the request, and sends it through the client. A read loop matches
response IDs to pending calls. The caller decodes the response or declared
error. Canceling a call removes it from the pending set and sends a cancel
frame. A malformed response closes the connection.

Generated handler registration builds a batch of decode, handle, and encode
callbacks. `Server.RegisterHandlers` validates the batch before changing the
handler map. `Server.Seal` checks for missing handlers and authorizers, then
closes registration. Serving and HTTP adapter construction call it
automatically.

The server decodes the request, authorizes private methods, runs the handler,
and encodes the result. Handler panics and undeclared errors become internal
protocol errors. `ReportError` receives diagnostics; `ObserveCall` receives
completed call events. Shutdown rejects new calls, cancels active calls on
native and HTTP transports, and waits for handlers to finish or the caller's
context to expire.

`plughttp` uses `plug.Invoke` to enter this call path without a network round
trip. `plugupgrade` hands upgraded HTTP connections and binary WebSocket streams
to `Server.ServeConn`.

## Connections and buffers

Connections start with a schema handshake. Family matching accepts additive
schema changes; `StrictSchema` requires matching fingerprints. Compression is
negotiated during the handshake. `Dial` retries without compression if the peer
does not support it; `NewClient` cannot replace a supplied connection.

Records and sum constructors have length-delimited bodies. Decoders skip
unfamiliar extension fields and preserve unknown constructors for forward
compatibility. Limits bound frames, decoded allocations, lists, and concurrent
server calls. Canceled handlers retain their concurrency slot until they return.
Their call IDs can be reused, but their results are discarded.

Encoders are pooled. A payload stays valid until its encoder is released.
Network calls finish writing before release; local calls copy their results.
Decoded byte slices may refer to the frame payload, which belongs to the result.

`plug.OpenRaw` opens a raw method on a dedicated connection. The method must be
the first request. Before acceptance, the handler can return a declared error.
After acceptance, framing stops and the handler owns the byte stream. The raw
connection preserves buffered bytes and closes when its context is canceled.

See the [schema reference](schema.md), [wire protocol](protocol.md), and
[JSON schema format](schema-json.md) for the formats and compatibility rules.
