// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import "fmt"

// ErrorCode identifies a core RPC failure.
type ErrorCode uint32

const (
	ErrorBadRequest ErrorCode = iota + 1
	ErrorInternal
	ErrorUnauthenticated
	ErrorPermissionDenied
	ErrorUnimplemented
)

// RemoteError is a core error returned by a peer.
type RemoteError struct {
	Code    ErrorCode
	Message string
}

func (e *RemoteError) Error() string {
	return fmt.Sprintf("plug: remote error %d: %s", e.Code, e.Message)
}

// SchemaMismatchError reports an exact-schema handshake failure.
type SchemaMismatchError struct {
	Local  [32]byte
	Remote RemoteSchema
	Exact  bool
}

func (e *SchemaMismatchError) Error() string {
	if e.Exact {
		return fmt.Sprintf("plug: exact schema mismatch: local %x, remote %x", e.Local, e.Remote.Fingerprint)
	}
	return fmt.Sprintf("plug: schema family mismatch: local %x, remote %x", e.Local, e.Remote.Family)
}

// VersionMismatchError reports an unsupported wire version.
type VersionMismatchError struct {
	Local  uint16
	Remote RemoteSchema
}

func (e *VersionMismatchError) Error() string {
	return fmt.Sprintf("plug: wire version mismatch: local %d, remote %d", e.Local, e.Remote.Version)
}
