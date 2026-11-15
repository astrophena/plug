# Plug protocol (version 1)

Plug runs over a reliable, ordered byte stream, such as a Unix or TCP socket.
The [schema specification](schema.md) defines service declarations.
The [JSON export](schema-json.md) supplies resolved IDs and hashes
for clients and tools that do not parse schemas.

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHOULD**, **SHOULD NOT**,
and **MAY** have the meanings defined by [RFC 2119].

Encryption, credentials, retries, discovery, and connection pooling are outside the protocol.
Version 1 has no framed streaming or server push.
[HTTP bindings](http.md) expose a separate JSON interface.

## Notation

| Type       | Encoding                                                 |
| ---------- | -------------------------------------------------------- |
| `u8`       | One unsigned byte                                        |
| `u32le`    | Four-byte unsigned integer, least-significant byte first |
| `uvarint`  | Unsigned base-128 integer, up to 64 bits                 |
| `varint`   | Signed integer mapped through ZigZag to `uvarint`        |
| `bytes[n]` | Exactly `n` bytes                                        |

Lengths count bytes, not characters.
Fields have no padding and immediately follow the preceding field.

### Variable-length integers

A `uvarint` stores seven value bits per byte, least-significant group first.
Bit 7 is set when another byte follows.
Encoders use the shortest representation:
`0` is `00`, `127` is `7f`, and `128` is `80 01`.
A decoder MUST reject truncation and overflow:
there are at most ten bytes, and the tenth byte, if present, is `00` or `01`.

ZigZag maps a nonnegative signed integer `n` to `2*n`
and a negative integer to `-2*n-1`.
Thus `0`, `-1`, and `1` encode as `00`, `01`, and `02`.
Implementations must handle these calculations without overflowing their signed integer type.

Request IDs use the full unsigned 64-bit range.
String and byte lengths, list counts, and record or constructor body lengths are unsigned 32-bit values,
encoded as `uvarint`.
A receiver's resource limits may impose smaller bounds.

## Schema identity

All identity text is encoded as UTF-8.
Spaces shown below are single ASCII spaces.
Type expressions use their schema spelling, without internal whitespace:
`User`, `list<User>`, `string?`, or `list<list<u32>>?`.
Named types retain their schema names; they are not expanded to their underlying types.

### Compatibility family

The family is the complete SHA-256 hash of:

```text
package name || 00 || decimal version
```

Here `00` is one NUL byte, not two text digits.
The version has no leading zeros.
No newline or terminator follows it.

### Constructor and method IDs

IDs use IEEE CRC-32:
polynomial `0x04c11db7`, reflected polynomial `0xedb88320`,
initial value `0xffffffff`, and final XOR `0xffffffff`.

Start a constructor's text with `PACKAGE.CONSTRUCTOR`.
Append ` name:TYPE` for each required field, in declaration order,
then append ` = RESULT`.

Start a method's text with its qualified name, `GROUP.METHOD`.
Append the required fields in the same way, then ` -> RESPONSE`.
If it declares an error, append ` throws ERROR`.
Names in capitals are placeholders for schema names and types.
When there are no required fields, append no field text.
For example:

```text
directory.user id:UserID name:string = User
directory.getUser id:UserID -> User throws LookupError
greeting.greet name:string -> string
```

ID text has no trailing newline.
Only constructor names gain the package prefix;
a method keeps its declared qualified name.
Optional fields, access rules, HTTP bindings, and HTTP statuses do not participate.
A schema is invalid if any two constructor or method IDs collide.

### Exact fingerprint

Build these normalized declaration lines:

| Declaration | Identity text                                                                      |
| ----------- | ---------------------------------------------------------------------------------- |
| Package     | `package PACKAGE version VERSION`                                                  |
| Permission  | `permission GROUP.NAME`                                                            |
| Newtype     | `type PACKAGE.NAME = SCALAR`                                                       |
| Constructor | Constructor ID text, with all fields and any nonzero ` httpStatus STATUS` appended |
| Method      | Method ID text, with all fields, an access clause, and HTTP clauses                |

