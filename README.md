# Plug

Yet another RPC protocol nobody needs (*probably*).

Plug defines services in schemas and provides a Go runtime for calling and serving them.
The schema also describes HTTP routes and can be exported as JSON for other implementations.

<p align="center">
  <img alt="Achtung!" src=".github/achtung.webp" width="200"/>
</p>

> [!WARNING]
>
> This is unstable code for personal use, subject to change without notice and
> may gain or lose functionality at any time.
> Don't depend on it (or do, I don't care).

## Documentation

- [Using Plug](docs/intro.md): build and call a Go service.
- [HTTP](docs/http.md): serve JSON routes, generate HTTP clients, or carry native Plug over HTTP.
- [Purl and PlugScript](docs/purl.md): explore and call a running service.

References for schemas, tools, and other implementations:

- [Schema specification](docs/schema.md): declarations, documentation, and compatibility.
- [Wire protocol](docs/protocol.md): schema identity, handshakes, frames, and values.
- [Schema JSON export](docs/schema-json.md): resolved metadata for clients and generators.

The [Go API](https://pkg.go.dev/go.astrophena.name/plug) documents the runtime.
Use `plug -help` and `purl -help` for command usage.

## Tools

Install the generator and [Purl](https://go.astrophena.name/plug/cmd/purl):

```sh
$ go install go.astrophena.name/plug/cmd/plug@latest
$ go install go.astrophena.name/plug/cmd/purl@latest
```

*Regenerate all bindings whenever you upgrade Plug*.

## Development

Use the latest version of [Go].

Run the tests with:

```sh
$ go test ./...
```

Run the development checks and install the Git pre-commit hook with:

```sh
$ go tool pre-commit
```

## License

This code is licensed under the [ISC](LICENSE.md) license.

[go]: https://go.dev
