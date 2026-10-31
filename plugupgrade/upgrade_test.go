// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plugupgrade_test

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/example/greeter"
	"go.astrophena.name/plug/plugupgrade"

	"github.com/coder/websocket"
)

func TestDialTransports(t *testing.T) {
	t.Parallel()
	for _, scheme := range []string{"http", "https", "ws", "wss"} {
		t.Run(scheme, func(t *testing.T) {
			t.Parallel()
			plugServer := newGreeterServer(t)
			upgrade, err := plugupgrade.NewServer(plugServer, plugupgrade.Options{})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewUnstartedServer(upgrade)
			var tlsConfig *tls.Config
			if scheme == "https" || scheme == "wss" {
				server.StartTLS()
				roots := x509.NewCertPool()
				roots.AddCert(server.Certificate())
				tlsConfig = &tls.Config{
					RootCAs: roots,
				}
			} else {
				server.Start()
			}
			t.Cleanup(server.Close)

			endpoint := server.URL
			if scheme == "ws" || scheme == "wss" {
				endpoint = strings.Replace(endpoint, "http", "ws", 1)
			}
			client := dialGreeter(t, endpoint, tlsConfig)
			defer client.Close()
			if err := greeter.NewClient(client).Ping(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCompression(t *testing.T) {
	t.Parallel()
	plugServer := newGreeterServerWithOptions(t, plug.ServerOptions{
		Compression: true,
	})
	upgrade, err := plugupgrade.NewServer(plugServer, plugupgrade.Options{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(upgrade)
	t.Cleanup(server.Close)
	for _, scheme := range []string{"http", "ws"} {
		t.Run(scheme, func(t *testing.T) {
			endpoint := strings.Replace(server.URL, "http", scheme, 1) + "/plug"
			client := dialClient(t, new(plugupgrade.Dialer), endpoint, plug.ClientOptions{
				Compression: true,
			})
			defer client.Close()
			if err := greeter.NewClient(client).Ping(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDialThroughTLSReverseProxy(t *testing.T) {
	t.Parallel()
	plugServer := newGreeterServer(t)
	upgrade, err := plugupgrade.NewServer(plugServer, plugupgrade.Options{})
	if err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(upgrade)
	t.Cleanup(backend.Close)
	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewTLSServer(httputil.NewSingleHostReverseProxy(backendURL))
	t.Cleanup(proxy.Close)
	roots := x509.NewCertPool()
	roots.AddCert(proxy.Certificate())
	for _, scheme := range []string{"https", "wss"} {
		t.Run(scheme, func(t *testing.T) {
			client := dialGreeter(t, strings.Replace(proxy.URL, "https", scheme, 1), &tls.Config{
				RootCAs: roots,
			})
			defer client.Close()
			if err := greeter.NewClient(client).Ping(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestServerPreservesBufferedPreface(t *testing.T) {
	t.Parallel()
	plugServer := newGreeterServer(t)
	upgrade, err := plugupgrade.NewServer(plugServer, plugupgrade.Options{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(upgrade)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
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
	request := "GET /plug HTTP/1.1\r\nHost: " + target.Host + "\r\nConnection: Upgrade\r\nUpgrade: plug\r\n\r\n"
	if _, err := io.WriteString(conn, request+string(preface[:])); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{
		Method: http.MethodGet,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusSwitchingProtocols)
	}
	var status [1]byte
	if _, err := io.ReadFull(reader, status[:]); err != nil {
		t.Fatal(err)
	}
	if status[0] != 0 {
		t.Errorf("server handshake status = %d, want accepted", status[0])
	}
}

func TestServerRejectsInvalidUpgrade(t *testing.T) {
	t.Parallel()
	upgrade, err := plugupgrade.NewServer(newGreeterServer(t), plugupgrade.Options{})
	if err != nil {
		t.Fatal(err)
	}
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
			request := httptest.NewRequest(tc.method, "http://example.test/plug", nil)
			request.Header = tc.headers
			request.Proto = tc.proto
			if tc.proto == "HTTP/2.0" {
				request.ProtoMajor, request.ProtoMinor = 2, 0
			}
			response := httptest.NewRecorder()
			upgrade.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Errorf("status = %d, want %d", response.Code, tc.status)
			}
		})
	}
}

func TestDialRejectsInvalidEndpointsAndResponses(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"empty":        "",
		"wrong scheme": "unix:///tmp/plug.sock",
		"missing host": "https:///plug",
		"user info":    "https://user@example.test/plug",
		"fragment":     "https://example.test/plug#schema",
		"WS host":      "ws:///plug",
		"WS user info": "wss://user@example.test/plug",
		"WS fragment":  "ws://example.test/plug#schema",
	}
	for name, endpoint := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := new(plugupgrade.Dialer).DialContext(t.Context(), endpoint); err == nil {
				t.Fatal("DialContext() succeeded")
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "denied", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)
	for _, endpoint := range []string{server.URL, strings.Replace(server.URL, "http", "ws", 1)} {
		if _, err := new(plugupgrade.Dialer).DialContext(t.Context(), endpoint+"/plug"); err == nil || !strings.Contains(err.Error(), "denied") {
			t.Fatalf("DialContext() error = %v, want denied response", err)
		}
	}
}

func TestServerShutdownClosesConnectionsAndRefusesUpgrades(t *testing.T) {
	t.Parallel()
	upgrade, err := plugupgrade.NewServer(newGreeterServer(t), plugupgrade.Options{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(upgrade)
	t.Cleanup(server.Close)
	client := dialGreeter(t, server.URL, nil)
	defer client.Close()
	wsClient := dialGreeter(t, strings.Replace(server.URL, "http", "ws", 1), nil)
	defer wsClient.Close()
	if !upgrade.IsActive() {
		t.Fatal("server has no active connections")
	}
	shutdownCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := upgrade.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	if err := greeter.NewClient(client).Ping(t.Context()); err == nil {
		t.Fatal("call after shutdown succeeded")
	}
	if err := greeter.NewClient(wsClient).Ping(t.Context()); err == nil {
		t.Fatal("WebSocket call after shutdown succeeded")
	}
	if upgrade.IsActive() {
		t.Fatal("server still has active connections")
	}
	for _, endpoint := range []string{server.URL, strings.Replace(server.URL, "http", "ws", 1)} {
		if _, err := new(plugupgrade.Dialer).DialContext(t.Context(), endpoint+"/plug"); err == nil || !strings.Contains(err.Error(), "503") {
			t.Fatalf("DialContext() error = %v, want 503", err)
		}
	}
}

func newGreeterServer(t *testing.T) *plug.Server {
	t.Helper()
	return newGreeterServerWithOptions(t, plug.ServerOptions{})
}

func newGreeterServerWithOptions(t *testing.T, options plug.ServerOptions) *plug.Server {
	t.Helper()
	options.Security = plug.Security{
		Authenticate: func(context.Context, net.Conn) (plug.Principal, error) {
			return plug.Principal{
				Subject: "test",
			}, nil
		},
		Authorize: func(context.Context, plug.Principal, string, plug.MethodInfo, any) error {
			return nil
		},
	}
	server, err := plug.NewServer(&greeter.Schema, options)
	if err != nil {
		t.Fatal(err)
	}
	if err := greeter.RegisterHandlers(server, greeter.Handlers{
		GetUser: func(context.Context, greeter.GetUserRequest) (greeter.User, error) {
			return greeter.User{}, &greeter.NotFound{}
		},
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) { return nil, nil },
		Ping:      func(context.Context) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	return server
}

func dialGreeter(t *testing.T, baseURL string, tlsConfig *tls.Config) *plug.Client {
	t.Helper()
	return dialClient(t, &plugupgrade.Dialer{
		TLSConfig: tlsConfig,
	}, baseURL+"/plug", plug.ClientOptions{})
}

func dialClient(t *testing.T, d *plugupgrade.Dialer, endpoint string, opts plug.ClientOptions) *plug.Client {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	conn, err := d.DialContext(ctx, endpoint)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	client, err := plug.NewClient(t.Context(), conn, &greeter.Schema, opts)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestNewServerRejectsNil(t *testing.T) {
	t.Parallel()
	if _, err := plugupgrade.NewServer(nil, plugupgrade.Options{}); err == nil {
		t.Fatal("NewServer(nil) succeeded")
	}
}

func TestDialHonorsCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := new(plugupgrade.Dialer).DialContext(ctx, "http://example.test/plug")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DialContext() error = %v, want context.Canceled", err)
	}
}

func TestProxyFromEnvironment(t *testing.T) {
	t.Setenv("ALL_PROXY", "socks5://127.0.0.1:1055")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("NO_PROXY", "")

	for _, scheme := range []string{"http", "https", "ws", "wss"} {
		target, _ := url.Parse(scheme + "://example.com/plug")
		proxyURL, err := plugupgrade.ProxyFromEnvironment(target)
		if err != nil {
			t.Fatal(err)
		}
		if proxyURL == nil || proxyURL.String() != "socks5://127.0.0.1:1055" {
			t.Fatalf("ProxyFromEnvironment(%s) = %v, want socks5://127.0.0.1:1055", scheme, proxyURL)
		}
		if target.Scheme != scheme {
			t.Fatal("ProxyFromEnvironment changed its argument")
		}
	}

	t.Setenv("HTTP_PROXY", "http://localhost:8080")
	t.Setenv("HTTPS_PROXY", "http://localhost:8443")
	for scheme, want := range map[string]string{"ws": "http://localhost:8080", "wss": "http://localhost:8443"} {
		target, _ := url.Parse(scheme + "://example.com/plug")
		proxyURL, err := plugupgrade.ProxyFromEnvironment(target)
		if err != nil {
			t.Fatal(err)
		}
		if proxyURL == nil || proxyURL.String() != want {
			t.Fatalf("ProxyFromEnvironment(%s) = %v; want %s", scheme, proxyURL, want)
		}
	}

	t.Setenv("NO_PROXY", "example.com")
	for _, scheme := range []string{"http", "https", "ws", "wss"} {
		target, _ := url.Parse(scheme + "://example.com/plug")
		proxyURL, err := plugupgrade.ProxyFromEnvironment(target)
		if err != nil {
			t.Fatal(err)
		}
		if proxyURL != nil {
			t.Fatalf("ProxyFromEnvironment(%s) with NO_PROXY = %v, want nil", scheme, proxyURL)
		}
	}
}

func TestDialThroughHTTPProxy(t *testing.T) {
	t.Parallel()
	plugServer := newGreeterServer(t)
	upgrade, err := plugupgrade.NewServer(plugServer, plugupgrade.Options{})
	if err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(upgrade)
	t.Cleanup(backend.Close)

	var proxyHit atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		proxyHit.Store(true)
		destConn, err := net.Dial("tcp", r.Host)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijack not supported", http.StatusInternalServerError)
			return
		}
		clientConn, _, err := hijacker.Hijack()
		if err != nil {
			return
		}
		clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		go func() {
			io.Copy(destConn, clientConn)
			destConn.Close()
		}()
		go func() {
			io.Copy(clientConn, destConn)
			clientConn.Close()
		}()
	}))
	t.Cleanup(proxy.Close)

	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}

	dialer := &plugupgrade.Dialer{
		Proxy: func(*url.URL) (*url.URL, error) {
			return proxyURL, nil
		},
	}
	for _, scheme := range []string{"http", "ws"} {
		t.Run(scheme, func(t *testing.T) {
			proxyHit.Store(false)
			endpoint := strings.Replace(backend.URL, "http", scheme, 1) + "/plug"
			client := dialClient(t, dialer, endpoint, plug.ClientOptions{})
			defer client.Close()

			if err := greeter.NewClient(client).Ping(t.Context()); err != nil {
				t.Fatal(err)
			}
			if !proxyHit.Load() {
				t.Fatal("HTTP proxy was not used")
			}
		})
	}
}

func TestDialCancelsProxyCONNECT(t *testing.T) {
	for _, transport := range []string{"http", "ws"} {
		for _, scheme := range []string{"http", "https"} {
			t.Run(transport+"/"+scheme, func(t *testing.T) {
				t.Parallel()
				var (
					entered = make(chan struct{})
					release = make(chan struct{})
				)
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodConnect {
						t.Errorf("proxy method = %s; want CONNECT", r.Method)
					}
					close(entered)
					<-release
					w.WriteHeader(http.StatusBadGateway)
				})
				proxy := httptest.NewUnstartedServer(handler)
				if scheme == "https" {
					proxy.StartTLS()
				} else {
					proxy.Start()
				}
				t.Cleanup(func() {
					close(release)
					proxy.Close()
				})
				proxyURL, err := url.Parse(proxy.URL)
				if err != nil {
					t.Fatal(err)
				}
				dialer := &plugupgrade.Dialer{
					Proxy: func(*url.URL) (*url.URL, error) { return proxyURL, nil },
				}
				if scheme == "https" {
					roots := x509.NewCertPool()
					roots.AddCert(proxy.Certificate())
					dialer.TLSConfig = &tls.Config{
						RootCAs: roots,
					}
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				done := make(chan error, 1)
				go func() {
					conn, err := dialer.DialContext(ctx, transport+"://example.test/plug")
					if conn != nil {
						conn.Close()
					}
					done <- err
				}()
				select {
				case <-entered:
				case err := <-done:
					t.Fatalf("DialContext returned before CONNECT: %v", err)
				case <-time.After(5 * time.Second):
					t.Fatal("proxy did not receive CONNECT")
				}
				cancel()
				select {
				case err := <-done:
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("DialContext() = %v; want context.Canceled", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("DialContext did not cancel the CONNECT request")
				}
			})
		}
	}
}

func TestDialThroughSOCKS5Proxy(t *testing.T) {
	t.Parallel()
	plugServer := newGreeterServer(t)
	upgrade, err := plugupgrade.NewServer(plugServer, plugupgrade.Options{})
	if err != nil {
		t.Fatal(err)
	}
	backend := httptest.NewServer(upgrade)
	t.Cleanup(backend.Close)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	var socksHit atomic.Bool
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			socksHit.Store(true)
			go handleSocks5Conn(conn)
		}
	}()

	proxyURL, err := url.Parse("socks5://" + ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	dialer := &plugupgrade.Dialer{
		Proxy: func(*url.URL) (*url.URL, error) {
			return proxyURL, nil
		},
	}
	for _, scheme := range []string{"http", "ws"} {
		t.Run(scheme, func(t *testing.T) {
			socksHit.Store(false)
			endpoint := strings.Replace(backend.URL, "http", scheme, 1) + "/plug"
			client := dialClient(t, dialer, endpoint, plug.ClientOptions{})
			defer client.Close()

			if err := greeter.NewClient(client).Ping(t.Context()); err != nil {
				t.Fatal(err)
			}
			if !socksHit.Load() {
				t.Fatal("SOCKS5 proxy was not used")
			}
		})
	}
}

func TestWebSocketAuthenticationAndLargeResponse(t *testing.T) {
	t.Parallel()
	name := strings.Repeat("hello", 32<<10)
	server, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{
		Security: plug.Security{
			Authorize: func(_ context.Context, principal plug.Principal, _ string, _ plug.MethodInfo, _ any) error {
				if principal.Subject != "test" {
					return errors.New("unexpected principal")
				}
				return nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := greeter.RegisterHandlers(server, greeter.Handlers{
		GetUser: func(context.Context, greeter.GetUserRequest) (greeter.User, error) {
			return greeter.User{
				Name: name,
				Role: &greeter.Guest{},
			}, nil
		},
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) { return nil, nil },
		Ping:      func(context.Context) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	upgrade, err := plugupgrade.NewServer(server, plugupgrade.Options{
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
	endpoint := strings.Replace(web.URL, "http", "ws", 1) + "/plug?profile=test"
	if _, err := new(plugupgrade.Dialer).DialContext(t.Context(), endpoint); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("unauthenticated DialContext() = %v; want denied", err)
	}
	dialer := &plugupgrade.Dialer{
		Header: http.Header{"Authorization": {"Bearer test"}},
	}
	client := dialClient(t, dialer, endpoint, plug.ClientOptions{})
	defer client.Close()

	user, err := greeter.NewClient(client).GetUser(t.Context(), greeter.GetUserRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if user.Name != name {
		t.Fatalf("response name length = %d; want %d", len(user.Name), len(name))
	}
}

func TestWebSocketRejectsCrossOriginAndText(t *testing.T) {
	t.Parallel()
	upgrade, err := plugupgrade.NewServer(newGreeterServer(t), plugupgrade.Options{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(upgrade)
	defer server.Close()
	endpoint := strings.Replace(server.URL, "http", "ws", 1) + "/plug"
	dialer := &plugupgrade.Dialer{
		Header: http.Header{"Origin": {"https://other.example"}},
	}
	if _, err := dialer.DialContext(t.Context(), endpoint); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("cross-origin DialContext() = %v; want 403", err)
	}
	conn, _, err := websocket.Dial(t.Context(), endpoint, nil)
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

func TestWebSocketPrefaceAcrossMessages(t *testing.T) {
	t.Parallel()
	upgrade, err := plugupgrade.NewServer(newGreeterServer(t), plugupgrade.Options{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(upgrade)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, server.URL+"/plug", nil)
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
	if status[0] != 0 {
		t.Fatalf("server handshake status = %d; want accepted", status[0])
	}
}

func handleSocks5Conn(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 256)
	if _, err := io.ReadFull(conn, buf[:2]); err != nil || buf[0] != 5 {
		return
	}
	nmethods := int(buf[1])
	if _, err := io.ReadFull(conn, buf[:nmethods]); err != nil {
		return
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return
	}
	if _, err := io.ReadFull(conn, buf[:4]); err != nil || buf[0] != 5 || buf[1] != 1 {
		return
	}
	var targetAddr string
	switch buf[3] {
	case 1:
		if _, err := io.ReadFull(conn, buf[:4]); err != nil {
			return
		}
		ip := net.IP(buf[:4])
		if _, err := io.ReadFull(conn, buf[4:6]); err != nil {
			return
		}
		port := int(buf[4])<<8 | int(buf[5])
		targetAddr = fmt.Sprintf("%s:%d", ip.String(), port)
	case 3:
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return
		}
		dlen := int(buf[0])
		if _, err := io.ReadFull(conn, buf[:dlen]); err != nil {
			return
		}
		domain := string(buf[:dlen])
		if _, err := io.ReadFull(conn, buf[dlen:dlen+2]); err != nil {
			return
		}
		port := int(buf[dlen])<<8 | int(buf[dlen+1])
		targetAddr = fmt.Sprintf("%s:%d", domain, port)
	default:
		return
	}
	targetConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		conn.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer targetConn.Close()
	if _, err := conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	go func() { io.Copy(targetConn, conn) }()
	io.Copy(conn, targetConn)
}