For methods, append ` public` or ` requires GROUP.PERMISSION` after any `throws` clause.
An omitted access clause is normalized to ` public`.
Then append each ` http VERB "PATH"` clause, sorted by verb and then unquoted path.
Use [quoted paths](#quoted-paths) for the path text.
Version and status numbers use decimal without leading zeros.

Keep the package line first.
Sort every other line together in UTF-8 byte order,
join the lines with one LF byte (`0a`), and hash them with SHA-256.
There is no final newline.
Documentation, categories, and standalone `type Name` documentation declarations contribute no lines.

This input differs from [canonical source](schema.md#canonical-form).
Hashing the formatted schema file produces the wrong fingerprint.

### Quoted paths

Canonical path strings match Go's `strconv.Quote`.
Wrap the decoded path in double quotes and process it in order:

- Escape `"` and `\` as `\"` and `\\`.
- Use `\a`, `\b`, `\f`, `\n`, `\r`, `\t`, and `\v` for those control characters.
- Use `\xhh` for other ASCII controls, DEL, and each invalid UTF-8 byte.
- Emit printable Unicode characters directly.
  Printable means categories L, M, N, P, S, plus ASCII space.
- Escape other code points as `\uhhhh` up to U+FFFF or `\Uhhhhhhhh` above it.

Hexadecimal digits in escapes are lowercase.
Do not normalize Unicode or replace printable characters with alternative escapes.

Native handshakes use the first 16 bytes of each hash.
Inspection and JSON exports carry all 32 bytes.

## Connection handshake

Every connection starts with a client hello and a server hello.

### Client hello

A client making calls MUST send a 38-byte call hello:

| Offset | Size | Field                                   |
| -----: | ---: | --------------------------------------- |
| 0      | 4    | ASCII `PLUG`                            |
| 4      | 1    | Wire version, currently `1`             |
| 5      | 1    | Flags                                   |
| 6      | 16   | Compatibility-family SHA-256 prefix     |
| 22     | 16   | Exact-schema SHA-256 fingerprint prefix |

Flag bit 0 requests inspection.
Bit 1 requests an exact schema match.
Bit 2 requests Zstandard frame compression.
All other bits MUST be zero.

An inspection hello is only six bytes long.
It MUST set bit 0 and MUST NOT set bits 1 or 2.
A call hello MUST NOT set bit 0.

### Server hello

The server first sends a status byte:

| Status | Name                     | Meaning                             |
| -----: | ------------------------ | ----------------------------------- |
| 0      | `OK`                     | Accepted                            |
| 1      | `UnsupportedVersion`     | Wire version unsupported            |
| 2      | `SchemaMismatch`         | Schema did not match                |
| 3      | `UnsupportedCompression` | Zstandard unavailable or disallowed |

An `OK` response to a call hello ends after this byte.
Framing starts with the next byte.

Inspection responses and rejected calls append a server description:

```text
status:u8 version:u8 family:bytes[32] fingerprint:bytes[32]
source_length:uvarint source:bytes[source_length]
```

`version` is the server's wire version.
`source` MUST be canonical UTF-8 schema text and MUST NOT exceed 16 MiB.
After sending a description, the server MUST close the connection.
Malformed hellos, such as an invalid magic or reserved flag, may be closed without a response.

For a call, the server compares the first 16 bytes of the family hashes.
If either peer requires an exact match, it also compares the fingerprint prefixes.
A mismatch produces `SchemaMismatch`.
Family matching does not check whether schema changes are [compatible](schema.md#compatible-evolution).

A server accepting bit 2 MUST enable Zstandard compression for the connection.
Otherwise it MUST return `UnsupportedCompression`.
Early version 1 servers reject this flag without a response;
a client MAY reconnect without compression.
No method call has been sent at that point.

## Frames

Each frame has a length prefix followed by a kind-specific body:

```text
+-------------+------+------------+----------------+---------+
| frame size  | kind | request ID | optional code  | payload |
|  uvarint    |  u8  |  uvarint   | 0, 1, or 4 B  |         |
+-------------+------+------------+----------------+---------+
              \________ frame size bytes __________/
```

`frame size` counts all bytes after the prefix.
A receiver MUST reject frames larger than its configured limit.

Request ID zero is reserved.
Clients MUST use nonzero IDs and MUST NOT reuse an ID while its call is active.
Responses may arrive in any order, but MUST use the corresponding request's ID.

| Kind | Name               | Direction        | Code                | Payload        |
| ---: | ------------------ | ---------------- | ------------------- | -------------- |
| 1    | `Request`          | Client to server | Method `u32le`      | Request fields |
| 2    | `Response`         | Server to client | None                | Response value |
| 3    | `ApplicationError` | Server to client | Constructor `u32le` | Error fields   |
| 4    | `ProtocolError`    | Server to client | Core error `u8`     | UTF-8 message  |
| 5    | `Cancel`           | Client to server | None                | Empty          |

A peer MUST NOT send a kind in the wrong direction.
A `Cancel` MUST have an empty payload.
`Response` and `Cancel` have no code.

Bit 7 of `kind` marks a compressed payload;
the other seven bits select the kind.
Bit 7 MUST be zero unless compression was accepted in the handshake.

### Compression

Only the payload is compressed.
The kind, request ID, and code stay uncompressed.
A compressed payload contains:

```text
decoded_size:uvarint zstandard_data
```

`decoded_size` MUST be positive and equal the decompressed payload's byte length.
Zstandard data MUST use [RFC 8878] with a window no larger than 1 MiB.
Each payload is independent; compression state never crosses a frame boundary.

A sender MAY leave a payload uncompressed.
It SHOULD avoid compressing empty, small, or incompressible payloads.
A receiver MUST reject invalid compressed data, a size mismatch,
or a decoded frame exceeding its configured limit.
The transmitted frame must also fit that limit.

### Frame example

For the original [greeting service](intro.md), with no HTTP binding,
the method ID is `0x54dee25b`.
Calling `greeting.greet(name: "Ada")` as request 1 produces:

```text
0a 01 01 5b e2 de 54 03 41 64 61
|  |  |  \_________/ |  \______/
|  |  |    method   |    Ada
|  |  |             +-- string byte length 3
|  |  +---------------- request ID 1
|  +------------------- Request
+---------------------- ten body bytes
```

The successful `"Hello, Ada!"` response is:

```text
0e 02 01 0b 48 65 6c 6c 6f 2c 20 41 64 61 21
|  |  |  |  \______________________________/
|  |  |  |             Hello, Ada!
|  |  |  +-- string byte length 11
|  |  +----- request ID 1
|  +-------- Response
+----------- fourteen body bytes
```

### Example from a schema export

The greeting method's ID input is:

```text
greeting.greet name:string -> string
```

Its fingerprint input is these two lines, without a final newline:

```text
package greeting version 1
greeting.greet name:string -> string public
```

The resulting values are:

```text
family       e8a2564c6385458545099556c056eca38a8848cf59e4b774c49ace47c9ce7465
fingerprint  92f3c0f69028f948ec0909cf1d337f8bcc2596a132dd3af383e7412443e37adf
method ID    54dee25b
```

A call hello with no flags is:

```text
50 4c 55 47 01 00
e8 a2 56 4c 63 85 45 85 45 09 95 56 c0 56 ec a3
92 f3 c0 f6 90 28 f9 48 ec 09 09 cf 1d 33 7f 8b
```

Generate metadata from the tutorial's original schema:

```sh
$ go tool plug generate -json -o greeting.json greeting/schema.plug
```

This Python example obtains the same hello and request bytes without implementing the identity algorithms:

```python
import json
import struct

with open("greeting.json", encoding="utf-8") as f:
    schema = json.load(f)

if schema["format_version"] != 1 or schema["protocol_version"] != 1:
    raise ValueError("unsupported Plug schema export")

greet = next(m for m in schema["methods"] if m["name"] == "greeting.greet")
hello = (
    b"PLUG"
    + bytes([schema["protocol_version"], 0])
    + bytes.fromhex(schema["family"])[:16]
    + bytes.fromhex(schema["fingerprint"])[:16]
)
request = bytes([10, 1, 1]) + struct.pack("<I", greet["id"]) + b"\x03Ada"
print(hello.hex(), request.hex())
```

## Value encoding

| Value        | Encoding                                                   |
| ------------ | ---------------------------------------------------------- |
| `bool`       | `00` for false or `01` for true                            |
| `i32`, `i64` | ZigZag `varint`                                            |
| `u32`, `u64` | `uvarint`                                                  |
| `f64`        | Eight-byte IEEE 754 binary64, little-endian                |
| `string`     | Byte length as `uvarint`, then valid UTF-8                 |
| `bytes`      | Byte length as `uvarint`, then bytes                       |
| `void`       | Empty                                                      |
| `T?`         | Boolean presence byte, then `T` when present               |
| `list<T>`    | Element count as `uvarint`, then each element              |
| Newtype      | Its underlying scalar                                      |
| Record       | Body length as `uvarint`, then fields                      |
| Sum          | Constructor `u32le`, body length as `uvarint`, then fields |

Values MUST fit their declared integer ranges.
For records and sums, the body length counts only the encoded fields.

A request encodes its fields directly;
the frame supplies the method ID and payload boundary.
An application error also encodes fields directly;
the frame supplies the constructor ID and payload boundary.
Neither has an additional body-length prefix.

Fields follow declaration order.
An encoder MAY omit absent optional fields at the end of a value.
For an absent field followed by a present field, it MUST emit the false presence byte.
A decoder MUST treat a missing optional tail as absent
and MUST skip unknown bytes at the end of a request, record body, sum body, or application error.
Bytes outside those extension boundaries are not valid padding.

An unknown constructor of a known sum MUST retain its ID and uninterpreted body,
so an older peer can receive and return values added by a compatible schema.
For sum error types, unknown constructors likewise retain the frame's constructor ID and error payload.

A decoder MUST reject truncation, invalid boolean tags, invalid UTF-8,
integer overflow, and values exceeding its resource limits.
Limits are local; the protocol does not negotiate them.

The Go runtime defaults to a 16 MiB maximum frame,
1,048,576 decoded list elements per payload,
and a 64 MiB decoding allocation budget excluding frame buffers.
Its list limit also bounds each encoded list.
See [Limits](https://pkg.go.dev/go.astrophena.name/plug#Limits) for configuration.

## Calls

Clients MAY have several requests active on a connection.
Writes MUST remain frame-aligned.
Servers MAY execute calls concurrently and return responses out of order.

The server decodes the request, authorizes it when required, and runs the handler.
It returns a `Response`, `ApplicationError`, or `ProtocolError`.

A client abandoning a call SHOULD send `Cancel`.
The server MUST cancel the matching call and suppress a later response.
Cancellation is advisory: a handler may take time to stop.
An unknown or completed ID in `Cancel` MAY be ignored.
Canceled handlers count toward the concurrency limit until they return.
Reusing a canceled ID does not permit the old handler to send a response for the new call.

## Errors

| Code | Name               | Meaning                            |
| ---: | ------------------ | ---------------------------------- |
| 1    | `BadRequest`       | Request did not match the schema   |
| 2    | `Internal`         | Server could not complete the call |
| 3    | `Unauthenticated`  | Method requires a principal        |
| 4    | `PermissionDenied` | Principal is not permitted         |
| 5    | `Unimplemented`    | Method ID is unknown               |

An unknown method SHOULD produce `Unimplemented` without closing the connection.
A declared handler error MUST produce `ApplicationError`.
Other handler failures and panics produce `Internal`.

When a malformed request can be identified,
a server SHOULD send `BadRequest` before closing the connection.
A peer MUST close after invalid framing, an invalid response payload, or an I/O error.
Closing fails every active call.

Credentials are not Plug messages.
A server derives a principal from the transport
and authorizes restricted methods after decoding their requests.

## Inspection

Inspection uses the six-byte hello and does not enter frame mode.
A server supporting the requested wire version MUST return `OK` and its description, then close.

Inspection exposes the complete hashes and canonical schema without generated client bindings.
Transport authentication still applies.

## Raw connection takeover

A raw method MUST use a fresh call connection and be its first request.
No other call may be active.

```text
Client                                      Server
  |---- Request(raw method, request ID) ----->|
  |                                           | decode, authorize, set up
  |<--- empty Response with same request ID --|
  |                                           |
  |<========== application bytes ===========>|
```

Before accepting, the server MAY return an application or protocol error.
Acceptance is an empty `Response`.
After that complete frame, both peers MUST stop framing and treat subsequent bytes as application data.

Plug compression ends with framing.
After takeover, Plug defines no message boundaries, cancellation frames, or error format.
Closing the stream is the only protocol-independent termination.

## WebSocket transport

WebSocket carries the entire Plug stream, including the handshake and raw takeover.
Peers MUST use binary messages and concatenate their payloads.
Plug frames MAY span messages or share a message.
Text messages are rejected.
WebSocket ping and pong frames are outside the Plug stream.

Native HTTP Upgrade sends an HTTP/1.1 `GET` with `Connection: Upgrade` and `Upgrade: plug`.
The server accepts with `101 Switching Protocols` and the same two headers;
the upgraded connection then carries the Plug byte stream.
See [HTTP](http.md#native-plug-over-http) for adapter usage.

[RFC 2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC 8878]: https://www.rfc-editor.org/rfc/rfc8878.html
