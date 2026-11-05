# Internals

See the [Go API](https://pkg.go.dev/go.astrophena.name/plug) for usage. These
notes describe constraints on runtime and generator changes.

## Packages and generation

Generated clients call `plug.Call`, which encodes requests and sends them over
the connection. The client's read loop matches responses to pending calls. The
server decodes each request, authorizes it, and invokes the registered handler.

`plugimpl` defines the schema types and codecs used by generated bindings. It
is public so generated packages in other modules can import it. It cannot import
`plug`, since `plug` imports `plugimpl`.

The CLI parses a schema with `schema.Parse` and resolves its types, IDs, and
hashes with `schema.RuntimeSchema`. The generators use the resulting `Schema`
value to produce bindings or a JSON export. They must leave this value unchanged
and produce the same bytes for the same schema.

## Calls and buffers

Native calls and local `plug.Invoke` calls use the same registered handlers.
`plughttp` uses `Invoke` to run handlers for HTTP JSON routes locally.
`plugupgrade` passes HTTP Upgrade and WebSocket connections to
`Server.ServeConn`, which serves native Plug calls.

A canceled handler keeps its concurrency slot until it returns. Its result must
be discarded even if another call has reused the request ID. The pending-call
map tracks call identity separately from the number of running handlers.

Encoders come from a pool. Network calls must finish writing before releasing
one; local calls copy results before release. Decoded bytes can refer to the
frame payload, so returning a decoder to its pool must clear its payload
reference without changing the bytes.
