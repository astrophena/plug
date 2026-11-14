// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"go.astrophena.name/plug/plugimpl"
)

// ErrCloseWriteUnsupported means the transport cannot close its write side separately.
var ErrCloseWriteUnsupported = errors.New("plug: raw connection does not support CloseWrite")

// RawConn is an accepted raw connection.
// Reads preserve buffered data.
type RawConn interface {
	net.Conn

	// CloseWrite closes the write side,
	// returning [ErrCloseWriteUnsupported] if unsupported.
	CloseWrite() error
}

type rawConn struct {
	net.Conn
	reader      io.Reader
	stopContext func() bool
	close       func() error
}

func newRawConn(ctx context.Context, conn net.Conn, reader io.Reader) *rawConn {
	closeConn := sync.OnceValue(conn.Close)
	c := &rawConn{
		Conn:   conn,
		reader: reader,
		close:  closeConn,
	}
	c.stopContext = context.AfterFunc(ctx, func() { closeConn() })
	return c
}

func (c *rawConn) Read(data []byte) (int, error) { return c.reader.Read(data) }

func (c *rawConn) Close() error {
	c.stopContext()
	return c.close()
}

func (c *rawConn) CloseWrite() error {
	conn, ok := c.Conn.(interface{ CloseWrite() error })
	if !ok {
		return ErrCloseWriteUnsupported
	}
	return conn.CloseWrite()
}

// RawStream switches a connection from RPC frames to raw bytes.
// Returning an error before [RawStream.Accept] rejects the request.
type RawStream struct {
	conn   RawConn
	accept func() error

	mu       sync.Mutex
	accepted bool
}

// Accept switches to raw I/O and returns the connection.
// It may be called once.
func (s *RawStream) Accept() (RawConn, error) {
	if s == nil {
		return nil, fmt.Errorf("plug: raw stream is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accepted {
		return nil, fmt.Errorf("plug: raw stream is already accepted")
	}
	if err := s.accept(); err != nil {
		return nil, err
	}
	s.accepted = true
	return s.conn, nil
}

func (s *RawStream) isAccepted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accepted
}

// OpenRaw calls method on conn and takes ownership of it.
// It closes conn on error or when ctx is canceled.
func OpenRaw[Request any](ctx context.Context, conn net.Conn, method plugimpl.RawMethod[Request], req Request, opts ClientOptions) (raw RawConn, err error) {
	if conn == nil {
		return nil, fmt.Errorf("plug: connection is nil")
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, conn.Close())
		}
	}()

	schema, limits, err := normalizeSchemaLimits(method.Schema(), opts.Limits)
	if err != nil {
		return nil, err
	}
	if err := method.Validate(schema); err != nil {
		return nil, err
	}
	compression, err := clientHandshake(ctx, conn, schema, opts.StrictSchema, opts.Compression)
	if err != nil {
		return nil, err
	}

	payload, err := plugimpl.Encode(method.Request(), req, limits.MaxListElements)
	if err != nil {
		return nil, fmt.Errorf("plug: encode request for %s: %w", method.Info().Name, err)
	}

	r := bufio.NewReaderSize(conn, 32*1024)
	f := newFramer(conn, r, limits, compression)
	defer f.close()
	var resp frame
	err = runConnIO(ctx, conn, func() error {
		if err := f.writeRequest(1, method.ID(), payload); err != nil {
			return err
		}
		resp, err = f.readFrame()
		return err
	})
	if err != nil {
		return nil, err
	}
	if resp.requestID != 1 {
		return nil, fmt.Errorf("plug: raw response uses request ID %d, want 1", resp.requestID)
	}
	if resp.kind == frameResponse {
		if len(resp.payload) != 0 {
			return nil, fmt.Errorf("plug: raw response has %d payload bytes", len(resp.payload))
		}
		return newRawConn(ctx, conn, r), nil
	}

	responseErr, _ := decodeResultError(method.Info(), method.Errors(), resp, limits, "raw response")
	return nil, responseErr
}
