// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plugupgrade

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/net/http/httpproxy"
	"golang.org/x/net/proxy"
)

const maximumErrorBody = 64 << 10

// Dialer opens Plug connections over HTTP Upgrade or WebSocket.
type Dialer struct {
	// Header supplies request headers.
	// Required handshake values take precedence.
	Header http.Header

	// NetDialer dials TCP connections.
	// Nil uses a default dialer.
	NetDialer *net.Dialer

	// Proxy selects a proxy for an HTTP URL.
	// WebSocket URLs use http or https schemes.
	// Nil uses [ProxyFromEnvironment].
	Proxy func(*url.URL) (*url.URL, error)

	// TLSConfig supplies TLS settings, cloned and restricted to HTTP/1.1.
	TLSConfig *tls.Config
}

// DialContext opens an http, https, ws, or wss URL.
// ctx controls dialing and the upgrade.
// The caller must close the connection.
//
// A WebSocket deadline that expires during I/O closes the connection.
func (d *Dialer) DialContext(ctx context.Context, endpoint string) (result net.Conn, err error) {
	defer func() {
		if err != nil && ctx.Err() != nil {
			err = errors.Join(ctx.Err(), err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	target, addr, err := upgradeTarget(endpoint)
	if err != nil {
		return nil, err
	}
	if target.Scheme == "ws" || target.Scheme == "wss" {
		return d.dialWS(ctx, target)
	}

	conn, err := d.dialConn(ctx, target, addr)
	if err != nil {
		return nil, err
	}
	owned := true
	defer func() {
		if owned {
			conn.Close()
		}
	}()

	baseConn := conn
	stopCancel := context.AfterFunc(ctx, func() { baseConn.Close() })
	defer stopCancel()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, fmt.Errorf("plugupgrade: set handshake deadline: %w", err)
		}
	}

	if target.Scheme == "https" {
		cfg := d.tlsConfig(target.Hostname())
		tc := tls.Client(conn, cfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			return nil, fmt.Errorf("plugupgrade: TLS handshake: %w", err)
		}
		conn = tc
	}

	req := &http.Request{
		Method:     http.MethodGet,
		URL:        target,
		Host:       target.Host,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     d.Header.Clone(),
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", protocolName)
	if err := req.Write(conn); err != nil {
		return nil, fmt.Errorf("plugupgrade: write upgrade request: %w", err)
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		return nil, fmt.Errorf("plugupgrade: read upgrade response: %w", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		message, readErr := io.ReadAll(io.LimitReader(resp.Body, maximumErrorBody+1))
		closeErr := resp.Body.Close()
		if readErr != nil {
			return nil, errors.Join(
				fmt.Errorf("plugupgrade: server rejected upgrade with %s: read response: %w", resp.Status, readErr),
				closeErr,
			)
		}
		if len(message) > maximumErrorBody {
			message = message[:maximumErrorBody]
		}
		detail := strings.TrimSpace(string(message))
		if detail == "" {
			return nil, errors.Join(fmt.Errorf("plugupgrade: server rejected upgrade with %s", resp.Status), closeErr)
		}
		return nil, errors.Join(fmt.Errorf("plugupgrade: server rejected upgrade with %s: %s", resp.Status, detail), closeErr)
	}
	if !headerHasToken(resp.Header, "Connection", "upgrade") ||
		!strings.EqualFold(strings.TrimSpace(resp.Header.Get("Upgrade")), protocolName) {
		return nil, errors.New("plugupgrade: invalid switching-protocols response")
	}
	if !stopCancel() || ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, fmt.Errorf("plugupgrade: clear handshake deadline: %w", err)
	}
	owned = false
	return &bufferedConn{
		Conn:   conn,
		reader: reader,
	}, nil
}

func (d *Dialer) dialConn(ctx context.Context, target *url.URL, addr string) (net.Conn, error) {
	proxyFunc := d.Proxy
	if proxyFunc == nil {
		proxyFunc = ProxyFromEnvironment
	}
	proxyURL, err := proxyFunc(target)
	if err != nil {
		return nil, fmt.Errorf("plugupgrade: resolve proxy for %s: %w", target, err)
	}
	if proxyURL != nil {
		switch proxyURL.Scheme {
		case "socks5", "socks5h":
			return d.dialSOCKS5Proxy(ctx, proxyURL, addr)
		case "http", "https":
			return d.dialHTTPProxy(ctx, proxyURL, addr)
		default:
			return nil, fmt.Errorf("plugupgrade: unsupported proxy scheme %q", proxyURL.Scheme)
		}
	}

	conn, err := d.netDialer().DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("plugupgrade: dial %s: %w", addr, err)
	}
	return conn, nil
}

func (d *Dialer) dialSOCKS5Proxy(ctx context.Context, proxyURL *url.URL, addr string) (net.Conn, error) {
	sd, err := proxy.FromURL(proxyURL, d.netDialer())
	if err != nil {
		return nil, fmt.Errorf("plugupgrade: SOCKS5 proxy %s: %w", proxyURL.Redacted(), err)
	}
	conn, err := sd.(proxy.ContextDialer).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("plugupgrade: dial %s via SOCKS5 proxy %s: %w", addr, proxyURL.Redacted(), err)
	}
	return conn, nil
}

