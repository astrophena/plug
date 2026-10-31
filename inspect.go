// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// Inspect retrieves a peer's canonical schema without knowing its fingerprint.
// It takes ownership of conn and closes it after the exchange.
func Inspect(ctx context.Context, conn net.Conn) (remote RemoteSchema, err error) {
	if conn == nil {
		return RemoteSchema{}, fmt.Errorf("plug: connection is nil")
	}
	defer func() { err = errors.Join(err, conn.Close()) }()
	err = runConnIO(ctx, conn, func() error {
		if err := writeClientHello(conn, handshakeInspect, false, false, [32]byte{}, [32]byte{}); err != nil {
			return err
		}
		status, value, err := readServerHello(conn, true)
		if err != nil {
			return err
		}
		remote = value
		if status == handshakeUnsupportedVersion || remote.Version != wireVersion {
			return &VersionMismatchError{
				Local:  wireVersion,
				Remote: remote,
			}
		}
		if status != handshakeOK {
			return fmt.Errorf("plug: inspect failed with handshake status %d", status)
		}
		return nil
	})
	if err != nil {
		return RemoteSchema{}, err
	}
	return remote, nil
}
