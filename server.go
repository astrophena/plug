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
	"maps"
	"net"
	"slices"
	"strings"
	"sync"

	"go.astrophena.name/plug/plugimpl"
)

// Server serves the methods in a schema. Use [NewServer] to create a server.
type Server struct {
	schema       *Schema
	limits       Limits
	compression  bool
	strictSchema bool
	security     Security
	observeCall  CallObserver
	reportError  ErrorReporter

	handlersMu sync.Mutex
	handlers   map[uint32]handlerEntry
	sealed     bool

	shutdownMu   sync.Mutex
	shuttingDown bool
	shutdownDone chan struct{}
	serverCtx    context.Context
	cancelCalls  context.CancelFunc
	activeCalls  sync.WaitGroup
}

type handlerEntry struct {
	info      MethodInfo
	invoke    func(context.Context, []byte) callResult
	invokeRaw func(context.Context, []byte, *RawStream) callResult
}

type callResult struct {
	kind       frameKind
	code       uint32
	payload    []byte
	enc        *plugimpl.Encoder
	fatal      bool
	observeCtx context.Context
	event      CallEvent
}

func (r *callResult) release() {
	if r.enc != nil {
		plugimpl.ReleaseEncoder(r.enc)
		r.enc = nil
		r.payload = nil
	}
}

func (r *callResult) observe(s *Server, stats payloadStats) {
	if r.observeCtx == nil {
		return
	}
	r.event.PayloadSize = stats.size
	r.event.Compressed = stats.compressed
	r.event.CompressedSize = stats.compressedSize
	if stats.compressed {
		r.event.CompressionSavings = stats.size - stats.compressedSize
	}
	s.safeObserveCall(r.observeCtx, r.event)
	r.observeCtx = nil
}

// NewServer returns a server bound to a copy of schema.
func NewServer(schema *Schema, opts ServerOptions) (*Server, error) {
	schema, limits, err := normalizeSchemaLimits(schema, opts.Limits)
	if err != nil {
		return nil, err
	}

	serverCtx, cancelCalls := context.WithCancel(context.Background())
	return &Server{
		schema:       schema,
		limits:       limits,
		compression:  opts.Compression,
		strictSchema: opts.StrictSchema,
		security:     opts.Security,
		observeCall:  opts.ObserveCall,
		reportError:  opts.ReportError,
		handlers:     make(map[uint32]handlerEntry),
		serverCtx:    serverCtx,
		cancelCalls:  cancelCalls,
		shutdownDone: make(chan struct{}),
	}, nil
}

// Schema returns an independent copy of the server's validated schema.
func (s *Server) Schema() *Schema { return plugimpl.CopySchema(s.schema) }

// Limits returns the server's resource limits.
func (s *Server) Limits() Limits { return s.limits }

