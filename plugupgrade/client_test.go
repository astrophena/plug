// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plugupgrade

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/plug"
)

func TestDial(t *testing.T) {
	t.Parallel()

	for _, scheme := range []string{"http", "https", "ws", "wss"} {
		t.Run(scheme, func(t *testing.T) {
			t.Parallel()
			var (
				s      = service(t)
				h      = newUpgrade(t, s)
				srv    = httptest.NewUnstartedServer(h)
				tlsCfg *tls.Config
			)
			if scheme == "https" || scheme == "wss" {
				srv.StartTLS()
				roots := x509.NewCertPool()
				roots.AddCert(srv.Certificate())
				tlsCfg = &tls.Config{
					RootCAs: roots,
				}
			} else {
				srv.Start()
			}
			t.Cleanup(srv.Close)

			addr := srv.URL
			if scheme == "ws" || scheme == "wss" {
				addr = strings.Replace(addr, "http", "ws", 1)
			}
			for _, compress := range []bool{false, true} {
				c := dialClient(t, &Dialer{
					TLSConfig: tlsCfg,
				}, addr+"/plug", plug.ClientOptions{
					Compression: compress,
				})
				ping(t, c)
			}
		})
	}
}

func TestReverseProxy(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(newUpgrade(t, service(t)))
	t.Cleanup(backend.Close)
	backendAddr, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewTLSServer(httputil.NewSingleHostReverseProxy(backendAddr))
	t.Cleanup(proxy.Close)
	roots := x509.NewCertPool()
	roots.AddCert(proxy.Certificate())
	for _, scheme := range []string{"https", "wss"} {
		t.Run(scheme, func(t *testing.T) {
			c := dialClient(t, &Dialer{
				TLSConfig: &tls.Config{
					RootCAs: roots,
				},
			}, strings.Replace(proxy.URL, "https", scheme, 1)+"/plug", plug.ClientOptions{})
			ping(t, c)
		})
	}
}

func TestDialRejected(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"empty":        "",
		"wrong scheme": "unix:///tmp/plug.sock",
		"missing host": "https:///plug",
		"user info":    "https://user@example.test/plug",
		"fragment":     "https://example.test/plug#schema",
	}
	for name, addr := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := new(Dialer).DialContext(t.Context(), addr); err == nil {
				t.Fatal("DialContext() succeeded")
			}
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	for _, addr := range []string{srv.URL, strings.Replace(srv.URL, "http", "ws", 1)} {
		if _, err := new(Dialer).DialContext(t.Context(), addr+"/plug"); err == nil || !strings.Contains(err.Error(), "denied") {
			t.Fatalf("DialContext() error = %v, want denied response", err)
		}
	}
}

func TestDialCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := new(Dialer).DialContext(ctx, "http://example.test/plug")
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
		proxyAddr, err := ProxyFromEnvironment(target)
		if err != nil {
			t.Fatal(err)
		}
		if proxyAddr == nil {
			t.Fatal("proxy is nil")
		}
		testutil.AssertEqual(t, proxyAddr.String(), "socks5://127.0.0.1:1055")
		testutil.AssertEqual(t, target.Scheme, scheme)
	}

	t.Setenv("HTTP_PROXY", "http://localhost:8080")
	t.Setenv("HTTPS_PROXY", "http://localhost:8443")
	for scheme, want := range map[string]string{"ws": "http://localhost:8080", "wss": "http://localhost:8443"} {
		target, _ := url.Parse(scheme + "://example.com/plug")
		proxyAddr, err := ProxyFromEnvironment(target)
		if err != nil {
			t.Fatal(err)
		}
		if proxyAddr == nil {
			t.Fatal("proxy is nil")
		}
		testutil.AssertEqual(t, proxyAddr.String(), want)
	}

	t.Setenv("NO_PROXY", "example.com")
	for _, scheme := range []string{"http", "https", "ws", "wss"} {
		target, _ := url.Parse(scheme + "://example.com/plug")
		proxyAddr, err := ProxyFromEnvironment(target)
		if err != nil {
			t.Fatal(err)
		}
		if proxyAddr != nil {
			t.Fatalf("ProxyFromEnvironment(%s) with NO_PROXY = %v, want nil", scheme, proxyAddr)
		}
	}
}

func TestHTTPProxy(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(newUpgrade(t, service(t)))
	t.Cleanup(backend.Close)

	var hit atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		hit.Store(true)
		dst, err := net.Dial("tcp", r.Host)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		defer dst.Close()

		src, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer src.Close()
		if _, err := io.WriteString(src, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		tunnel(src, dst)
	}))
	t.Cleanup(proxy.Close)

	proxyAddr, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}

	d := &Dialer{
		Proxy: func(*url.URL) (*url.URL, error) {
			return proxyAddr, nil
		},
	}
	for _, scheme := range []string{"http", "ws"} {
		t.Run(scheme, func(t *testing.T) {
			hit.Store(false)
			addr := strings.Replace(backend.URL, "http", scheme, 1) + "/plug"
			c := dialClient(t, d, addr, plug.ClientOptions{})

			ping(t, c)
			if !hit.Load() {
				t.Fatal("HTTP proxy was not used")
			}
		})
	}
}

