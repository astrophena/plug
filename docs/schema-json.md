# Schema JSON export (version 1)

The export describes resolved types, wire IDs, handshake hashes, and canonical source.
Use it to build clients or generators without parsing `.plug` files
or reproducing the identity algorithms.
Calls use the [Plug protocol](protocol.md);
schema validity follows the [schema specification](schema.md).

## Generate an export

From the [greeting tutorial](intro.md):

```sh
$ go tool plug generate -json -o greeting.json greeting/schema.plug
```

Add `-check` to report a missing or stale file without writing it.

The output is UTF-8 JSON, indented with two spaces and ending in a newline.
Object keys use lowercase snake_case.
All described properties are present, including empty strings, zero numbers, and false booleans.
Collections are arrays, using `[]` when empty.
Only `name` and `element` in type expressions are conditional.

## Service object

| Property           | JSON type | Meaning                                    |
| ------------------ | --------- | ------------------------------------------ |
| `format_version`   | number    | Export format version: `1`                 |
| `protocol_version` | number    | Native wire protocol version: `1`          |
| `package`          | string    | Schema namespace                           |
| `major`            | number    | Schema version, from 1 through 65535       |
| `family`           | string    | Complete compatibility-family SHA-256 hash |
| `fingerprint`      | string    | Complete exact-schema SHA-256 hash         |
| `source`           | string    | Canonical schema text                      |
| `documentation`    | string    | Package documentation, or `""`             |
| `permissions`      | array     | Permission objects                         |
| `types`            | array     | Named type objects                         |
| `categories`       | array     | Documentation groups                       |
| `methods`          | array     | Method objects                             |

Hashes are 64-character lowercase hexadecimal strings.
Decode them to 32 bytes and use the first 16 bytes in a call hello.
IDs are unsigned 32-bit JSON numbers;
encode them little-endian in method and constructor ID fields.
Use the exported values directly;
the [identity rules](protocol.md#schema-identity) explain their calculation.

Permissions, types, methods, and each type's constructors are sorted by schema name.
HTTP bindings are sorted by verb, then path.
Categories retain source order;
each category's type and method names are sorted.
Field arrays retain declaration order, which is their wire order.

The export omits source paths, positions, and Go names.
`source` is the same canonical text returned by inspection and rejected handshakes.
Documentation strings use normalized [Go doc syntax](schema.md#documentation-and-linting),
without comment markers or a trailing newline.
References, URL definitions, and indentation are preserved.

## Named types and fields

| Property        | JSON type | Meaning                                             |
| --------------- | --------- | --------------------------------------------------- |
| `name`          | string    | Schema type name                                    |
| `documentation` | string    | Type documentation, or `""`                         |
| `kind`          | string    | `"newtype"`, `"record"`, or `"sum"`                 |
| `underlying`    | string    | Scalar primitive for a newtype; otherwise `""`      |
| `error`         | boolean   | Whether the type is dedicated to application errors |
| `constructors`  | array     | Constructor objects; `[]` for a newtype             |

A record has one constructor; a sum has two or more.
Each constructor contains `name`, `documentation`, numeric `id`,
a `fields` array, and numeric `http_status`.
Its containing type identifies the result type.
`http_status` is `0` when undeclared;
[HTTP adapters](http.md#responses-and-errors) then use 400 for an application error.

Each field contains `name`, `documentation`, and a structured `type` expression.
Required fields precede optional fields.

### Type expressions

Every expression has `kind` and `optional`.
A leaf has `name` and no `element`;
a list has `element` and no `name`.
For example, `list<User>?` is:

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

| Kind          | Additional property | Meaning                                                                        |
| ------------- | ------------------- | ------------------------------------------------------------------------------ |
| `"primitive"` | `name`              | `bool`, `bytes`, `f64`, `i32`, `i64`, `u32`, `u64`, `string`, `void`, or `raw` |
| `"named"`     | `name`              | Reference to a name in `types`                                                 |
| `"list"`      | `element`           | Nested expression for each list element                                        |

`optional: true` uses the protocol's presence-byte encoding.
Absent trailing optional fields may be omitted entirely;
see [value encoding](protocol.md#value-encoding).
List elements cannot be optional.
`void` and `raw` occur only as non-optional method responses.

## Methods and categories

A method object contains:

| Property        | JSON type | Meaning                                           |
| --------------- | --------- | ------------------------------------------------- |
| `name`          | string    | Qualified schema method name                      |
| `documentation` | string    | Method documentation, or `""`                     |
| `id`            | number    | Native method ID                                  |
| `fields`        | array     | Request fields in wire order                      |
| `response`      | object    | Successful response type expression               |
| `error`         | string    | Declared application-error type name, or `""`     |
| `public`        | boolean   | Whether the method skips permission authorization |
| `permission`    | string    | Required permission, or `""` for a public method  |
| `http`          | array     | Objects with string `verb` and `path`             |

`public` does not bypass transport authentication.
A public method can still require an authenticated connection under the server's transport policy.

A permission object contains string `name` and `documentation`.

A category contains string `name` and `documentation`,
plus `types` and `methods` arrays of schema names.
Uncategorized declarations remain in the service arrays without category membership.
Categories and documentation do not affect wire identity.

## Format compatibility

Check `format_version` and `protocol_version` before using an export.
Unknown properties may be ignored in a supported version.
Adding properties preserves `format_version`;
changing existing meanings or representations requires a new version.
Consumers must support the type kinds and primitives they use.

Documentation, categories, and formatting can change exported bytes without changing the fingerprint.
`-check` reports any difference from the generated export, including these changes.
