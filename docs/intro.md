# Using Plug

The greeting service below takes a name and returns a message. Its schema
describes the method; Plug generates the Go types and client from that schema.

## Set up

In a new directory, create a module and add the generator:

```sh
$ go mod init example.com/greeting
$ go get -tool go.astrophena.name/plug/cmd/plug
```

Create directories for the bindings, server, and client:

```sh
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

The method takes a string named `name` and returns a string. `public` lets
clients call it without a Plug permission.

Save this as `greeting/generate.go`:

```go
package greeting

//go:generate go tool plug generate -o greeting_plug.go schema.plug
```

Generate the Go bindings from the module root:

```sh
$ go generate ./greeting
```

The generator writes `greeting_plug.go` in package `greeting`. It contains the
service's `Schema` value, handler definitions, and client methods. Commit it
with the schema.

## Run the server

`plug.NewServer` creates a server from the generated `Schema` value. The
generated `Handlers` struct has a function field for each method. Set `Greet`
to a function that accepts `GreetRequest` and returns a string, then register
the handlers before serving connections.

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
	server, err := plug.NewServer(&greeting.Schema, plug.ServerOptions{})
	if err != nil {
		log.Fatal(err)
	}
	if err := greeting.RegisterHandlers(server, greeting.Handlers{
		Greet: func(_ context.Context, req greeting.GreetRequest) (string, error) {
			return "Hello, " + req.Name + "!", nil
		},
	}); err != nil {
		log.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:9000")
	if err != nil {
		log.Fatal(err)
	}
	if err := server.Serve(context.Background(), listener); err != nil {
		log.Fatal(err)
	}
}
```

`Serve` accepts connections from the listener and dispatches calls to the
registered handlers. Start the server from the module root:

```sh
$ go run ./cmd/server
```

## Call it

`plug.Dial` connects to the server and performs the schema handshake. It returns
a `*plug.Client`, which handles the connection. Wrap it with `greeting.NewClient`
to get the generated `Greet` method.

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
	conn, err := plug.Dial(ctx, "tcp", "127.0.0.1:9000", &greeting.Schema, plug.ClientOptions{})
	if err != nil {
		log.Fatal(err)
	}
	client := greeting.NewClient(conn)
	defer client.Close()

	message, err := client.Greet(ctx, greeting.GreetRequest{Name: "Ada"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(message)
}
```

Run the client in another terminal:

```sh
$ go run ./cmd/client
Hello, Ada!
```

Stop the server with Ctrl-C.

## Next steps

- Use [Purl](purl.md) to call a service without generated client code.
- Read the [schema reference](schema.md) for types, errors, permissions, and
  compatible updates.
- Add [HTTP routes or an Upgrade endpoint](http.md).
- See the [Go API](https://pkg.go.dev/go.astrophena.name/plug) for
  authentication, resource limits, cancellation, and shutdown.