// Shutdown rejects new calls, cancels active calls, and waits for them to finish
// or ctx to expire. It applies to native connections and [Invoke] calls.
// Subsequent calls wait for the same shutdown to finish.
func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("plug: server is nil")
	}

	s.shutdownMu.Lock()
	if !s.shuttingDown {
		s.shuttingDown = true
		s.cancelCalls()
		go func() {
			s.activeCalls.Wait()
			close(s.shutdownDone)
		}()
	}
	s.shutdownMu.Unlock()

	select {
	case <-s.shutdownDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// trackConnectionCall registers work that must complete before [Shutdown] returns.
func (s *Server) trackConnectionCall(parentCtx context.Context) (context.Context, context.CancelFunc, error) {
	// serveConn links parentCtx to server shutdown once for the whole connection.
	if err := s.addActiveCall(); err != nil {
		return nil, nil, err
	}

	callCtx, cancel := context.WithCancel(parentCtx)
	return callCtx, cancel, nil
}

func (s *Server) finishConnectionCall(cancel context.CancelFunc) {
	cancel()
	s.activeCalls.Done()
}

func (s *Server) addActiveCall() error {
	s.shutdownMu.Lock()
	defer s.shutdownMu.Unlock()

	if s.shuttingDown {
		return fmt.Errorf("plug: server is shutting down")
	}

	s.activeCalls.Add(1)
	return nil
}

// RegisterHandlers validates and registers a batch atomically.
// Registration closes when serving starts or [Server.Seal] succeeds.
func (s *Server) RegisterHandlers(registrations []plugimpl.Registration) error {
	if s == nil {
		return fmt.Errorf("plug: server is nil")
	}

	s.handlersMu.Lock()
	defer s.handlersMu.Unlock()

	if s.sealed {
		return fmt.Errorf("plug: cannot register handlers after serving started")
	}

	batch := make(map[uint32]handlerEntry, len(registrations))
	for _, reg := range registrations {
		if err := plugimpl.ValidateMethod(reg.Schema, s.schema, reg.ID, reg.Raw); err != nil {
			return err
		}
		info, _ := plugimpl.LookupMethod(s.schema, reg.ID)
		if reg.Decode == nil {
			return fmt.Errorf("plug: request decoder for method %s (0x%08x) is nil", info.Name, reg.ID)
		}
		if reg.Raw {
			if reg.HandleRaw == nil {
				return fmt.Errorf("plug: raw handler for method %s (0x%08x) is nil", info.Name, reg.ID)
			}
			if reg.Handle != nil || reg.Encode != nil {
				return fmt.Errorf("plug: raw method %s (0x%08x) has framed callbacks", info.Name, reg.ID)
			}
		} else {
			if reg.Handle == nil {
				return fmt.Errorf("plug: handler for method %s (0x%08x) is nil", info.Name, reg.ID)
			}
			if reg.Encode == nil {
				return fmt.Errorf("plug: response encoder for method %s (0x%08x) is nil", info.Name, reg.ID)
			}
			if reg.HandleRaw != nil {
				return fmt.Errorf("plug: method %s (0x%08x) has a raw handler", info.Name, reg.ID)
			}
		}
		if prev, ok := s.handlers[reg.ID]; ok {
			return fmt.Errorf("plug: method %s already has handler %s", info.Name, prev.info.Name)
		}
		if prev, ok := batch[reg.ID]; ok {
			return fmt.Errorf("plug: duplicate handler registration in batch for method %s (already in batch as %s)", info.Name, prev.info.Name)
		}

		entry := handlerEntry{info: info}
		if reg.Raw {
			entry.invokeRaw = newRawInvoke(s, info, reg)
		} else {
			entry.invoke = newInvoke(s, info, reg)
		}
		batch[reg.ID] = entry
	}

	maps.Copy(s.handlers, batch)
	return nil
}

func newInvoke(
	s *Server,
	info MethodInfo,
	reg plugimpl.Registration,
) func(context.Context, []byte) callResult {
	return func(ctx context.Context, payload []byte) callResult {
		call, request, result, ok := s.beginCall(ctx, info, reg.Decode, payload)
		if !ok {
			return result
		}

		var response any
		duration, callErr, panicErr := runHandler(func() error {
			var err error
			response, err = reg.Handle(ctx, request)
			return err
		})
		if panicErr != nil {
			return call.internal(StageHandlerPanic, panicErr, duration, "internal server error")
		}
		if callErr != nil {
			if result, ok := call.applicationError(reg.Errors, callErr, duration); ok {
				return result
			}
			return call.internal(StageHandlerPanic, callErr, duration, "internal server error")
		}

		enc, err := reg.Encode(response, s.limits.MaxListElements)
		if err != nil {
			return call.internal(
				StageEncodeResponse,
				err,
				duration,
				fmt.Sprintf("encode %s response: %v", info.Name, err),
			)
		}
		return call.succeed(callResult{
			kind:    frameResponse,
			payload: enc.Data(),
			enc:     enc,
		}, duration)
	}
}

func (s *Server) safeObserveCall(ctx context.Context, event CallEvent) {
	if s.observeCall == nil {
		return
	}
	ignorePanic(func() { s.observeCall(ctx, event) })
}

func (s *Server) safeReportError(ctx context.Context, event ErrorEvent) {
	if s.reportError == nil {
		return
	}
	ignorePanic(func() { s.reportError(ctx, event) })
}

func ignorePanic(run func()) {
	defer func() { recover() }()
	run()
}

// Serve accepts connections from listener until ctx is canceled or accepting
// fails. Server takes ownership of listener and every accepted connection.
func (s *Server) Serve(ctx context.Context, listener net.Listener) (err error) {
	if s == nil {
		return fmt.Errorf("plug: server is nil")
	}
	if listener == nil {
		return fmt.Errorf("plug: listener is nil")
	}
	if err := s.Seal(); err != nil {
		return errors.Join(err, listener.Close())
	}
	serveCtx, cancelServe := context.WithCancel(ctx)

	var (
		connections       sync.WaitGroup
		closeListenerOnce sync.Once
		closeListenerErr  error
	)

	closeListener := func() {
		closeListenerOnce.Do(func() { closeListenerErr = listener.Close() })
	}
	stop := context.AfterFunc(serveCtx, closeListener)
	defer func() {
		cancelServe()
		stop()
		closeListener()
		err = errors.Join(err, closeListenerErr)
		connections.Wait()
	}()

	for {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil || errors.Is(acceptErr, net.ErrClosed) {
				return nil
			}
			return acceptErr
		}
		connections.Go(func() { s.serveConn(serveCtx, conn) })
	}
}