func TestProxyCanceled(t *testing.T) {
	for _, transport := range []string{"http", "ws"} {
		for _, scheme := range []string{"http", "https"} {
			t.Run(transport+"/"+scheme, func(t *testing.T) {
				t.Parallel()
				var (
					entered = make(chan struct{})
					release = make(chan struct{})
				)
				h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodConnect {
						t.Errorf("proxy method = %s; want CONNECT", r.Method)
					}
					close(entered)
					<-release
					w.WriteHeader(http.StatusBadGateway)
				})
				proxy := httptest.NewUnstartedServer(h)
				if scheme == "https" {
					proxy.StartTLS()
				} else {
					proxy.Start()
				}
				t.Cleanup(func() {
					close(release)
					proxy.Close()
				})
				proxyAddr, err := url.Parse(proxy.URL)
				if err != nil {
					t.Fatal(err)
				}
				d := &Dialer{
					Proxy: func(*url.URL) (*url.URL, error) { return proxyAddr, nil },
				}
				if scheme == "https" {
					roots := x509.NewCertPool()
					roots.AddCert(proxy.Certificate())
					d.TLSConfig = &tls.Config{
						RootCAs: roots,
					}
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				done := make(chan error, 1)
				go func() {
					conn, err := d.DialContext(ctx, transport+"://example.test/plug")
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

func TestSOCKS5Proxy(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(newUpgrade(t, service(t)))
	t.Cleanup(backend.Close)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var (
		hit atomic.Bool
		wg  sync.WaitGroup
	)
	t.Cleanup(func() {
		ln.Close()
		wg.Wait()
	})
	wg.Go(func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			hit.Store(true)
			wg.Go(func() { handleSocks5Conn(conn) })
		}
	})

	proxyAddr, err := url.Parse("socks5://" + ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	d := &Dialer{
		Proxy: func(*url.URL) (*url.URL, error) {
			return proxyAddr, nil
		},
	}
	for _, scheme := range []string{"http", "ws"} {
		t.Run(scheme, func(t *testing.T) {
			hit.Store(false)
			addr := strings.Replace(backend.URL, "http", scheme, 1) + "/plug"
			c := dialClient(t, d, addr, plug.ClientOptions{})

			ping(t, c)
			if !hit.Load() {
				t.Fatal("SOCKS5 proxy was not used")
			}
		})
	}
}

func TestProxyDeadline(t *testing.T) {
	t.Parallel()
	// Reuse httptest's certificate for the in-memory TLS handshake.
	cert := httptest.NewTLSServer(nil)
	roots := x509.NewCertPool()
	roots.AddCert(cert.Certificate())
	config := cert.TLS.Clone()
	cert.Close()

	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				peer, conn := net.Pipe()
				defer conn.Close()
				if scheme == "https" {
					peer = tls.Server(peer, config)
				}
				defer peer.Close()

				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				defer cancel()
				d := &Dialer{
					TLSConfig: &tls.Config{
						RootCAs: roots,
					},
				}
				done := make(chan error, 1)
				go func() {
					c, err := d.connectProxy(ctx, conn, &url.URL{
						Scheme: scheme,
						Host:   "127.0.0.1:8080",
					}, "example.test:80")
					if c != nil {
						c.Close()
					}
					done <- err
				}()

				req, err := http.ReadRequest(bufio.NewReader(peer))
				if err != nil {
					t.Fatal(err)
				}
				testutil.AssertEqual(t, req.Method, http.MethodConnect)
				testutil.AssertEqual(t, req.Host, "example.test:80")
				req.Body.Close()
				closed := make(chan struct{})
				go func() {
					io.Copy(io.Discard, peer)
					close(closed)
				}()

				synctest.Sleep(time.Minute - time.Nanosecond)
				select {
				case err := <-done:
					t.Fatalf("CONNECT returned before its deadline: %v", err)
				default:
				}

				synctest.Sleep(time.Nanosecond)
				if err := <-done; err == nil {
					t.Fatal("CONNECT succeeded after its deadline")
				}
				if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
					t.Fatalf("context error = %v; want deadline exceeded", ctx.Err())
				}
				select {
				case <-closed:
				default:
					t.Fatal("proxy connection stayed open")
				}
			})
		})
	}
}

func dialClient(t *testing.T, d *Dialer, addr string, opts plug.ClientOptions) *plug.Client {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	conn, err := d.DialContext(ctx, addr)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	c, err := plug.NewClient(t.Context(), conn, testSchema(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	return c
}

func handleSocks5Conn(c net.Conn) {
	defer c.Close()
	var hello [3]byte
	if _, err := io.ReadFull(c, hello[:]); err != nil || hello != [3]byte{5, 1, 0} {
		return
	}
	if _, err := c.Write([]byte{5, 0}); err != nil {
		return
	}

	// The local backend has an IPv4 address and needs no proxy authentication.
	var req [10]byte
	if _, err := io.ReadFull(c, req[:]); err != nil || req[0] != 5 || req[1] != 1 || req[3] != 1 {
		return
	}
	addr := net.JoinHostPort(net.IP(req[4:8]).String(), strconv.Itoa(int(binary.BigEndian.Uint16(req[8:]))))
	dst, err := net.Dial("tcp", addr)
	if err != nil {
		return
	}
	defer dst.Close()
	if _, err := c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	tunnel(c, dst)
}

func tunnel(a, b net.Conn) {
	done := make(chan struct{})
	go func() {
		io.Copy(a, b)
		a.Close()
		close(done)
	}()
	io.Copy(b, a)
	b.Close()
	<-done
}
