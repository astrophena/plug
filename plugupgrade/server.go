// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package plugupgrade carries Plug connections over HTTP/1.1 Upgrade or
// WebSocket. A reverse proxy may terminate TLS and authenticate the request.
//
// WebSocket peers send a ping every 30 seconds and close the connection if no
// pong arrives within 10 seconds. Raw stream users must keep reading to handle
// control frames.
package plugupgrade

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.astrophena.name/plug"

	"github.com/coder/websocket"
)

const protocolName = "plug"

// Options configures an HTTP upgrade server.
type Options struct {
	// Authenticate derives a principal before upgrading the request.
	// If nil, the request's context supplies the principal.
	Authenticate func(*http.Request) (plug.Principal, error)

	// WriteError optionally writes a custom error response when authentication
	// fails. When nil, http.Error is used.
	WriteError func(http.ResponseWriter, *http.Request, error)
}

// Server serves Plug over native and WebSocket upgrades on the same endpoint.
// WebSocket upgrades reject cross-origin browser requests.
type Server struct {
	server  *plug.Server
	options Options

	mu      sync.Mutex
	conns   map[net.Conn]struct{}
	closing bool
	wait    sync.WaitGroup
}

// IsActive reports whether upgraded Plug connections are open.
func (s *Server) IsActive() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns) != 0
}

// NewServer returns an HTTP upgrade adapter for server.
func NewServer(server *plug.Server, options Options) (*Server, error) {
	if server == nil {
		return nil, errors.New("plugupgrade: Plug server is nil")
	}
	if err := server.Seal(); err != nil {
		return nil, err
	}
	return &Server{
		server:  server,
		options: options,
		conns:   make(map[net.Conn]struct{}),
	}, nil
}

// ServeHTTP validates and upgrades an HTTP/1.1 request, then serves Plug until
// the peer disconnects or the request context is canceled.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	protocol := strings.TrimSpace(r.Header.Get("Upgrade"))
	if r.ProtoMajor != 1 || r.ProtoMinor < 1 ||
		!headerHasToken(r.Header, "Connection", "upgrade") ||
		(!strings.EqualFold(protocol, protocolName) && !strings.EqualFold(protocol, "websocket")) {
		w.Header().Set("Connection", "Upgrade")
		w.Header().Set("Upgrade", protocolName+", websocket")
		http.Error(w, http.StatusText(http.StatusUpgradeRequired), http.StatusUpgradeRequired)
		return
	}

	s.mu.Lock()
	closing := s.closing
	s.mu.Unlock()
	if closing {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()
	if s.options.Authenticate != nil {
		principal, err := s.options.Authenticate(r)
		if err != nil {
			if s.options.WriteError != nil {
				s.options.WriteError(w, r, err)
			} else {
				http.Error(w, "unauthenticated", http.StatusUnauthorized)
			}
			return
		}
		ctx = plug.WithPrincipal(ctx, principal)
	}

	var (
		conn net.Conn
		bw   *bufio.Writer
	)
	if strings.EqualFold(protocol, "websocket") {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conn = newWSConn(ctx, ws)
	} else {
		conn, bw = hijack(w)
		if conn == nil {
			return
		}
	}

	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		conn.Close()
		return
	}
	s.conns[conn] = struct{}{}
	s.wait.Add(1)
	s.mu.Unlock()
	defer func() {
		conn.Close()
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		s.wait.Done()
	}()

	if bw != nil {
		bw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: " + protocolName + "\r\n\r\n")
		if err := bw.Flush(); err != nil {
			return
		}
	}
	s.server.ServeConn(ctx, conn)
}

func hijack(w http.ResponseWriter) (net.Conn, *bufio.Writer) {
	h, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "connection cannot be hijacked", http.StatusInternalServerError)
		return nil, nil
	}
	conn, rw, err := h.Hijack()
	if err != nil {
		return nil, nil
	}
	c := &bufferedConn{
		Conn:   conn,
		reader: rw.Reader,
	}
	if err := c.SetDeadline(time.Time{}); err != nil {
		c.Close()
		return nil, nil
	}
	return c, rw.Writer
}

// Shutdown refuses new upgrades, closes active connections, and waits for their
// handlers to return or ctx to expire. A Server cannot be reused afterward.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	conns := make([]net.Conn, 0, len(s.conns))
	for conn := range s.conns {
		conns = append(conns, conn)
	}
	s.mu.Unlock()

	var closeErr error
	if err := s.server.Shutdown(ctx); err != nil {
		closeErr = errors.Join(closeErr, err)
	}
	for _, conn := range conns {
		if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			closeErr = errors.Join(closeErr, err)
		}
	}
	done := make(chan struct{})
	go func() {
		s.wait.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return errors.Join(closeErr, ctx.Err())
	case <-done:
		return closeErr
	}
}

func headerHasToken(header http.Header, name, token string) bool {
	for _, value := range header.Values(name) {
		for part := range strings.SplitSeq(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}
