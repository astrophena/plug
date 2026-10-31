# Using Plug

Plug turns a schema into Go types and typed RPC bindings. Applications work with
those generated bindings and the small runtime API in package `plug`.

This guide builds a native Plug service and calls it. The [schema] and
[protocol] specifications cover the full language and wire format.

## Define a schema

Put the service contract in a `.plug` file. For example:

```plug
package greeter version 1

permission user.read

user
  id: i64
  name: string
= User

type LookupError

notFound
  id: i64
= LookupError

greeter.getUser
  id: i64
-> User
  throws LookupError
  requires user.read

greeter.ping -> void
  public
```

The package name becomes the generated Go package. The major version identifies
the compatibility family. Increase it for a breaking schema change.

Methods are public by default. Use `requires` to restrict a method to a declared
permission. The `public` clause makes anonymous access explicit.

See [schema.md](schema.md) for records, sums, optional fields, lists, raw
methods, HTTP bindings, and compatibility rules.

## Generate Go bindings

Keep a small generation file next to the schema:

```go
package greeter

//go:generate go tool plug generate -o greeter_plug.go schema.plug
```

Run generation from that package:

```sh
$ go generate
```

Commit the generated file with the schema. Generation is deterministic. The
public declarations are the parts an application needs:

- schema types, request types, and declared error types;
- permission constants and the `Schema` value;
- `Handlers` and `RegisterHandlers` for servers; and
- `Client` and `NewClient` for callers.

Codecs, wire IDs, and method descriptors stay in generated implementation code.

## Implement the service

Create a Plug server from the generated schema, then register every handler:

```go
func newServer() (*plug.Server, error) {
	server, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{
		Security: plug.Security{
			Authenticate: authenticate,
			Authorize:    authorize,
		},
	})
	if err != nil {
		return nil, err
	}

	if err := greeter.RegisterHandlers(server, greeter.Handlers{
		GetUser: func(ctx context.Context, req greeter.GetUserRequest) (greeter.User, error) {
			user, found := findUser(req.ID)
			if !found {
				return greeter.User{}, &greeter.NotFound{ID: req.ID}
			}
			return user, nil
		},
		Ping: func(context.Context) error {
			return nil
		},
	}); err != nil {
		return nil, err
	}
	return server, nil
}
```

`RegisterHandlers` is atomic. It rejects a batch if a field is nil, a method
does not belong to the schema, or a method already has a handler. Serving seals
the server, so later registration fails.

The zero value of `ServerOptions` selects the default resource limits.
`Limits.MaxDecodeBytes` bounds allocations per native payload, with a default of
64 MiB. `MaxListElements` bounds each encoded list and the total list elements
decoded from one payload. Clients apply the same limits to responses and errors.

## Authentication and authorization

`Authenticate` derives a `plug.Principal` from a native connection. `Authorize`
runs before each private handler and receives the principal, permission, method
metadata, and decoded request.

Plug does not define credentials. The transport boundary may use Unix peer
credentials, TLS state, or another property of the connection. Keep credential
parsing in `Authenticate` and permission policy in `Authorize`.

A server with private methods must have an authorizer before it starts serving.
Public methods do not require a principal.

## Serve native connections

`Serve` accepts concurrent calls from a listener:

```go
func serve(ctx context.Context, server *plug.Server) error {
	listener, err := net.Listen("unix", "/run/greeter/plug.sock")
	if err != nil {
		return err
	}
	return server.Serve(ctx, listener)
}
```

`Serve` takes ownership of the listener and accepted connections. `ServeConn`
takes ownership of a connection when another component performs the accept.

Cancel the serving context to stop accepting connections. `Shutdown` rejects new
calls, cancels active call contexts, and waits for their handlers up to the
caller's deadline. This applies to native and HTTP calls. Calling `Shutdown`
again waits for the same shutdown to finish.

## Call the service

`Dial` connects, checks the schema, and owns the resulting connection:

