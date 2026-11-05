# Schema JSON export (version 1)

The JSON export describes a service's resolved types, wire IDs, and handshake
hashes. Use it to build clients or generators without parsing `.plug` files.
Calls use the [Plug protocol](protocol.md).

## Generate an export

```sh
$ go tool plug generate -json -o greeter.json schema.plug
```

Add `-check` to check an existing file without writing it.

The file is UTF-8 JSON, indented with two spaces and terminated by a newline.
Object keys use lowercase snake_case. Every property described below is present,
including empty strings, zero numbers, and false booleans. Collections are
always arrays, including `[]` when empty. The only conditional properties are
`name` and `element` in type expressions.

## Service object

| Property           | JSON type | Meaning                                      |
| ------------------ | --------- | -------------------------------------------- |
| `format_version`   | number    | Export format version, currently `1`.        |
| `protocol_version` | number    | Native wire protocol version, currently `1`. |
| `package`          | string    | Schema namespace.                            |
| `major`            | number    | Schema major version (1–65535).              |
| `family`           | string    | Complete compatibility-family SHA-256 hash.  |
| `fingerprint`      | string    | Complete exact-schema SHA-256 hash.          |
| `documentation`    | string    | Package documentation, or `""`.              |
| `permissions`      | array     | Permission objects.                          |
| `types`            | array     | Named type objects.                          |
| `categories`       | array     | Documentation groups.                        |
| `methods`          | array     | RPC method objects.                          |

Hashes are 64-character lowercase hexadecimal strings. A consumer decodes them
to 32 bytes and uses their first 16 bytes in a call hello. IDs are unsigned
32-bit numbers, encoded little-endian in frames. Use the exported IDs and hashes
directly; the [protocol](protocol.md#schema-identity) describes their
calculation.

Permissions, named types, methods, and constructors within each type are sorted
by schema name. HTTP bindings are sorted by verb, then path. Category order is
preserved, while each category's members are sorted by name. Field arrays retain
their declaration order, which is their wire encoding order.

The export omits source paths, positions, Go names, and schema source text.
Documentation strings contain normalized
[Go doc comment syntax](schema.md#documentation-and-linting) without comment
markers or a trailing newline. Schema references, link definitions, and
indentation are preserved; references are not replaced with plain names as in
generated binding comments.

## Named types and fields

A named type object contains:

| Property        | JSON type | Meaning                                                        |
| --------------- | --------- | -------------------------------------------------------------- |
| `name`          | string    | Schema type name.                                              |
| `documentation` | string    | Type documentation, or `""`.                                   |
| `kind`          | string    | `"newtype"`, `"record"`, or `"sum"`.                           |
| `underlying`    | string    | Primitive scalar for a newtype; otherwise `""`.                |
| `error`         | boolean   | Whether this type is dedicated to declared application errors. |
| `constructors`  | array     | Constructor objects; empty for a newtype.                      |

A record has one constructor; a sum has two or more. Each constructor contains
`name`, `documentation`, numeric `id`, a `fields` array, and numeric
`http_status`. Nesting identifies its result type. `http_status` is `0` when no
status was declared; an HTTP adapter uses status 400 for such an application
error. This annotation does not affect native value encoding.

Each field contains `name`, `documentation`, and a structured `type` expression.
Fields remain in wire order, including the optional extension tail.

### Type expressions

A type expression has `kind` and `optional`. A leaf has `name` and no `element`;
a list has `element` and no `name`:

```json
{
  "kind": "list",
  "element": {
    "kind": "named",
    "name": "User",
    "optional": false
  },
  "optional": true
}
```

This represents `list<User>?`.

| Kind          | Additional property | Meaning                                                                                |
| ------------- | ------------------- | -------------------------------------------------------------------------------------- |
| `"primitive"` | `name`              | One of `bool`, `bytes`, `f64`, `i32`, `i64`, `u32`, `u64`, `string`, `void`, or `raw`. |
| `"named"`     | `name`              | Reference to a named type in the service's `types` array.                              |
| `"list"`      | `element`           | Nested type expression for each list element.                                          |

`optional: true` adds the protocol's presence byte. Optional list elements are
invalid in Plug schemas. `void` and `raw` occur only as non-optional method
responses. All other validity rules come from the
[schema specification](schema.md).

## Methods and categories

A method object contains:

| Property        | JSON type | Meaning                                                   |
| --------------- | --------- | --------------------------------------------------------- |
| `name`          | string    | Qualified schema method name.                             |
| `documentation` | string    | Method documentation, or `""`.                            |
| `id`            | number    | Assigned native method ID.                                |
| `fields`        | array     | Request fields in wire order.                             |
| `response`      | object    | Successful response type expression.                      |
| `error`         | string    | Declared application-error type name, or `""`.            |
| `public`        | boolean   | Whether the method permits anonymous callers.             |
| `permission`    | string    | Required permission name, or `""` for a public method.    |
| `http`          | array     | HTTP binding objects containing string `verb` and `path`. |

A permission object contains string `name` and `documentation`.

A category object contains string `name` and `documentation`, plus `types` and
`methods` arrays of schema names. Declarations outside any category remain in
the service's type and method arrays without category membership. Categories and
documentation do not participate in wire identity.

## Format compatibility

Check `format_version` and `protocol_version` before using an export. Unknown
properties may be ignored in a supported version. Adding properties preserves
`format_version`; changing existing meanings or representations requires a new
version. Consumers must support any type kinds or primitives they use.

Documentation, categories, and generator formatting can change the exported
bytes without changing the fingerprint. `-check` reports the file as stale
whenever its contents differ from the generated export, including these changes.
