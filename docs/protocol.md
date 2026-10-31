# Plug protocol (version 1)

## Status

This document defines version 1 of the native Plug wire protocol. The schema
language is specified separately in [schema.md](schema.md).

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHOULD**, **SHOULD NOT**,
and **MAY** are to be interpreted as described by
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

Plug runs over a reliable, ordered byte stream such as a Unix or TCP stream
socket. Encryption, peer identity, retries, discovery, and connection pooling
are outside the protocol.

## Notation

This document uses these field types:

| Type       | Encoding                                                 |
| ---------- | -------------------------------------------------------- |
| `u8`       | One unsigned byte                                        |
| `u32le`    | Four-byte unsigned integer, least-significant byte first |
| `uvarint`  | Unsigned base-128 varint                                 |
| `varint`   | ZigZag integer encoded as a `uvarint`                    |
| `bytes[n]` | Exactly `n` bytes                                        |

Lengths count bytes, not characters. Unless stated otherwise, integers have no
padding and variable fields immediately follow the preceding field.

## Connection handshake

Every connection begins with a client hello and a server hello.

```text
Client                                      Server
  |                                           |
  |------------- ClientHello ---------------->|
  |                                           |
  |<------------ ServerHello -----------------|
  |                                           |
  |       frames, or close after description  |
```

### Client hello

A client making calls MUST send a 38-byte call hello:

```text
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-------+---+---+-------------------------------+-------------------
| PLUG  |ver|flg| family SHA-256 prefix (16 B)  | fingerprint ...
+-------+---+---+-------------------------------+-------------------
  bytes 0..3    bytes 6..21                       bytes 22..37
```

| Offset | Size | Field                                   |
| -----: | ---: | --------------------------------------- |
|      0 |    4 | ASCII `PLUG`                            |
|      4 |    1 | Wire version, currently `1`             |
|      5 |    1 | Flags                                   |
|      6 |   16 | Compatibility-family SHA-256 prefix     |
|     22 |   16 | Exact-schema SHA-256 fingerprint prefix |

Flag bit 0 requests inspection. Flag bit 1 requests an exact schema match. Flag
bit 2 requests Zstandard frame compression. All other bits MUST be zero.

An inspection hello ends after byte 5. It is therefore six bytes long, MUST set
bit 0, and MUST NOT set bits 1 or 2. A call hello MUST NOT set bit 0.