func (d *Dialer) dialHTTPProxy(ctx context.Context, proxyURL *url.URL, addr string) (net.Conn, error) {
	conn, err := d.netDialer().DialContext(ctx, "tcp", urlAddr(proxyURL))
	if err != nil {
		return nil, fmt.Errorf("plugupgrade: dial HTTP proxy %s: %w", proxyURL.Redacted(), err)
	}
	return d.connectProxy(ctx, conn, proxyURL, addr)
}

func (d *Dialer) connectProxy(ctx context.Context, conn net.Conn, proxyURL *url.URL, addr string) (net.Conn, error) {
	baseConn := conn
	stopCancel := context.AfterFunc(ctx, func() { baseConn.Close() })
	defer stopCancel()

	owned := true
	defer func() {
		if owned {
			conn.Close()
		}
	}()

	if proxyURL.Scheme == "https" {
		cfg := d.tlsConfig(proxyURL.Hostname())
		tc := tls.Client(conn, cfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			return nil, fmt.Errorf("plugupgrade: HTTP proxy TLS handshake: %w", err)
		}
		conn = tc
	}

	req := &http.Request{
		Method: http.MethodConnect,
		URL: &url.URL{
			Opaque: addr,
		},
		Host:       addr,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
	}
	if proxyURL.User != nil {
		password, _ := proxyURL.User.Password()
		auth := proxyURL.User.Username() + ":" + password
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
	}

	if err := req.Write(conn); err != nil {
		return nil, fmt.Errorf("plugupgrade: write CONNECT to proxy %s: %w", proxyURL.Redacted(), err)
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		return nil, fmt.Errorf("plugupgrade: read CONNECT response from proxy %s: %w", proxyURL.Redacted(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("plugupgrade: HTTP proxy %s rejected CONNECT with status %s", proxyURL.Redacted(), resp.Status)
	}
	if !stopCancel() || ctx.Err() != nil {
		return nil, ctx.Err()
	}

	owned = false
	if reader.Buffered() > 0 {
		return &bufferedConn{
			Conn:   conn,
			reader: reader,
		}, nil
	}
	return conn, nil
}

func (d *Dialer) netDialer() *net.Dialer {
	if d.NetDialer != nil {
		return d.NetDialer
	}
	return new(net.Dialer)
}

func (d *Dialer) tlsConfig(serverName string) *tls.Config {
	cfg := new(tls.Config)
	if d.TLSConfig != nil {
		cfg = d.TLSConfig.Clone()
	}
	if cfg.ServerName == "" {
		cfg.ServerName = serverName
	}
	cfg.NextProtos = []string{"http/1.1"}
	return cfg
}

// ProxyFromEnvironment selects a proxy using HTTP_PROXY, HTTPS_PROXY, ALL_PROXY, and NO_PROXY.
// Lowercase forms are also accepted.
// WebSocket URLs use HTTP proxy rules.
func ProxyFromEnvironment(reqURL *url.URL) (*url.URL, error) {
	u := *reqURL
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	}

	cfg := httpproxy.FromEnvironment()
	allProxy := os.Getenv("ALL_PROXY")
	if allProxy == "" {
		allProxy = os.Getenv("all_proxy")
	}
	if allProxy != "" {
		if cfg.HTTPProxy == "" {
			cfg.HTTPProxy = allProxy
		}
		if cfg.HTTPSProxy == "" {
			cfg.HTTPSProxy = allProxy
		}
	}
	return cfg.ProxyFunc()(&u)
}

func upgradeTarget(endpoint string) (*url.URL, string, error) {
	target, err := url.Parse(endpoint)
	if err != nil {
		return nil, "", fmt.Errorf("plugupgrade: parse endpoint: %w", err)
	}
	switch target.Scheme {
	case "http", "https", "ws", "wss":
	default:
		return nil, "", fmt.Errorf("plugupgrade: endpoint scheme %q is not http, https, ws, or wss", target.Scheme)
	}
	if target.Hostname() == "" {
		return nil, "", errors.New("plugupgrade: endpoint host is required")
	}
	if target.User != nil {
		return nil, "", errors.New("plugupgrade: endpoint user information is not supported")
	}
	if target.Fragment != "" {
		return nil, "", errors.New("plugupgrade: endpoint fragment is not supported")
	}
	if target.Path == "" {
		target.Path = "/"
	}
	return target, urlAddr(target), nil
}

func urlAddr(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" || u.Scheme == "wss" {
			port = "443"
		}
	}
	return net.JoinHostPort(u.Hostname(), port)
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(data []byte) (int, error) { return c.reader.Read(data) }