// ServeConn performs the handshake and serves the connection until it
// closes or ctx is canceled.
func (s *Server) ServeConn(ctx context.Context, conn net.Conn) error {
	if s == nil {
		return fmt.Errorf("plug: server is nil")
	}
	if conn == nil {
		return fmt.Errorf("plug: connection is nil")
	}
	if err := s.Seal(); err != nil {
		return errors.Join(err, conn.Close())
	}
	return s.serveConn(ctx, conn)
}

// Seal checks for missing handlers and authorizers, then closes registration.
// It is idempotent and called by [Server.Serve], [Server.ServeConn], and HTTP adapters.
// If it fails, registration remains open.
func (s *Server) Seal() error {
	if s == nil {
		return fmt.Errorf("plug: server is nil")
	}

	s.handlersMu.Lock()
	defer s.handlersMu.Unlock()

	if s.sealed {
		return nil
	}

	var missing []string
	for _, method := range s.schema.Methods {
		if _, ok := s.handlers[method.ID]; !ok {
			missing = append(missing, method.Name)
		}
	}
	slices.Sort(missing)
	if len(missing) > 0 {
		return fmt.Errorf("plug: missing handlers for %s", strings.Join(missing, ", "))
	}

	for _, method := range s.schema.Methods {
		if !method.Public && s.security.Authorize == nil {
			return fmt.Errorf("plug: method %s requires permission %q but no authorizer is configured", method.Name, method.Permission)
		}
	}

	s.sealed = true
	return nil
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) (err error) {
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer func() {
		stop()
		if ctx.Err() != nil {
			err = nil
		}
	}()

	ctx, principal, compression, serve, err := s.acceptClient(ctx, conn)
	if err != nil || !serve {
		return errors.Join(err, conn.Close())
	}

	return s.serveFrames(ctx, conn, principal, compression)
}

func (s *Server) acceptClient(ctx context.Context, conn net.Conn) (context.Context, Principal, bool, bool, error) {
	var principal Principal
	if s.security.Authenticate != nil {
		var err error
		principal, err = s.security.Authenticate(ctx, conn)
		if err != nil {
			s.safeReportError(ctx, ErrorEvent{
				Stage: StageAuthenticate,
				Err:   err,
			})
			return ctx, Principal{}, false, false, err
		}
		ctx = WithPrincipal(ctx, principal)
	}

	hello, err := readClientHello(conn)
	if err != nil {
		if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) && ctx.Err() == nil {
			s.safeReportError(ctx, ErrorEvent{
				Stage: StageHandshake,
				Err:   err,
			})
		}
		return ctx, principal, false, false, err
	}
	if hello.version != wireVersion {
		s.safeReportError(ctx, ErrorEvent{
			Stage: StageHandshake,
			Err:   fmt.Errorf("unsupported client wire version %d", hello.version),
		})
		return ctx, principal, false, false, writeServerHello(conn, handshakeUnsupportedVersion, s.schema, true)
	}
	if hello.mode == handshakeInspect {
		return ctx, principal, false, false, writeServerHello(conn, handshakeOK, s.schema, true)
	}
	if hello.family != handshakeHash(s.schema.Family) {
		s.safeReportError(ctx, ErrorEvent{
			Stage: StageHandshake,
			Err:   fmt.Errorf("schema family mismatch"),
		})
		return ctx, principal, false, false, writeServerHello(conn, handshakeSchemaMismatch, s.schema, true)
	}
	if (hello.strict || s.strictSchema) && hello.fingerprint != handshakeHash(s.schema.Fingerprint) {
		s.safeReportError(ctx, ErrorEvent{
			Stage: StageHandshake,
			Err:   fmt.Errorf("strict schema fingerprint mismatch"),
		})
		return ctx, principal, false, false, writeServerHello(conn, handshakeSchemaMismatch, s.schema, true)
	}
	if hello.compression && !s.compression {
		s.safeReportError(ctx, ErrorEvent{
			Stage: StageHandshake,
			Err:   fmt.Errorf("zstd compression is unsupported"),
		})
		return ctx, principal, false, false, writeServerHello(conn, handshakeUnsupportedCompression, s.schema, true)
	}
	if err := writeServerHello(conn, handshakeOK, s.schema, false); err != nil {
		s.safeReportError(ctx, ErrorEvent{
			Stage: StageHandshake,
			Err:   err,
		})
		return ctx, principal, false, false, err
	}
	return ctx, principal, hello.compression, true, nil
}

