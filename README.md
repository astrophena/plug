# Plug

> WARNING! WARNING! WARNING!
>
> This is unstable code for personal use, subject to change without notice and
> may gain or lose functionality at any time. Don't depend on it (or do, I don't
> care).

Plug provides schema-generated RPC for Go services.

## Documentation

- [Internals](docs/internals.md)
- [Protocol](docs/protocol.md)
- [Schema JSON format](docs/schema-json.md)
- [Schema specification](docs/schema.md)
- [Using Plug](docs/intro.md)

## Development

You need the latest version of [Go] installed.

First, clone the repository:

```sh
$ git clone https://github.com/astrophena/plug
$ cd plug
```

To run the tests, run:

```sh
$ go test ./...
```

To set up the Git pre-commit hook for development:

```sh
$ go tool pre-commit
```

## License

This code is licensed under the [ISC](LICENSE.md) license.

[go]: https://go.dev
