# Plug

Yet another RPC protocol nobody needs (probably).

<p align="center">
  <img alt="Achtung!" src=".github/achtung.webp" width="200"/>
</p>

> [!WARNING]
>
> This is unstable code for personal use, subject to change without notice and
> may gain or lose functionality at any time. Don't depend on it (or do, I don't
> care).

## Documentation

Start with [Using Plug](docs/intro.md) to build a service, or
[Purl and PlugScript](docs/purl.md) to call an existing one.

- [Schema reference](docs/schema.md): types, methods, permissions,
  compatibility.
- [HTTP](docs/http.md): JSON routes, generated clients, Upgrade and WebSocket.
- [Wire protocol](docs/protocol.md): handshakes, frames, value encoding.
- [Schema JSON export](docs/schema-json.md): resolved metadata for other tools.
- [Internals](docs/internals.md): notes for changing the runtime and generator.

## Tools

Install the standalone commands with Go:

```sh
$ go install go.astrophena.name/plug/cmd/plug@latest
$ go install go.astrophena.name/plug/cmd/purl@latest
```

The [service guide](docs/intro.md) shows how to add the generator to a Go
module. Run `plug -help` or `purl -help` for command help.

## Development

Use the latest version of Go

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
