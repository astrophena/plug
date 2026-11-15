# Using Plug

This guide builds a service that takes a name and returns a greeting.
Plug generates its Go types, handler registrations, and client from a schema.

## Set up

In a new directory, create a module and add the generator:

```sh
$ go mod init example.com/greeting
$ go get -tool go.astrophena.name/plug/cmd/plug
$ mkdir -p greeting cmd/server cmd/client
```

## Define and generate the service

Save this as `greeting/schema.plug`:

```plug
// greeting greets callers by name.
package greeting

// greeting.greet returns a greeting.
greeting.greet
  // name is the person to greet.
  name: string
-> string public
```

The method takes a string named `name` and returns a string.
`public` means it requires no method permission.

Save this as `greeting/generate.go`:

```go
package greeting

//go:generate go tool plug generate -o greeting_plug.go schema.plug
```

Generate the bindings from the module root:

```sh
$ go generate ./greeting
```

The generated file contains `Schema`, `Handlers`, and `Client`.
Commit it with the schema.
Regenerate it whenever the schema or Plug version changes.

## Run the server

Create a server, register the generated handlers, and serve a listener.
Save this as `cmd/server/main.go`:

```go
package main

import (
	"context"
	"log"
	"net"

	"example.com/greeting/greeting"
	"go.astrophena.name/plug"
)

func main() {
	srv, err := plug.NewServer(&greeting.Schema, plug.ServerOptions{})
	if err != nil {
		log.Fatal(err)
	}
	if err := greeting.RegisterHandlers(srv, greeting.Handlers{
		Greet: func(_ context.Context, req greeting.GreetRequest) (string, error) {
			return "Hello, " + req.Name + "!", nil
		},
	}); err != nil {
		log.Fatal(err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:9000")
	if err != nil {
		log.Fatal(err)
	}
	if err := srv.Serve(context.Background(), l); err != nil {
		log.Fatal(err)
	}
}
```

`Serve` takes ownership of the listener and accepted connections.
Register all handlers before serving;
the first serve or local invocation seals registration.

Start the server from the module root:

```sh
$ go run ./cmd/server
```

## Call it

`plug.Dial` connects and performs the schema handshake.
Wrap the returned client with `greeting.NewClient` to call the generated methods.

Save this as `cmd/client/main.go`:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"example.com/greeting/greeting"
	"go.astrophena.name/plug"
)

func main() {
	ctx := context.Background()
	c, err := plug.Dial(ctx, "tcp", "127.0.0.1:9000", &greeting.Schema, plug.ClientOptions{})
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	msg, err := greeting.NewClient(c).Greet(ctx, greeting.GreetRequest{Name: "Ada"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(msg)
}
```

Run the client in another terminal:

```sh
$ go run ./cmd/client
Hello, Ada!
```

Stop the server with Ctrl-C.

## Cancellation and shutdown

Clients can make concurrent calls over the same connection.
Import `time` and give a call a deadline by deriving its context:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

msg, err := greeting.NewClient(c).Greet(ctx, greeting.GreetRequest{Name: "Ada"})
```

Canceling the context stops waiting and requests cancellation on the server.
Handlers should observe their context;
a canceled handler occupies its concurrency slot until it returns.

Pass a cancelable context to `Serve` to control the server's lifetime.
Canceling it closes the listener and its connections.
Call `Server.Shutdown` with a fresh, bounded context to reject new calls,
cancel active binary calls, and wait for handlers to return.
`Shutdown` does not close the listener itself.

HTTP servers and Upgrade adapters have their own shutdown methods.
For JSON routes, derive HTTP request contexts from `Server.Context()`
so Plug shutdown also cancels their handlers.
The [HTTP guide](http.md) shows the wiring.

## Authentication and permissions

Authentication identifies a caller; authorization checks whether it may call a method.
For native connections, `ServerOptions.Security.Authenticate` derives a `Principal`
from the connection before the handshake, including inspection.
A configured authenticator may reject a connection even when every method is public.

To restrict the greeting, declare `permission greeting.use`,
replace `public` with `requires greeting.use`, and regenerate the bindings.
Configure `Security.Authenticate` to identify callers
and `Security.Authorize` to check that permission against the principal and request.
A restricted method requires a principal and an authorizer.
`public` skips this method-level check.

HTTP adapters can identify callers from the HTTP request or its context.
They use the same Plug authorizer for restricted methods.
See the [Go example](https://pkg.go.dev/go.astrophena.name/plug#example-NewServer)
and [HTTP authentication](http.md#json-routes) for the APIs.

## Next steps

- Use [Purl](purl.md) to call a service without generated client code.
- Add [HTTP routes or an Upgrade endpoint](http.md).
- Read the [schema specification](schema.md) for types, declared errors, and compatible updates.
- See the [Go API](https://pkg.go.dev/go.astrophena.name/plug) for limits, raw streams, and local invocation.
