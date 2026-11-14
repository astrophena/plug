// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plugupgrade

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/plug"
	"go.astrophena.name/plug/plugimpl"
)

func TestBufferedPreface(t *testing.T) {
	t.Parallel()
	s := service(t)
	upgrade := newUpgrade(t, s)
	srv := httptest.NewServer(upgrade)
	t.Cleanup(srv.Close)
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", target.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var preface [6]byte
	copy(preface[:4], "PLUG")
	preface[4] = 1
	preface[5] = 1 // inspect
	r := "GET /plug HTTP/1.1\r\nHost: " + target.Host + "\r\nConnection: Upgrade\r\nUpgrade: plug\r\n\r\n"
	if _, err := io.WriteString(conn, r+string(preface[:])); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	w, err := http.ReadResponse(reader, &http.Request{
		Method: http.MethodGet,
	})
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, w.StatusCode, http.StatusSwitchingProtocols)
	var status [1]byte
	if _, err := io.ReadFull(reader, status[:]); err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, status[0], byte(0))
}

func TestUpgradeRejected(t *testing.T) {
	t.Parallel()
	upgrade := newUpgrade(t, service(t))
	cases := map[string]struct {
		method  string
		proto   string
		headers http.Header
		status  int
	}{
		"method": {
			method:  http.MethodPost,
			proto:   "HTTP/1.1",
			headers: http.Header{"Connection": {"Upgrade"}, "Upgrade": {"plug"}},
			status:  http.StatusMethodNotAllowed,
		},
		"protocol": {
			method:  http.MethodGet,
			proto:   "HTTP/2.0",
			headers: http.Header{"Connection": {"Upgrade"}, "Upgrade": {"plug"}},
			status:  http.StatusUpgradeRequired,
		},
		"connection header": {
			method:  http.MethodGet,
			proto:   "HTTP/1.1",
			headers: http.Header{"Upgrade": {"plug"}},
			status:  http.StatusUpgradeRequired,
		},
		"upgrade header": {
			method:  http.MethodGet,
			proto:   "HTTP/1.1",
			headers: http.Header{"Connection": {"Upgrade"}, "Upgrade": {"unknown"}},
			status:  http.StatusUpgradeRequired,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(tc.method, "http://example.test/plug", nil)
			r.Header = tc.headers
			r.Proto = tc.proto
			if tc.proto == "HTTP/2.0" {
				r.ProtoMajor, r.ProtoMinor = 2, 0
			}
			w := httptest.NewRecorder()
			upgrade.ServeHTTP(w, r)
			testutil.AssertEqual(t, w.Code, tc.status)
		})
	}
}

func TestShutdown(t *testing.T) {
	t.Parallel()
	upgrade := newUpgrade(t, service(t))
	srv := httptest.NewServer(upgrade)
	t.Cleanup(srv.Close)
	var (
		c  = dialClient(t, new(Dialer), srv.URL+"/plug", plug.ClientOptions{})
		ws = dialClient(t, new(Dialer), strings.Replace(srv.URL, "http", "ws", 1)+"/plug", plug.ClientOptions{})
	)
	if !upgrade.IsActive() {
		t.Fatal("server has no active connections")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := upgrade.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(t.Context(), echoMethod(testSchema()), ""); err == nil {
		t.Fatal("call after shutdown succeeded")
	}
	if _, err := ws.Call(t.Context(), echoMethod(testSchema()), ""); err == nil {
		t.Fatal("WebSocket call after shutdown succeeded")
	}
	if upgrade.IsActive() {
		t.Fatal("server still has active connections")
	}
	for _, addr := range []string{srv.URL, strings.Replace(srv.URL, "http", "ws", 1)} {
		if _, err := new(Dialer).DialContext(t.Context(), addr+"/plug"); err == nil || !strings.Contains(err.Error(), "503") {
			t.Fatalf("DialContext() error = %v, want 503", err)
		}
	}
}

func testSchema() *plug.Schema {
	return &plug.Schema{
		Package: "upgrade",
		Major:   1,
		Family:  plugimpl.FamilyFingerprint("upgrade", 1),
		Source:  "package upgrade version 1\nupgrade.echo message: string -> string public\n",
		Methods: []plug.MethodInfo{
			{
				ID:     1,
				Name:   "upgrade.echo",
				Public: true,
				Fields: []plug.FieldInfo{
					{
						Name: "message",
						Type: plugimpl.PrimitiveType("string"),
					},
				},
				Response: plugimpl.PrimitiveType("string"),
			},
		},
	}
}

func echoMethod(sch *plug.Schema) plugimpl.Method[string, string] {
	return plugimpl.NewMethod(sch, 1, plugimpl.StringCodec, plugimpl.StringCodec, plugimpl.ErrorCodec{})
}

func service(t *testing.T) *plug.Server {
	t.Helper()
	s, err := plug.NewServer(testSchema(), plug.ServerOptions{
		Compression: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	registerEcho(t, s)
	return s
}

func registerEcho(t *testing.T, s *plug.Server) {
	t.Helper()
	var regs []plugimpl.Registration
	if err := plugimpl.AppendHandler(&regs, echoMethod(s.Schema()), func(_ context.Context, v string) (string, error) { return v, nil }, "Echo"); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterHandlers(regs); err != nil {
		t.Fatal(err)
	}
}

func ping(t *testing.T, c *plug.Client) {
	t.Helper()
	got, err := c.Call(t.Context(), echoMethod(testSchema()), "hello")
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, got, "hello")
}

func newUpgrade(t *testing.T, s *plug.Server) *Server {
	t.Helper()
	h, err := NewServer(s, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := h.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return h
}
