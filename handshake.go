// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
	"unicode/utf8"
)

const wireVersion uint16 = 1

const (
	clientHeaderSize  = 6
	handshakeHashSize = 16
	callHelloSize     = clientHeaderSize + 2*handshakeHashSize

	handshakeFlagInspect = 1 << 0
	handshakeFlagStrict  = 1 << 1
	handshakeFlagZstd    = 1 << 2
)

var wireMagic = [4]byte{'P', 'L', 'U', 'G'}

type handshakeMode uint8

const (
	handshakeCall handshakeMode = iota
	handshakeInspect
)

type handshakeStatus uint8

const (
	handshakeOK handshakeStatus = iota
	handshakeUnsupportedVersion
	handshakeSchemaMismatch
	handshakeUnsupportedCompression
)

var errCompressionUnsupported = errors.New("plug: compression is unsupported")

type clientHello struct {
	version     uint16
	mode        handshakeMode
	strict      bool
	compression bool
	family      [handshakeHashSize]byte
	fingerprint [handshakeHashSize]byte
}

func readClientHello(conn net.Conn) (clientHello, error) {
	var header [clientHeaderSize]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return clientHello{}, err
	}
	if [4]byte(header[:4]) != wireMagic {
		return clientHello{}, fmt.Errorf("plug: invalid wire magic %q", header[:4])
	}
	flags := header[5]
	if flags&^(handshakeFlagInspect|handshakeFlagStrict|handshakeFlagZstd) != 0 {
		return clientHello{}, fmt.Errorf("plug: unknown client handshake flags 0x%02x", flags)
	}
	result := clientHello{
		version:     uint16(header[4]),
		strict:      flags&handshakeFlagStrict != 0,
		compression: flags&handshakeFlagZstd != 0,
	}
	if flags&handshakeFlagInspect != 0 {
		result.mode = handshakeInspect
		if result.strict || result.compression {
			return clientHello{}, fmt.Errorf("plug: inspect handshake uses call-only flags")
		}
		return result, nil
	}

	var hashes [2 * handshakeHashSize]byte
	if _, err := io.ReadFull(conn, hashes[:]); err != nil {
		return clientHello{}, err
	}
	copy(result.family[:], hashes[:handshakeHashSize])
	copy(result.fingerprint[:], hashes[handshakeHashSize:])
	return result, nil
}

func writeClientHello(conn net.Conn, mode handshakeMode, strict, compression bool, family, fingerprint [32]byte) error {
	data := make([]byte, clientHeaderSize, callHelloSize)
	copy(data[:4], wireMagic[:])
	data[4] = byte(wireVersion)
	if mode == handshakeInspect {
		data[5] |= handshakeFlagInspect
	}
	if strict {
		data[5] |= handshakeFlagStrict
	}
	if compression {
		data[5] |= handshakeFlagZstd
	}
	if mode == handshakeCall {
		data = append(data, family[:handshakeHashSize]...)
		data = append(data, fingerprint[:handshakeHashSize]...)
	}
	return writeAll(conn, data)
}

func writeServerHello(conn net.Conn, status handshakeStatus, schema *Schema, includeDescription bool) error {
	if !includeDescription {
		return writeAll(conn, []byte{byte(status)})
	}
	data := make([]byte, 0, 1+1+32+32+binary.MaxVarintLen32+len(schema.Source))
	data = append(data, byte(status), byte(wireVersion))
	data = append(data, schema.Family[:]...)
	data = append(data, schema.Fingerprint[:]...)
	data = binary.AppendUvarint(data, uint64(len(schema.Source)))
	data = append(data, schema.Source...)
	return writeAll(conn, data)
}

func readServerHello(conn net.Conn, includeDescription bool) (handshakeStatus, RemoteSchema, error) {
	var data [1]byte
	if _, err := io.ReadFull(conn, data[:]); err != nil {
		return 0, RemoteSchema{}, err
	}
	status := handshakeStatus(data[0])
	if status > handshakeUnsupportedCompression {
		return 0, RemoteSchema{}, fmt.Errorf("plug: unknown handshake status %d", status)
	}
	if !includeDescription {
		return status, RemoteSchema{}, nil
	}
	remote, err := readServerDescription(conn)
	return status, remote, err
}

func readServerDescription(conn net.Conn) (RemoteSchema, error) {
	var data [1 + 32 + 32]byte
	if _, err := io.ReadFull(conn, data[:]); err != nil {
		return RemoteSchema{}, err
	}
	remote := RemoteSchema{
		Version: uint16(data[0]),
	}
	copy(remote.Family[:], data[1:33])
	copy(remote.Fingerprint[:], data[33:])
	source, err := readSchemaSource(conn)
	if err != nil {
		return RemoteSchema{}, err
	}
	remote.Source = source
	return remote, nil
}

func clientHandshake(ctx context.Context, conn net.Conn, schema *Schema, strict, compression bool) (bool, error) {
	err := runConnIO(ctx, conn, func() error {
		if err := writeClientHello(conn, handshakeCall, strict, compression, schema.Family, schema.Fingerprint); err != nil {
			if compression && ctx.Err() == nil {
				return errors.Join(errCompressionUnsupported, err)
			}
			return err
		}
		status, remote, err := readServerHello(conn, false)
		if err != nil {
			if compression && ctx.Err() == nil {
				return errors.Join(errCompressionUnsupported, err)
			}
			return err
		}
		if status != handshakeOK {
			remote, err = readServerDescription(conn)
			if err != nil {
				return err
			}
		}
		switch status {
		case handshakeOK:
			return nil
		case handshakeUnsupportedVersion:
			return &VersionMismatchError{
				Local:  wireVersion,
				Remote: remote,
			}
		case handshakeSchemaMismatch:
			exact := remote.Family == schema.Family
			local := schema.Family
			if exact {
				local = schema.Fingerprint
			}
			return &SchemaMismatchError{
				Local:  local,
				Remote: remote,
				Exact:  exact,
			}
		case handshakeUnsupportedCompression:
			return errCompressionUnsupported
		default:
			return fmt.Errorf("plug: unknown handshake status %d", status)
		}
	})
	return compression && err == nil, err
}

func readSchemaSource(conn net.Conn) (string, error) {
	size, err := readStreamUvarint(conn)
	if err != nil {
		return "", err
	}
	if size > uint64(DefaultMaxFrameSize) {
		return "", fmt.Errorf("plug: remote schema size %d exceeds maximum %d", size, DefaultMaxFrameSize)
	}
	source := make([]byte, int(size))
	if _, err := io.ReadFull(conn, source); err != nil {
		return "", err
	}
	if !utf8.Valid(source) {
		return "", fmt.Errorf("plug: remote schema is not UTF-8")
	}
	return string(source), nil
}

func handshakeHash(value [32]byte) [handshakeHashSize]byte {
	return [handshakeHashSize]byte(value[:handshakeHashSize])
}

func interruptConnOnContext(ctx context.Context, conn net.Conn) func() error {
	fired := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(fired)
		conn.SetDeadline(time.Now())
	})
	return func() error {
		if stop() {
			return nil
		}
		<-fired
		return conn.SetDeadline(time.Time{})
	}
}

func runConnIO(ctx context.Context, conn net.Conn, run func() error) (err error) {
	cleanup := interruptConnOnContext(ctx, conn)
	defer func() {
		cleanupErr := cleanup()
		if contextErr := ctx.Err(); contextErr != nil {
			err = errors.Join(contextErr, err, cleanupErr)
			return
		}
		err = errors.Join(err, cleanupErr)
	}()
	return run()
}