The family and fingerprint algorithms are defined by the
[schema specification](schema.md#identity).

### Server hello

The server first sends a status byte:

| Status | Name                     | Meaning                              |
| -----: | ------------------------ | ------------------------------------ |
|      0 | `OK`                     | The request was accepted             |
|      1 | `UnsupportedVersion`     | The wire version is unsupported      |
|      2 | `SchemaMismatch`         | The requested schema did not match   |
|      3 | `UnsupportedCompression` | Zstandard compression is unavailable |

An `OK` response to a call hello ends after this byte. Framing begins with the
next byte.

An inspection response and every rejected call append a server description:

```text
+--------+---------+------------+----------------+----------+--------+
| status | version | family[32] | fingerprint[32]| source n | source |
|  u8    |   u8    |            |                | uvarint  | n bytes|
+--------+---------+------------+----------------+----------+--------+
```

The source MUST be the canonical UTF-8 schema and MUST NOT exceed 16 MiB. After
sending a description, the server MUST close the connection.

For an ordinary call, peers compare the first 16 bytes of their family hashes.
If either peer requires an exact match, they MUST also compare the first 16
bytes of their exact fingerprints. A mismatch produces `SchemaMismatch`.

A server that accepts flag bit 2 MUST enable Zstandard compression for the
connection. A server that does not support or permit compression MUST return
`UnsupportedCompression`. Early version 1 servers reject the flag without a
response, so a client MAY reconnect without compression. No method call has been
sent at that point.

### Handshake example

The shortest successful exchange is 39 bytes: a 38-byte call hello followed by
the byte `00`. The connection is then ready for frames.

## Frames

Each frame has a length prefix and a kind-specific body:

```text
+-------------+------+------------+----------------+---------+
| frame size  | kind | request ID | optional code  | payload |
|  uvarint    |  u8  |  uvarint   | 0, 1, or 4 B  |         |
+-------------+------+------------+----------------+---------+
              \________ frame size bytes __________/
```

`frame size` counts every byte after the prefix. A receiver MUST reject a frame
larger than its configured limit. The default limit is 16 MiB.

Request ID zero is reserved. A client MUST use a nonzero ID and MUST NOT reuse
an ID while its call is active. A server MAY send responses in any order, but a
response MUST use the ID of its request.

| Kind | Name               | Direction        | Code                | Payload        |
| ---: | ------------------ | ---------------- | ------------------- | -------------- |
|    1 | `Request`          | Client to server | Method `u32le`      | Request fields |
|    2 | `Response`         | Server to client | None                | Response value |
|    3 | `ApplicationError` | Server to client | Constructor `u32le` | Error fields   |
|    4 | `ProtocolError`    | Server to client | Core error `u8`     | UTF-8 message  |
|    5 | `Cancel`           | Client to server | None                | Empty          |

A peer MUST NOT send a kind in the wrong direction. A `Cancel` frame MUST have
an empty payload. A `Response` or `Cancel` frame has no code.

Bit 7 of `kind` marks a compressed payload. The remaining seven bits identify
the frame kind in the table. Bit 7 MUST be zero unless compression was accepted
during the handshake.

### Compression

Compression applies only to the payload. The frame kind, request ID, and code
remain uncompressed. A compressed payload has this form:

```text
+-------------------+------------------+
| decoded size      | Zstandard data   |
| uvarint           |                  |
+-------------------+------------------+
```

`decoded size` is the exact number of bytes in the original payload. The
Zstandard data MUST use the format from [RFC 8878] and a window no larger than 1
MiB. Each payload is compressed independently; compression state never crosses a
frame boundary.

A sender MAY leave any payload uncompressed. It SHOULD avoid compressing empty,
small, or incompressible payloads. A receiver MUST reject compressed data that
is invalid, expands to a different size, or makes the decoded frame exceed its
configured frame limit. The transmitted frame MUST also fit that limit.

### Frame example

The generated ID of `greeter.ping` is `0xd28fc86c`. Calling it as request 1 with
no fields produces:

```text
06 01 01 6c c8 8f d2
|  |  |  \_________/ method ID, little-endian
|  |  +-------------- request ID 1
|  +----------------- Request
+-------------------- six bytes follow
```

An empty successful response is:

```text
02 02 01
|  |  +-- request ID 1
|  +----- Response
+-------- two bytes follow
```

## Value encoding

Frame payloads encode schema values as follows:

| Value        | Encoding                                                   |
| ------------ | ---------------------------------------------------------- |
| `bool`       | `00` for false or `01` for true                            |
| `i32`, `i64` | ZigZag `varint`                                            |
| `u32`, `u64` | `uvarint`                                                  |
| `f64`        | Eight-byte IEEE 754 value, little-endian                   |
| `string`     | Byte length as `uvarint`, then valid UTF-8                 |
| `bytes`      | Byte length as `uvarint`, then bytes                       |
| `void`       | Empty                                                      |
| `T?`         | Boolean presence byte, then `T` when present               |
| `list<T>`    | Element count as `uvarint`, then each element              |
| Newtype      | Its underlying scalar                                      |
| Record       | Body length as `uvarint`, then fields in declaration order |
| Sum          | Constructor `u32le`, body length as `uvarint`, then fields |

For signed integers, `0`, `-1`, and `1` encode as the unsigned values `0`, `1`,
and `2`. Encoders MUST reject values outside the declared integer range.

A method request contains its fields directly because the frame already names
the method. An application error also contains its fields directly because the
frame carries the constructor ID.

Fields are encoded in declaration order. An encoder MAY omit absent optional
fields at the end of a value. It MUST emit the false presence byte for an absent
field followed by a present field. A decoder MUST treat a missing optional tail
as absent and MUST ignore unknown bytes at the end of a request, record, sum, or
application error.

An unknown constructor of a known sum MUST be retained as its constructor ID and
uninterpreted body. This allows an older peer to receive and return values added
by a compatible schema.

A decoder MUST reject truncation, invalid boolean tags, invalid UTF-8, integer
overflow, and values outside configured resource limits. The default maximum
list length is 1,048,576 elements. The Go runtime also bounds the total decoded
list elements per payload by this limit. Its default allocation budget is 64 MiB
per payload, including nested values and replaced slice storage. Frame buffers
have their own limit. These are local limits and are not negotiated.

## Calls

A client MAY have several requests active on the same connection. Writes MUST
remain frame-aligned. The server MAY execute calls concurrently and return their
responses out of order.

For each request, the server performs these steps:

```text
decode request
      |
      v
authorize, when required
      |
      v
run handler
      |
      +---- success ----------> Response
      +---- declared error ---> ApplicationError
      +---- other failure ----> ProtocolError
```

A client that abandons a call SHOULD send `Cancel`. The server MUST cancel the
matching call when it receives that frame and MUST suppress a later response.
Cancellation is advisory: the handler may take time to stop. An unknown or
already completed request ID in `Cancel` MAY be ignored. Canceled handlers count
toward the server concurrency limit until they return. Reusing a canceled call
ID does not make the old handler eligible to send a response.

## Errors

Core error codes are:

| Code | Name               | Meaning                                |
| ---: | ------------------ | -------------------------------------- |
|    1 | `BadRequest`       | The request did not match the schema   |
|    2 | `Internal`         | The server could not complete the call |
|    3 | `Unauthenticated`  | The method requires a principal        |
|    4 | `PermissionDenied` | The principal is not permitted         |
|    5 | `Unimplemented`    | The method ID is unknown               |

An unknown method SHOULD produce `Unimplemented` without closing the connection.
A declared handler error MUST produce `ApplicationError`. Other handler failures
and panics produce `Internal`.

When enough of a malformed request is available to identify it, a server SHOULD
send `BadRequest` before closing the connection. A peer MUST close the
connection after invalid framing, an invalid response payload, or an I/O error.
Closing the connection fails all active calls.

Authentication credentials are not Plug messages. A server derives a principal
from the transport and authorizes a restricted method after decoding its
request.

## Inspection

Inspection uses the short client hello and does not enter frame mode. A server
that supports the requested wire version MUST return `OK` followed by its full
description, then close the connection.

Inspection exposes the canonical schema and complete hashes without requiring
generated bindings for that service.

## Raw connection takeover

A raw method uses a fresh call connection and MUST be its first request. No
other call may be active.

```text
Client                                      Server
  |---- Request(raw method, request ID) ----->|
  |                                           | decode, authorize, set up
  |<--- empty Response with same request ID --|
  |                                           |
  |<========== application bytes ===========>|
```

Before acceptance, the server MAY reject the request with an application or
protocol error. Acceptance is an empty `Response`. After that complete frame,
both peers MUST stop Plug framing and treat all subsequent bytes as application
data.

Compression ends with Plug framing. Bytes sent after raw takeover are not
compressed by Plug.

Plug defines no message boundaries, cancellation frames, or error format after
takeover. Closing the stream is the only protocol-independent termination.

## WebSocket transport

WebSocket carries the Plug byte stream, including the handshake and raw
takeover. Peers MUST use binary messages and concatenate their payloads. Plug
frames MAY span messages or share a message. Text messages are rejected.
WebSocket ping and pong frames are outside the Plug stream.

## Scope

Version 1 has no framed streaming, server push, retries, or credential format.
HTTP projection is an adapter described by the
[schema specification](schema.md#http-bindings), not another Plug wire mode.

[RFC 8878]: https://www.rfc-editor.org/rfc/rfc8878.html
