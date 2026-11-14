// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"go.astrophena.name/plug/plugimpl"
)

// ErrClientClosed reports use of a closed client.
var ErrClientClosed = errors.New("plug: client is closed")

const numPendingShards = 16

type pendingShard struct {
	mu sync.Mutex
	m  map[uint64]*pendingCall
}

type pendingCall struct {
	response chan frame
}

type clientRequest struct {
	id     uint64
	method uint32
	enc    *plugimpl.Encoder
}

// Client makes RPC calls over a connection.
// Its methods are safe for concurrent use.
type Client struct {
	conn   net.Conn
	framer *framer
	schema *Schema
	limits Limits

	nextID atomic.Uint64

	shards      [numPendingShards]pendingShard
	pendingPool sync.Pool
	writes      chan clientRequest

	cancelMu    sync.Mutex
	cancels     []uint64
	cancelReady chan struct{}

	closeOnce sync.Once
	closeErr  error
	done      chan struct{}
}

// NewClient performs the handshake on conn and takes ownership of it.
// It keeps an independent copy of schema.
func NewClient(ctx context.Context, conn net.Conn, schema *Schema, opts ClientOptions) (*Client, error) {
	if conn == nil {
		return nil, fmt.Errorf("plug: connection is nil")
	}

	schema, limits, err := normalizeSchemaLimits(schema, opts.Limits)
	if err != nil {
		return nil, errors.Join(err, conn.Close())
	}
	compression, err := clientHandshake(ctx, conn, schema, opts.StrictSchema, opts.Compression)
	if err != nil {
		return nil, errors.Join(err, conn.Close())
	}

	c := &Client{
		conn:        conn,
		schema:      schema,
		limits:      limits,
		writes:      make(chan clientRequest),
		cancelReady: make(chan struct{}, 1),
		done:        make(chan struct{}),
		pendingPool: sync.Pool{
			New: func() any {
				return &pendingCall{
					response: make(chan frame, 1),
				}
			},
		},
	}
	c.framer = newFramer(conn, bufio.NewReaderSize(conn, 32*1024), c.limits, compression)
	for i := range c.shards {
		c.shards[i].m = make(map[uint64]*pendingCall)
	}

	go c.readLoop()
	go c.writeLoop()

	return c, nil
}

// Dial connects to address and returns a [Client].
// See [DialFunc] for compression fallback.
func Dial(ctx context.Context, network, address string, schema *Schema, opts ClientOptions) (*Client, error) {
	var dialer net.Dialer
	return DialFunc(ctx, func(ctx context.Context) (net.Conn, error) {
		return dialer.DialContext(ctx, network, address)
	}, schema, opts)
}

// DialFunc opens a connection with dial and returns a [Client].
// If the peer rejects compression, it redials without compression.
func DialFunc(ctx context.Context, dial func(context.Context) (net.Conn, error), schema *Schema, opts ClientOptions) (*Client, error) {
	if dial == nil {
		return nil, fmt.Errorf("plug: dial function is nil")
	}

	conn, err := dial(ctx)
	if err != nil {
		return nil, err
	}
	c, err := NewClient(ctx, conn, schema, opts)
	if !opts.Compression || !errors.Is(err, errCompressionUnsupported) {
		return c, err
	}

	conn, err = dial(ctx)
	if err != nil {
		return nil, err
	}
	opts.Compression = false
	return NewClient(ctx, conn, schema, opts)
}

func (c *Client) call(ctx context.Context, methodID uint32, enc *plugimpl.Encoder) (frame, error) {
	id := c.nextID.Add(1)
	if id == 0 {
		id = c.nextID.Add(1)
	}
	pending := c.pendingPool.Get().(*pendingCall)
	if err := c.addPending(id, pending); err != nil {
		c.pendingPool.Put(pending)
		plugimpl.ReleaseEncoder(enc)
		return frame{}, err
	}
	if err := c.sendRequest(ctx, id, methodID, enc); err != nil {
		if c.removePending(id) {
			c.pendingPool.Put(pending)
		}
		return frame{}, err
	}

	select {
	case value := <-pending.response:
		c.pendingPool.Put(pending)
		return value, nil
	case <-ctx.Done():
		if c.removePending(id) {
			c.pendingPool.Put(pending)
			c.sendCancel(id)
		}
		return frame{}, ctx.Err()
	case <-c.done:
		if c.removePending(id) {
			c.pendingPool.Put(pending)
		}
		return frame{}, c.closeErr
	}
}

// Err returns the error that closed the connection, or nil while it is open.
func (c *Client) Err() error {
	select {
	case <-c.done:
		return c.closeErr
	default:
		return nil
	}
}

// Close closes the connection and fails pending calls.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	return c.shutdown(ErrClientClosed)
}

func (c *Client) readLoop() {
	defer c.framer.close()

	for {
		value, err := c.framer.readFrame()
		if err != nil {
			c.shutdown(err)
			return
		}
		if value.requestID == 0 {
			c.shutdown(fmt.Errorf("plug: response uses reserved request ID zero"))
			return
		}
		switch value.kind {
		case frameResponse, frameApplicationError, frameProtocolError:
		default:
			c.shutdown(fmt.Errorf("plug: server sent invalid frame kind %d", value.kind))
			return
		}

		shard := &c.shards[value.requestID%numPendingShards]
		shard.mu.Lock()
		pending, ok := shard.m[value.requestID]
		if ok {
			delete(shard.m, value.requestID)
		}
		shard.mu.Unlock()

		if ok {
			pending.response <- value
		}
	}
}

