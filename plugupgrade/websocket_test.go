// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plugupgrade

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/coder/websocket"
)

func TestKeepAlive(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"pong", "timeout", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				var pings atomic.Int64
				ws, peer := wsPair(t, &websocket.AcceptOptions{
					OnPingReceived: func(context.Context, []byte) bool {
						pings.Add(1)
						return mode == "pong"
					},
				})
				defer ws.CloseNow()
				defer peer.CloseNow()
				readCtx := ws.CloseRead(ctx)
				peer.CloseRead(ctx)
				done := make(chan struct{})
				go func() {
					defer close(done)
					keepAlive(ctx, ws)
				}()

				synctest.Sleep(pingInterval - time.Nanosecond)
				if pings.Load() != 0 {
					t.Fatal("ping sent before its interval")
				}
				synctest.Sleep(time.Nanosecond)
				if got := pings.Load(); got != 1 {
					t.Fatalf("pings = %d; want 1", got)
				}

				switch mode {
				case "pong":
					synctest.Sleep(pingInterval)
					if got := pings.Load(); got != 2 {
						t.Fatalf("pings = %d; want 2", got)
					}
					cancel()
				case "cancel":
					cancel()
				case "timeout":
					synctest.Sleep(pingTimeout - time.Nanosecond)
					select {
					case <-done:
						t.Fatal("connection closed before the pong timeout")
					default:
					}
					synctest.Sleep(time.Nanosecond)
				}
				synctest.Wait()
				select {
				case <-done:
				default:
					t.Fatal("keep-alive did not stop")
				}
				if readCtx.Err() == nil {
					t.Fatal("keep-alive did not close the connection")
				}
			})
		})
	}
}

func wsPair(t *testing.T, opts *websocket.AcceptOptions) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() {
		server.Close()
		client.Close()
	})
	accepted := make(chan *websocket.Conn, 1)
	go func() {
		defer close(accepted)
		rw := bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server))
		req, err := http.ReadRequest(rw.Reader)
		if err != nil {
			t.Error(err)
			return
		}
		ws, err := websocket.Accept(&pipeResponse{
			ResponseRecorder: httptest.NewRecorder(),
			conn:             server,
			rw:               rw,
		}, req, opts)
		if err != nil {
			t.Error(err)
			return
		}
		accepted <- ws
	}()

	tr := &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) { return client, nil },
	}
	defer tr.CloseIdleConnections()
	ws, _, err := websocket.Dial(t.Context(), "ws://example.test", &websocket.DialOptions{
		HTTPClient: &http.Client{
			Transport: tr,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	peer := <-accepted
	if peer == nil {
		ws.CloseNow()
		t.Fatal("WebSocket upgrade failed")
	}
	return ws, peer
}

type pipeResponse struct {
	*httptest.ResponseRecorder
	conn net.Conn
	rw   *bufio.ReadWriter
}

func (w *pipeResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	// An unknown body length would add Connection: close.
	resp := w.Result()
	resp.Body = http.NoBody
	resp.ContentLength = 0
	if err := resp.Write(w.rw); err != nil {
		return nil, nil, err
	}
	if err := w.rw.Flush(); err != nil {
		return nil, nil, err
	}
	return w.conn, w.rw, nil
}

func TestWebSocketConnClose(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		<-conn.CloseRead(r.Context()).Done()
	}))
	defer server.Close()
	conn, err := new(Dialer).DialContext(t.Context(), strings.Replace(server.URL, "http", "ws", 1))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