```go
raw, err := plug.Dial(
	ctx,
	"unix",
	"/run/greeter/plug.sock",
	&greeter.Schema,
	plug.ClientOptions{},
)
if err != nil {
	return err
}

client := greeter.NewClient(raw)
defer func() {
	if err := client.Close(); err != nil {
		log.Printf("close Plug client: %v", err)
	}
}()

user, err := client.GetUser(ctx, greeter.GetUserRequest{ID: 42})
if err != nil {
	var notFound *greeter.NotFound
	if errors.As(err, &notFound) {
		return fmt.Errorf("user %d does not exist", notFound.ID)
	}
	return err
}
fmt.Println(user.Name)
```

Use `NewClient` instead of `Dial` when the application already has a `net.Conn`.
It performs the same handshake and takes ownership of the connection.

A client supports concurrent calls. A call context controls that call only.
Canceling it sends a cancellation frame without closing other calls. Close the
client when it is no longer needed.

Declared application errors arrive as generated error types. Core protocol
failures arrive as `*plug.RemoteError`. Transport and context failures retain
their usual Go error forms.

## Choose compatibility rules

By default, a client accepts an additive schema generation from the same package
and major version. New declarations and optional fields can therefore roll out
without updating every process at once.

Set `StrictSchema` in both client and server options when peers must use the
exact same schema fingerprint:

```go
options := plug.ClientOptions{StrictSchema: true}
```

`plug.Inspect` retrieves a peer's canonical schema without generated bindings.
It is useful for diagnostics and tooling.

## Expose HTTP bindings

A method with an `http` clause receives a generated route. Serve those routes
with `plughttp`:

```go
handler, err := plughttp.New(
	server,
	greeter.HTTPRoutes(server),
	plughttp.Options{Authenticate: authenticateHTTP},
)
```

HTTP requests enter the same decoding, authorization, handler, and error path as
native calls. `plughttp.NewBrowser` renders the generated schema as browsable
documentation and serves the complete reference as a Markdown download at
`index.md` below the browser's mount path. The HTML pages link to the download.

To browse a schema file, run `go tool plug doc schema.plug` and open
`http://localhost:8080`. Use `-addr` to select another address or a Unix socket.

`plugupgrade.Server` accepts native HTTP Upgrade and WebSocket on the same
authenticated endpoint. `plugupgrade.Dialer.DialContext` takes a complete URL:
`http://` or `https://` for native Upgrade, `ws://` or `wss://` for WebSocket.
Both transports use the dialer's headers, TLS settings, and proxy configuration.

Use WebSocket through proxies such as Cloudflare. Peers send pings every 30
seconds and close the connection after 10 seconds without a pong. Raw stream
users must keep reading to handle control frames.

### Generate an HTTP client

`plug generate` can generate a typed HTTP client in Go that depends only on the
standard library:

```sh
$ plug generate -http-client -o greeter_http.go schema.plug
```

It can also generate TypeScript types and an HTTP client that uses standard
`fetch`:

```sh
$ plug generate -ts -o greeter.ts schema.plug
```

Generated TypeScript errors expose schema fields through `details`. Fields that
conflict with error metadata, such as `status` or `code`, stay in `details`. A
required string `message` field also supplies the error's message.

When a method declares several HTTP bindings, generated clients use the first
binding in canonical verb-and-path order. Servers continue to expose all
declared bindings.

## Raw methods

A raw method uses a dedicated connection. Its handler receives a
`*plug.RawStream`. It may reject the request by returning an error, or call
`Accept` once and use the returned connection as a byte stream.

If the schema declares a `greeter.tunnel` raw method, its generated client
function opens the raw call:

```go
conn, err := net.Dial("unix", address)
if err != nil {
	return err
}
raw, err := greeter.OpenTunnel(ctx, conn, request, plug.ClientOptions{})
```

The generated function takes ownership of `conn`. After acceptance, framing does
not resume on that connection.

[protocol]: protocol.md
[schema]: schema.md