func (c *Client) addPending(id uint64, pending *pendingCall) error {
	select {
	case <-c.done:
		return c.closeErr
	default:
	}

	shard := &c.shards[id%numPendingShards]
	shard.mu.Lock()
	defer shard.mu.Unlock()

	select {
	case <-c.done:
		return c.closeErr
	default:
	}

	shard.m[id] = pending
	return nil
}

func (c *Client) removePending(id uint64) bool {
	shard := &c.shards[id%numPendingShards]
	shard.mu.Lock()
	defer shard.mu.Unlock()
	if _, ok := shard.m[id]; !ok {
		return false
	}
	delete(shard.m, id)
	return true
}

// sendRequest transfers enc to the writer or releases it if the call ends first.
func (c *Client) sendRequest(ctx context.Context, id uint64, method uint32, enc *plugimpl.Encoder) error {
	select {
	case c.writes <- clientRequest{
		id:     id,
		method: method,
		enc:    enc,
	}:
		return nil
	case <-ctx.Done():
		plugimpl.ReleaseEncoder(enc)
		return ctx.Err()
	case <-c.done:
		plugimpl.ReleaseEncoder(enc)
		return c.closeErr
	}
}

func (c *Client) sendCancel(id uint64) {
	c.cancelMu.Lock()
	defer c.cancelMu.Unlock()

	select {
	case <-c.done:
		return
	default:
	}
	c.cancels = append(c.cancels, id)

	select {
	case c.cancelReady <- struct{}{}:
	default:
	}
}

func (c *Client) writeLoop() {
	for {
		var err error
		select {
		case <-c.done:
			return
		case req := <-c.writes:
			err = c.framer.writeRequest(req.id, req.method, req.enc.Data())
			plugimpl.ReleaseEncoder(req.enc)
		case <-c.cancelReady:
			err = c.writeCancels()
		}
		if err != nil {
			c.shutdown(err)
			return
		}
	}
}

func (c *Client) writeCancels() error {
	c.cancelMu.Lock()
	ids := c.cancels
	c.cancels = nil
	c.cancelMu.Unlock()

	for _, id := range ids {
		if err := c.framer.writeCancel(id); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) shutdown(reason error) error {
	var closeResult error
	c.closeOnce.Do(func() {
		c.closeErr = reason
		closeResult = c.conn.Close()
		c.cancelMu.Lock()
		close(c.done)
		c.cancels = nil
		c.cancelMu.Unlock()
	})
	return closeResult
}

// Call invokes method with req.
// Canceling ctx stops waiting for a response
// and requests cancellation on the server.
func (c *Client) Call[Request, Response any](
	ctx context.Context,
	method plugimpl.Method[Request, Response],
	req Request,
) (Response, error) {
	var zero Response

	if c == nil {
		return zero, fmt.Errorf("plug: client is nil")
	}
	if err := method.Validate(c.schema); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}

	maxList := c.limits.MaxListElements
	enc, err := plugimpl.EncodePooled(method.Request(), req, maxList)
	if err != nil {
		return zero, fmt.Errorf("plug: encode request for %s: %w", method.Info().Name, err)
	}

	result, err := c.call(ctx, method.ID(), enc)
	if err != nil {
		return zero, err
	}

	response, err, protocolError := decodeMethodResult(method, result, c.limits)
	if protocolError {
		c.shutdown(err)
	}
	return response, err
}

func decodeMethodResult[Request, Response any](
	method plugimpl.Method[Request, Response],
	result frame,
	limits Limits,
) (Response, error, bool) {
	var zero Response
	if result.kind == frameResponse {
		decoded, err := plugimpl.Decode(method.Response(), result.payload, limits.MaxListElements, limits.MaxDecodeBytes)
		if err != nil {
			return zero, fmt.Errorf("plug: decode response for %s: %w", method.Info().Name, err), true
		}
		return decoded, nil, false
	}
	err, protocolError := decodeResultError(method.Info(), method.Errors(), result, limits, "response")
	return zero, err, protocolError
}

func decodeResultError(
	method MethodInfo,
	errorsCodec plugimpl.ErrorCodec,
	result frame,
	limits Limits,
	responseName string,
) (error, bool) {
	switch result.kind {
	case frameApplicationError:
		if errorsCodec.Decode == nil {
			return fmt.Errorf("plug: method %s returned an undeclared application error", method.Name), true
		}
		appErr, err := plugimpl.DecodeError(errorsCodec, result.payload, result.code, limits.MaxListElements, limits.MaxDecodeBytes)
		if err != nil {
			return fmt.Errorf("plug: decode application error for %s: %w", method.Name, err), true
		}
		if appErr == nil {
			return fmt.Errorf("plug: application error decoder returned nil"), true
		}
		return appErr, false
	case frameProtocolError:
		if !utf8.Valid(result.payload) {
			return fmt.Errorf("plug: protocol error message is not UTF-8"), true
		}
		errCode := ErrorCode(result.code)
		if errCode < ErrorBadRequest || errCode > ErrorUnimplemented {
			return fmt.Errorf("plug: unknown core error code %d", errCode), true
		}
		return &RemoteError{
			Code:    errCode,
			Message: string(result.payload),
		}, false
	default:
		return fmt.Errorf("plug: unexpected %s result kind %d", responseName, result.kind), true
	}
}
