# HTTP

Plug supports JSON routes and native Plug over HTTP Upgrade or WebSocket.
They use different handlers and clients:

| Interface   | Server                  | Client                                                  |
| ----------- | ----------------------- | ------------------------------------------------------- |
| JSON routes | `plughttp.New`          | Generated Go or TypeScript HTTP client, `curl`, `fetch` |
| Native Plug | `plugupgrade.NewServer` | Native Go client, Purl                                  |

Both adapters call the handlers registered with the Plug server.

## JSON routes

Add an HTTP binding to the [greeting service](intro.md):

```plug
greeting.greet
  name: string
-> string
  public
  http GET "/greet/{name}"
```

Regenerate the native bindings with `go generate ./greeting`.
In `cmd/server/main.go`, add imports for `net/http` and `go.astrophena.name/plug/plughttp`.
After registering the handlers, replace the listener and `Serve` block with:

```go
api, err := plughttp.New(srv, greeting.HTTPRoutes(srv), plughttp.Options{})
if err != nil {
	log.Fatal(err)
}
httpSrv := &http.Server{
	Addr:        "127.0.0.1:8080",
	Handler:     api,
	BaseContext: func(net.Listener) context.Context { return srv.Context() },
}
if err := httpSrv.ListenAndServe(); err != nil {
	log.Fatal(err)
}
```

Update module dependencies and restart the server:

```sh
$ go mod tidy
$ go run ./cmd/server
```

It now serves HTTP on port 8080:

```sh
$ curl http://127.0.0.1:8080/greet/Ada
"Hello, Ada!"
```