func (s *Server) serveFrames(ctx context.Context, conn net.Conn, principal Principal, compression bool) error {
	callCtx, cancelCalls := context.WithCancel(ctx)
	stopShutdown := context.AfterFunc(s.serverCtx, cancelCalls)
	defer func() {
		stopShutdown()
		cancelCalls()
	}()

	r := bufio.NewReaderSize(conn, 32*1024)
	f := newFramer(conn, r, s.limits, compression)
	calls := newActiveCalls()

	var (
		serveErr error
		requests uint64
		rawDone  bool
	)

	for {
		fr, err := f.readFrame()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) && ctx.Err() == nil {
				s.safeReportError(ctx, ErrorEvent{
					Stage: StageReadFrame,
					Err:   err,
				})
			}
			serveErr = err
			break
		}
		if fr.requestID == 0 {
			s.safeReportError(ctx, ErrorEvent{
				Stage: StageValidateFrame,
				Err:   fmt.Errorf("request uses reserved ID zero"),
			})
			serveErr = rejectFrame(f, fr.requestID, "request uses reserved ID zero")
			break
		}

		switch fr.kind {
		case frameCancel:
			if len(fr.payload) != 0 {
				s.safeReportError(ctx, ErrorEvent{
					Stage: StageValidateFrame,
					Err:   fmt.Errorf("cancel frame has a payload"),
				})
				serveErr = rejectFrame(f, fr.requestID, "cancel frame has a payload")
				break
			}
			calls.cancel(fr.requestID)
			continue
		case frameRequest:
			requests++
		default:
			s.safeReportError(ctx, ErrorEvent{
				Stage: StageValidateFrame,
				Err:   fmt.Errorf("client sent invalid frame kind %d", fr.kind),
			})
			serveErr = rejectFrame(f, fr.requestID, fmt.Sprintf("client sent invalid frame kind %d", fr.kind))
		}
		if serveErr != nil {
			break
		}

		if calls.count() >= s.limits.MaxConcurrentCalls {
			if err := f.writeProtocolError(fr.requestID, ErrorInternal, "maximum concurrent calls reached"); err != nil {
				serveErr = err
				break
			}
			continue
		}

		entry, ok := s.handlers[fr.code]
		if !ok {
			info, _ := plugimpl.LookupMethod(s.schema, fr.code)
			s.safeReportError(ctx, ErrorEvent{
				Stage:     StageUnknownMethod,
				Method:    info,
				Principal: principal,
				Err:       fmt.Errorf("unknown method 0x%08x", fr.code),
			})
			if err := f.writeProtocolError(fr.requestID, ErrorUnimplemented, fmt.Sprintf("unknown method 0x%08x", fr.code)); err != nil {
				serveErr = err
				break
			}
			continue
		}
		if entry.invokeRaw != nil {
			if requests != 1 || calls.count() != 0 {
				s.safeReportError(ctx, ErrorEvent{
					Stage:     StageValidateFrame,
					Method:    entry.info,
					Principal: principal,
					Err:       fmt.Errorf("raw method must be the first request on a dedicated connection"),
				})
				serveErr = rejectFrame(f, fr.requestID, "raw method must be the first request on a dedicated connection")
				break
			}
			serveErr = s.serveRawCall(callCtx, conn, r, f, fr, entry)
			rawDone = serveErr == nil
			break
		}

		requestCtx, cancel, trackErr := s.trackConnectionCall(callCtx)
		if trackErr != nil {
			if err := f.writeProtocolError(fr.requestID, ErrorInternal, "server is shutting down"); err != nil {
				serveErr = err
				break
			}
			continue
		}

		call := &activeCall{
			cancel: cancel,
		}
		if !calls.add(fr.requestID, call) {
			s.finishConnectionCall(cancel)
			s.safeReportError(ctx, ErrorEvent{
				Stage: StageValidateFrame,
				Err:   fmt.Errorf("duplicate active request ID"),
			})
			serveErr = rejectFrame(f, fr.requestID, "duplicate active request ID")
			break
		}

		go func(ctx context.Context, id uint64, payload []byte, invoke func(context.Context, []byte) callResult) {
			defer s.activeCalls.Done()
			defer calls.done()
			result := invokeSafely(func() callResult {
				return invoke(ctx, payload)
			})
			defer result.release()

			if !calls.finish(id, call) {
				result.observe(s, payloadStats{
					size: len(result.payload),
				})
				return
			}
			stats, err := f.writeResult(id, result.kind, result.code, result.payload)
			result.observe(s, stats)
			if err != nil {
				s.safeReportError(ctx, ErrorEvent{
					Stage: StageWriteFrame,
					Err:   err,
				})
			}
			if err != nil || result.fatal {
				conn.Close()
			}
		}(requestCtx, fr.requestID, fr.payload, entry.invoke)
	}

	calls.cancelAll()
	closeErr := conn.Close()
	if rawDone {
		return nil
	}
	if ctx.Err() != nil || errors.Is(serveErr, io.EOF) || errors.Is(serveErr, net.ErrClosed) {
		return nil
	}
	return errors.Join(serveErr, closeErr)
}

