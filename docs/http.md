# HTTP

Plug can serve JSON routes or carry its native protocol over HTTP Upgrade and
WebSocket. These use separate handlers and clients:

| Interface   | Server                  | Client                                                  |
| ----------- | ----------------------- | ------------------------------------------------------- |
| JSON routes | `plughttp.New`          | Generated Go or TypeScript HTTP client, `curl`, `fetch` |
| Native Plug | `plugupgrade.NewServer` | `plugupgrade.Dialer`, Purl                              |

## JSON routes

Add an `http` clause to a method. For the [greeting service](intro.md):

```plug
greeting.greet name: string -> string public
  http GET "/greet/{name}"
```

Regenerate the bindings. After registering the handlers, pass the generated
routes to `plughttp.New`:

```go
func serveHTTP(server *plug.Server) error {
	handler, err := plughttp.New(server, greeting.HTTPRoutes(server), plughttp.Options{})
	if err != nil {
		return err
	}
	return http.ListenAndServe("127.0.0.1:8080", handler)
}
```

The route calls the same handler as a native request:

```sh
$ curl http://127.0.0.1:8080/greet/Ada
"Hello, Ada!"
```

Use `plughttp.Options.Authenticate` to supply a principal from the HTTP request,
or set it in the request context. The Plug server's authorizer checks private
method calls before the handler runs.

### Route patterns

Paths follow `net/http.ServeMux` rules. They must start with `/` and cannot
contain a query or fragment. Each `{name}` placeholder must name a scalar or
newtype request field and occupy a complete path segment. Placeholders must be
unique. Duplicate or ambiguous routes are rejected; overlapping routes need a
single most-specific match.

A final `{name...}` captures the rest of the path, including slashes. It
requires a string or string-newtype field. An optional field permits an empty
capture at the subtree root. Generated clients escape each wildcard segment
separately; ordinary `{name}` parameters remain single escaped segments.

Servers expose every declared binding. Generated HTTP clients choose the first
binding sorted by verb and path. Raw methods cannot have JSON routes.

### Requests

For `GET` and `DELETE`, fields come from the path and query. All fields must be
scalars, scalar newtypes, or lists of those types. Repeat query parameters for
lists, use base64 for bytes, and omit absent optional values.

Other verbs accept a JSON object. Query values override body fields, and path
values override both. Duplicate JSON members, trailing JSON values, missing
required fields, unknown fields, and unknown sum constructors are rejected.

`plughttp.Options.MaxBodyBytes` limits the JSON body. See the
[handler options](https://pkg.go.dev/go.astrophena.name/plug/plughttp#Options)
for defaults.

### JSON values

Use `encoding/json/v2` with generated Go values. Records use schema field names
as keys; sums add `type` with the constructor name:

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

Go keeps the full integer range. TypeScript uses `number`, which cannot
represent every `i64` or `u64` exactly.

Go HTTP clients preserve unfamiliar response constructors as complete JSON
objects. Native Go bindings display unknown wire constructors as diagnostic
objects such as `{"type":"unknown","constructorID":123,"payload":"AA=="}`.

### Responses and errors

| Result                | Status              | Body                         |
| --------------------- | ------------------- | ---------------------------- |
| Value                 | 200 by default      | JSON value                   |
| Absent optional value | 200                 | `null`                       |
| `void`                | 204                 | Empty                        |
| Declared error        | `httpStatus` or 400 | `code`, `message`, `details` |

A response type with an `HTTPStatus() int` method can select a status from 200
through 599. The handler rejects other statuses. Declared errors put their
schema fields in `details`:

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

### Generated clients

```sh
$ go tool plug generate -http-client -o greeting_http.go greeting/schema.plug
$ go tool plug generate -ts -o greeting.ts greeting/schema.plug
```

The Go client uses only the standard library. The TypeScript client uses
`fetch`. TypeScript errors keep schema fields in `details`; a required string
`message` field also supplies the error message.

## Native Plug over HTTP

`plugupgrade.NewServer` accepts HTTP Upgrade and WebSocket connections on one
endpoint. Authenticate requests through `plugupgrade.Options.Authenticate` or
supply a principal in the request context. The Plug server's authorizer still
checks private method calls.

`plugupgrade.Dialer.DialContext` takes a full URL: `http` or `https` for native
Upgrade, `ws` or `wss` for WebSocket. Both use the dialer's headers, TLS
settings, and proxy configuration. The returned connection can be passed to
`plug.NewClient`.

Purl connects directly to an Upgrade endpoint:

```sh
$ purl https://api.example.com/plug
$ purl wss://api.example.com/plug
```

A JSON route such as `/greet/Ada` does not accept native Plug connections.
WebSocket works through proxies such as Cloudflare. Peers ping every 30 seconds
and close after 10 seconds without a pong. Raw stream users must keep reading to
handle WebSocket control frames. See the [wire protocol](protocol.md) for
framing.

## Schema browser

`plughttp.NewBrowser` serves HTML reference pages and an `index.md` download.
See its
[Go example](https://pkg.go.dev/go.astrophena.name/plug/plughttp#example-NewBrowser)
for mounting it with static assets. To browse a schema file directly:

```sh
$ go tool plug doc greeting/schema.plug
```

Open `http://localhost:8080`. Restart the command after schema changes.
