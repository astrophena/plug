// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plugupgrade

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/plug"

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
				testutil.AssertEqual(t, pings.Load(), int64(0))
				synctest.Sleep(time.Nanosecond)
				testutil.AssertEqual(t, pings.Load(), int64(1))

				switch mode {
				case "pong":
					synctest.Sleep(pingInterval)
					testutil.AssertEqual(t, pings.Load(), int64(2))
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

func TestWebSocketClose(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		<-conn.CloseRead(r.Context()).Done()
	}))
	defer srv.Close()
	conn, err := new(Dialer).DialContext(t.Context(), strings.Replace(srv.URL, "http", "ws", 1))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWebSocketAuth(t *testing.T) {
	t.Parallel()
	var (
		msg = strings.Repeat("hello", 32<<10)
		sch = testSchema()
	)
	sch.Permissions = []plug.PermissionInfo{
		{
			Name: "echo",
		},
	}
	sch.Methods[0].Public = false
	sch.Methods[0].Permission = "echo"
	srv, err := plug.NewServer(sch, plug.ServerOptions{
		Security: plug.Security{
			Authorize: func(_ context.Context, p plug.Principal, _ string, _ plug.MethodInfo, _ any) error {
				if p.Subject != "test" {
					return errors.New("unexpected principal")
				}
				return nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Shutdown(context.Background()) })
	registerEcho(t, srv)
	upgrade, err := NewServer(srv, Options{
		Authenticate: func(r *http.Request) (plug.Principal, error) {
			if r.Header.Get("Authorization") != "Bearer test" || r.URL.Path != "/plug" || r.URL.Query().Get("profile") != "test" {
				return plug.Principal{}, errors.New("denied")
			}
			return plug.Principal{
				Subject: "test",
			}, nil
		},
		WriteError: func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, err.Error(), http.StatusForbidden)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	web := httptest.NewServer(upgrade)
	defer web.Close()
	addr := strings.Replace(web.URL, "http", "ws", 1) + "/plug?profile=test"
	if _, err := new(Dialer).DialContext(t.Context(), addr); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("unauthenticated DialContext() = %v; want denied", err)
	}
	d := &Dialer{
		Header: http.Header{"Authorization": {"Bearer test"}},
	}
	c := dialClient(t, d, addr, plug.ClientOptions{})

	got, err := c.Call(t.Context(), echoMethod(testSchema()), msg)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, got, msg)
}

func TestWebSocketRejected(t *testing.T) {
	t.Parallel()
	upgrade := newUpgrade(t, service(t))
	srv := httptest.NewServer(upgrade)
	defer srv.Close()
	addr := strings.Replace(srv.URL, "http", "ws", 1) + "/plug"
	d := &Dialer{
		Header: http.Header{"Origin": {"https://other.example"}},
	}
	if _, err := d.DialContext(t.Context(), addr); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("cross-origin DialContext() = %v; want 403", err)
	}
	conn, _, err := websocket.Dial(t.Context(), addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte("PLUG")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.Read(ctx); websocket.CloseStatus(err) != websocket.StatusUnsupportedData {
		t.Fatalf("Read() = %v; want unsupported data", err)
	}
}

func TestWebSocketPreface(t *testing.T) {
	t.Parallel()
	upgrade := newUpgrade(t, service(t))
	srv := httptest.NewServer(upgrade)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, srv.URL+"/plug", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	for _, b := range []byte{'P', 'L', 'U', 'G', 1, 1} {
		if err := conn.Write(ctx, websocket.MessageBinary, []byte{b}); err != nil {
			t.Fatal(err)
		}
	}
	var status [1]byte
	if _, err := io.ReadFull(websocket.NetConn(ctx, conn, websocket.MessageBinary), status[:]); err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, status[0], byte(0))
}

func wsPair(t *testing.T, opts *websocket.AcceptOptions) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	srv, client := net.Pipe()
	t.Cleanup(func() {
		srv.Close()
		client.Close()
	})
	accepted := make(chan *websocket.Conn, 1)
	go func() {
		defer close(accepted)
		rw := bufio.NewReadWriter(bufio.NewReader(srv), bufio.NewWriter(srv))
		req, err := http.ReadRequest(rw.Reader)
		if err != nil {
			t.Error(err)
			return
		}
		ws, err := websocket.Accept(&pipeResponse{
			ResponseRecorder: httptest.NewRecorder(),
			conn:             srv,
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