func invokeSafely(invoke func() callResult) (result callResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = coreResult(ErrorInternal, "internal server error", false)
		}
	}()
	return invoke()
}

func coreResult(code ErrorCode, message string, fatal bool) callResult {
	return callResult{
		kind:    frameProtocolError,
		code:    uint32(code),
		payload: []byte(strings.ToValidUTF8(message, "")),
		fatal:   fatal,
	}
}

func rejectFrame(frames *framer, requestID uint64, message string) error {
	if err := frames.writeProtocolError(requestID, ErrorBadRequest, message); err != nil {
		return errors.Join(fmt.Errorf("plug: %s", message), err)
	}
	return fmt.Errorf("plug: %s", message)
}

type activeCalls struct {
	mu      sync.Mutex
	pending map[uint64]*activeCall
	running uint32
}

type activeCall struct {
	cancel context.CancelFunc
}

func newActiveCalls() *activeCalls {
	return &activeCalls{
		pending: make(map[uint64]*activeCall),
	}
}

func (a *activeCalls) count() uint32 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running
}

func (a *activeCalls) add(id uint64, call *activeCall) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.pending[id]; ok {
		return false
	}
	a.pending[id] = call
	a.running++
	return true
}

// cancel suppresses the response, but the call keeps its capacity until done.
func (a *activeCalls) cancel(id uint64) {
	a.mu.Lock()
	call := a.pending[id]
	delete(a.pending, id)
	a.mu.Unlock()
	if call != nil {
		call.cancel()
	}
}

func (a *activeCalls) finish(id uint64, call *activeCall) bool {
	a.mu.Lock()
	current := a.pending[id] == call
	if current {
		delete(a.pending, id)
	}
	a.mu.Unlock()
	call.cancel()
	return current
}

func (a *activeCalls) done() {
	a.mu.Lock()
	a.running--
	a.mu.Unlock()
}

func (a *activeCalls) cancelAll() {
	a.mu.Lock()
	calls := a.pending
	a.pending = nil
	a.mu.Unlock()

	for _, call := range calls {
		call.cancel()
	}
}
