// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plugupgrade

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"testing/synctest"
	"time"
)

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
				if req.Method != http.MethodConnect || req.Host != "example.test:80" {
					t.Fatalf("proxy request = %s %s; want CONNECT example.test:80", req.Method, req.Host)
				}
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
