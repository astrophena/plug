// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package browser

const protocolMap = `schema.plug
    │
    ▼
generated API
    │
    ├── native → handshake + frames ──┐
    │                                 │
    └── HTTP → URL + JSON ────────────┤
                                      ▼
                         authenticate + authorize
                                      │
                                      ▼
                                   handler
                                      │
                                      ▼
                              response or error`

const protocolPython = `import socket

SOCKET = "/run/example/plug.sock"


def receive_exact(connection, size):
    result = bytearray()
    while len(result) < size:
        chunk = connection.recv(size - len(result))
        if not chunk:
            raise EOFError("Plug server closed the connection")
        result.extend(chunk)
    return bytes(result)


def receive_uvarint(connection):
    value = 0
    for shift in range(0, 70, 7):
        part = receive_exact(connection, 1)[0]
        value |= (part & 0x7F) << shift
        if part < 0x80:
            return value
    raise ValueError("Plug varint is too large")


with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
    connection.connect(SOCKET)

    # "PLUG", wire version 1, inspection flag.
    connection.sendall(b"PLUG\x01\x01")

    status = receive_exact(connection, 1)[0]
    if status != 0:
        raise RuntimeError(f"Plug inspection failed with status {status}")

    wire_version = receive_exact(connection, 1)[0]
    family = receive_exact(connection, 32).hex()
    fingerprint = receive_exact(connection, 32).hex()
    schema_size = receive_uvarint(connection)
    schema = receive_exact(connection, schema_size).decode("utf-8")

print(f"wire version: {wire_version}")
print(f"family: {family}")
print(f"fingerprint: {fingerprint}")
print()
print(schema)`