`BaseContext` connects HTTP request contexts to Plug shutdown.
HTTP also cancels a request's context when the client disconnects or the request finishes.
Shut down the HTTP server and Plug server separately;
see [cancellation and shutdown](intro.md#cancellation-and-shutdown).

Set `plughttp.Options.Authenticate` to identify callers from HTTP requests,
or supply a principal through `plug.WithPrincipal` in middleware.
The Plug server's authorizer checks restricted methods.
A public method skips authorization, but a configured HTTP authenticator still runs.

### Generated clients

Generate a standalone Go HTTP client in a separate directory:

```sh
$ mkdir -p greetinghttp cmd/httpclient
$ go tool plug generate -http-client -o greetinghttp/client.go greeting/schema.plug
```

It defines its own types and `Client`;
do not put it in the same package as the native bindings.
Its Go package name is still `greeting`, so use an import alias.
Save this as `cmd/httpclient/main.go`:

```go
package main

import (
	"context"
	"fmt"
	"log"

	greetinghttp "example.com/greeting/greetinghttp"
)

func main() {
	c := greetinghttp.NewClient("http://127.0.0.1:8080")
	msg, err := c.Greet(context.Background(), greetinghttp.GreetRequest{Name: "Ada"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(msg)
}
```

Run it with `go run ./cmd/httpclient`.
The generated client uses only the Go standard library.
`WithHTTPClient` and `WithHeader` configure its transport and default headers.

For TypeScript, generate `greeting.ts`:

```sh
$ go tool plug generate -ts -o greeting.ts greeting/schema.plug
```

The client uses `fetch`:

```ts
import { Client } from "./greeting.ts";

const c = new Client("http://127.0.0.1:8080");
console.log(await c.greet({ name: "Ada" }));
```

TypeScript errors keep schema fields in `details`;
a required string `message` field also supplies the error message.

### Route patterns

Paths follow `net/http.ServeMux` rules.
They start with `/` and contain no query or fragment.
Each `{name}` placeholder names a scalar or newtype request field
and occupies a complete path segment.
Placeholders must be unique.
Duplicate or ambiguous routes are rejected;
overlapping routes need a single most-specific match.

A final `{name...}` captures the rest of the path, including slashes.
It requires a string or string-newtype field.
An optional wildcard lets generated clients address the subtree root without supplying a path value.
Clients escape each wildcard segment separately;
ordinary `{name}` parameters remain single escaped segments.

Servers expose every declared binding.
Generated HTTP clients use the first binding sorted by verb, then path.
Raw methods cannot have JSON routes.

### Requests

For `GET` and `DELETE`, fields come from the path and query.
Fields must be scalars, scalar newtypes, or lists of those types.
Repeat query parameters for lists and omit absent optional values.
Scalar parameters take one value;
bytes use standard padded base64, integers use decimal, and booleans accept `true` or `false`.

Other verbs accept a JSON object.
An empty body supplies no fields.
Query values override body fields, and path values override both.
Duplicate JSON members, trailing JSON values, missing required fields,
unknown fields, and unknown sum constructors are rejected.

The Go handler's `MaxBodyBytes` option limits JSON bodies.
Zero uses the Plug server's maximum frame size.
See [Options](https://pkg.go.dev/go.astrophena.name/plug/plughttp#Options).

### JSON values

Records use schema field names as keys.
Sums add `type` with the constructor name:

```json
{ "id": 42, "name": "Ada" }
```

```json
{ "type": "admin", "level": 9 }
```

```json
{ "type": "guest" }
```

| Schema value           | JSON                                                       |
| ---------------------- | ---------------------------------------------------------- |
| `bool`, `string`       | Boolean, string                                            |
| Integers, `f64`        | Number                                                     |
| Newtype                | Underlying scalar                                          |
| `bytes`                | Base64 string; empty or nil bytes use `""`                 |
| `list<T>`              | Array; empty or nil lists use `[]`, including nested lists |
| Absent optional field  | Omitted; omission or `null` on input means absence         |
| Present optional value | Contained value, including `0`, `false`, `""`, and `[]`    |

Use `encoding/json/v2` with generated Go values.
Go preserves the full integer range.
TypeScript uses `number`, which cannot represent every `i64` or `u64` exactly.

Go HTTP clients retain unfamiliar response constructors as complete JSON objects.
Native Go bindings represent unknown wire constructors in JSON as diagnostic objects,
such as `{"type":"unknown","constructorID":123,"payload":"AA=="}`.

### Responses and errors

| Result                | Status              | Body                         |
| --------------------- | ------------------- | ---------------------------- |
| Value                 | 200 by default      | JSON value                   |
| Absent optional value | 200                 | `null`                       |
| `void`                | 204                 | Empty                        |
| Declared error        | `httpStatus` or 400 | `code`, `message`, `details` |

In Go, a non-void response with an `HTTPStatus() int` method can select a status from 200 through 599.
Other statuses are rejected.

Declared errors put their constructor name in `code` and schema fields in `details`:

```json
{ "code": "notFound", "message": "notFound: id=42", "details": { "id": 42 } }
```

Core errors omit `details`:

| Code                | Status |
| ------------------- | ------ |
| `bad_request`       | 400    |
| `unauthenticated`   | 401    |
| `permission_denied` | 403    |
| `internal`          | 500    |
| `unimplemented`     | 501    |

## Native Plug over HTTP

`plugupgrade.NewServer` accepts HTTP Upgrade and WebSocket on the same endpoint.
To add `/plug` to the HTTP server above,
import `go.astrophena.name/plug/plugupgrade` and insert this before `ListenAndServe`:

```go
upgrade, err := plugupgrade.NewServer(srv, plugupgrade.Options{})
if err != nil {
	log.Fatal(err)
}
mux := http.NewServeMux()
mux.Handle("/plug", upgrade)
mux.Handle("/", api)
httpSrv.Handler = mux
```

Identify HTTP callers through `plugupgrade.Options.Authenticate`
or the request context.
The Plug server's authorizer checks restricted methods.
Its native connection authenticator also runs if configured.

Run `go mod tidy` to record the adapter's dependencies, then restart the server.
Purl can now use either transport:

```sh
$ purl http://127.0.0.1:8080/plug
$ purl ws://127.0.0.1:8080/plug
```

For Go, pass a `plugupgrade.Dialer` to `plug.DialFunc`:

```go
d := new(plugupgrade.Dialer)
c, err := plug.DialFunc(ctx, func(ctx context.Context) (net.Conn, error) {
	return d.DialContext(ctx, "http://127.0.0.1:8080/plug")
}, &greeting.Schema, plug.ClientOptions{})
```

`http` and `https` select native HTTP/1.1 Upgrade;
`ws` and `wss` select WebSocket.
The dialer supports headers, TLS settings, and proxy configuration.
A JSON route such as `/greet/Ada` does not accept native Plug connections.

The Go WebSocket adapter pings every 30 seconds and allows 10 seconds for a pong.
Raw stream users must keep reading to process control frames.
See the [protocol](protocol.md#websocket-transport) for the byte-stream mapping.

`http.Server.Shutdown` does not manage hijacked connections.
Call `plugupgrade.Server.Shutdown` as well;
it shuts down the underlying Plug server and closes upgraded connections.

## Schema browser

`plughttp.NewBrowser` serves HTML reference pages and an `index.md` download.
Its [Go example](https://pkg.go.dev/go.astrophena.name/plug/plughttp#example-NewBrowser)
shows mounting it with `web.Server` and `plughttp.StaticFS()`.

To browse a schema file directly:

```sh
$ go tool plug doc greeting/schema.plug
```

Open `http://localhost:8080`.
Restart the command after schema changes.
