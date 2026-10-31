# Schema JSON format (version 1)

The JSON export is a compiled description of a Plug service. It contains the
resolved types, wire IDs, and handshake hashes needed to build clients or other
generators. Consumers need a JSON reader and an implementation of the
[Plug protocol](protocol.md); they do not need the schema parser, Go bindings,
or this repository's sources.

## Generate an export

```sh
$ go tool plug generate -json -o greeter.json schema.plug
$ go tool plug generate -json -check -o greeter.json schema.plug
```

The input is parsed and validated using the same rules as Go generation. The
output file is replaced atomically. `-check` compares the complete generated
bytes with an existing file and fails if it is missing or stale, without writing
it. `-json`, `-http-client`, and `-ts` are mutually exclusive.

The file is UTF-8 JSON, indented with two spaces and terminated by a newline.
Object keys use lowercase snake_case. Every property described below is present,
including empty strings, zero numbers, and false booleans. Collections are
always arrays, including `[]` when empty. The only conditional properties are
`name` and `element` in type expressions.

## Service object

| Property           | JSON type | Meaning                                          |
| ------------------ | --------- | ------------------------------------------------ |
| `format_version`   | number    | Export format version, currently `1`.            |
| `protocol_version` | number    | Native wire protocol version, currently `1`.     |
| `package`          | string    | Schema namespace.                                |
| `major`            | number    | Positive 16-bit schema compatibility generation. |
| `family`           | string    | Complete compatibility-family SHA-256 hash.      |
| `fingerprint`      | string    | Complete exact-schema SHA-256 hash.              |
| `documentation`    | string    | Package documentation, or `""`.                  |
| `permissions`      | array     | Permission objects.                              |
| `types`            | array     | Named type objects.                              |
| `categories`       | array     | Presentation categories.                         |
| `methods`          | array     | RPC method objects.                              |

The three versions are independent: `format_version` describes this JSON
contract, `protocol_version` describes connection framing and value encoding,
and `major` identifies the service's compatibility generation.

Hashes are 64-character lowercase hexadecimal strings. A consumer decodes them
to 32 bytes and uses their first 16 bytes in a call hello. IDs are unsigned
32-bit decimal JSON numbers. When used in protocol frames, IDs are encoded as
four bytes in little-endian order. Consumers can use the exported IDs and hashes
directly without implementing the
[schema identity algorithms](schema.md#identity).

Permissions, named types, methods, and constructors within each type are sorted
by schema name. HTTP bindings are sorted by verb, then path. Category order is
preserved, while each category's members are sorted by name. Field arrays retain
their declaration order, which is their wire encoding order.

Source paths, source positions, generated Go names, and schema source text are
absent. Names and documentation retain their schema meaning. Documentation
strings contain normalized
[Go doc comment syntax](schema.md#documentation-and-linting) without comment
markers or a trailing newline. Schema references, link definitions, and
indentation are preserved; references are not lowered to plain names as they are
in generated binding comments.

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

This represents `list<User>?`. Consumers follow the object structure without
parsing a schema expression.

| Kind          | Additional property | Meaning                                                                                |
| ------------- | ------------------- | -------------------------------------------------------------------------------------- |
| `"primitive"` | `name`              | One of `bool`, `bytes`, `f64`, `i32`, `i64`, `u32`, `u64`, `string`, `void`, or `raw`. |
| `"named"`     | `name`              | Reference to a named type in the service's `types` array.                              |
| `"list"`      | `element`           | Nested type expression for each list element.                                          |

`optional: true` adds the protocol's presence byte. Optional list elements are
invalid in Plug schemas. `void` and `raw` occur only as non-optional method
responses. All other validity rules come from the
[schema specification](schema.md).

Records, sums, newtypes, and application-error payloads keep their existing
[value encoding](protocol.md#value-encoding). In particular, the constructor ID
of an application error is carried by the frame, and its payload contains the
fields directly.

## Methods and presentation metadata

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

## Read an export independently

For the [greeter example](../example/greeter/schema.plug), this Python code uses
only the standard library and the exported file:

```python
import json
import struct

with open("greeter.json", encoding="utf-8") as f:
    schema = json.load(f)

if schema["format_version"] != 1 or schema["protocol_version"] != 1:
    raise ValueError("unsupported Plug schema export")

methods = {method["name"]: method for method in schema["methods"]}
ping = methods["greeter.ping"]

# A call hello with no optional connection flags.
hello = (
    b"PLUG"
    + bytes([schema["protocol_version"], 0])
    + bytes.fromhex(schema["family"])[:16]
    + bytes.fromhex(schema["fingerprint"])[:16]
)

# Request 1 with no fields: six bytes follow the length prefix.
request = bytes([6, 1, 1]) + struct.pack("<I", ping["id"])
print(hello.hex(), request.hex())
```

Connection handling, response framing, and value codecs follow the protocol
specification. The JSON export describes the service contract rather than
carrying RPC values.

## Format compatibility

Consumers should check `format_version` and `protocol_version` before using an
export. They may ignore unknown object properties within a supported format
version. Backward-compatible property additions retain `format_version`; changes
to existing property meaning or representation require a new format version.
Unsupported type kinds or primitive names require consumer support before they
can be used.

The fingerprint identifies the wire schema, not the exported file's bytes.
Documentation and category changes can change an export without changing its
fingerprint. Generator formatting changes can also change the bytes compared by
`-check` while preserving the JSON contract.
