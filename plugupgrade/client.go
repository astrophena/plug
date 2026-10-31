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

// Dialer opens Plug connections through HTTP/1.1 Upgrade or WebSocket.
type Dialer struct {
	// Header supplies request headers. Required handshake values take precedence.
	Header http.Header

	// NetDialer supplies TCP dialing. If nil, a default dialer is used.
	NetDialer *net.Dialer

	// Proxy selects a proxy for an HTTP URL. WebSocket URLs are mapped to http
	// or https. If nil, ProxyFromEnvironment is used.
	Proxy func(*url.URL) (*url.URL, error)

	// TLSConfig supplies TLS settings. Connections use a clone restricted to
	// HTTP/1.1.
	TLSConfig *tls.Config
}

// DialContext opens endpoint, a complete http, https, ws, or wss URL.
// The context bounds dialing and the upgrade. The caller closes the connection.
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
	target, address, err := upgradeTarget(endpoint)
	if err != nil {
		return nil, err
	}
	if target.Scheme == "ws" || target.Scheme == "wss" {
		return d.dialWS(ctx, target)
	}

	conn, err := d.dialConn(ctx, target, address)
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
		config := d.tlsConfig(target.Hostname())
		tlsConn := tls.Client(conn, config)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return nil, fmt.Errorf("plugupgrade: TLS handshake: %w", err)
		}
		conn = tlsConn
	}

	request := &http.Request{
		Method:     http.MethodGet,
		URL:        target,
		Host:       target.Host,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     d.Header.Clone(),
	}
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", protocolName)
	if err := request.Write(conn); err != nil {
		return nil, fmt.Errorf("plugupgrade: write upgrade request: %w", err)
	}

	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		return nil, fmt.Errorf("plugupgrade: read upgrade response: %w", err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		message, readErr := io.ReadAll(io.LimitReader(response.Body, maximumErrorBody+1))
		closeErr := response.Body.Close()
		if readErr != nil {
			return nil, errors.Join(
				fmt.Errorf("plugupgrade: server rejected upgrade with %s: read response: %w", response.Status, readErr),
				closeErr,
			)
		}
		if len(message) > maximumErrorBody {
			message = message[:maximumErrorBody]
		}
		detail := strings.TrimSpace(string(message))
		if detail == "" {
			return nil, errors.Join(fmt.Errorf("plugupgrade: server rejected upgrade with %s", response.Status), closeErr)
		}
		return nil, errors.Join(fmt.Errorf("plugupgrade: server rejected upgrade with %s: %s", response.Status, detail), closeErr)
	}
	if !headerHasToken(response.Header, "Connection", "upgrade") ||
		!strings.EqualFold(strings.TrimSpace(response.Header.Get("Upgrade")), protocolName) {
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

func (d *Dialer) dialConn(ctx context.Context, target *url.URL, address string) (net.Conn, error) {
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
			return d.dialSOCKS5Proxy(ctx, proxyURL, address)
		case "http", "https":
			return d.dialHTTPProxy(ctx, proxyURL, address)
		default:
			return nil, fmt.Errorf("plugupgrade: unsupported proxy scheme %q", proxyURL.Scheme)
		}
	}

	conn, err := d.netDialer().DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("plugupgrade: dial %s: %w", address, err)
	}
	return conn, nil
}

func (d *Dialer) dialSOCKS5Proxy(ctx context.Context, proxyURL *url.URL, targetAddress string) (net.Conn, error) {
	socksDialer, err := proxy.FromURL(proxyURL, d.netDialer())
	if err != nil {
		return nil, fmt.Errorf("plugupgrade: SOCKS5 proxy %s: %w", proxyURL.Redacted(), err)
	}
	conn, err := socksDialer.(proxy.ContextDialer).DialContext(ctx, "tcp", targetAddress)
	if err != nil {
		return nil, fmt.Errorf("plugupgrade: dial %s via SOCKS5 proxy %s: %w", targetAddress, proxyURL.Redacted(), err)
	}
	return conn, nil
}

func (d *Dialer) dialHTTPProxy(ctx context.Context, proxyURL *url.URL, targetAddress string) (net.Conn, error) {
	proxyHost := proxyURL.Hostname()
	proxyPort := proxyURL.Port()
	if proxyPort == "" {
		if proxyURL.Scheme == "https" {
			proxyPort = "443"
		} else {
			proxyPort = "80"
		}
	}
	proxyAddress := net.JoinHostPort(proxyHost, proxyPort)

	pConn, err := d.netDialer().DialContext(ctx, "tcp", proxyAddress)
	if err != nil {
		return nil, fmt.Errorf("plugupgrade: dial HTTP proxy %s: %w", proxyURL.Redacted(), err)
	}
	return d.connectProxy(ctx, pConn, proxyURL, targetAddress)
}

func (d *Dialer) connectProxy(ctx context.Context, pConn net.Conn, proxyURL *url.URL, targetAddress string) (net.Conn, error) {
	baseConn := pConn
	stopCancel := context.AfterFunc(ctx, func() { baseConn.Close() })
	defer stopCancel()

	owned := true
	defer func() {
		if owned {
			pConn.Close()
		}
	}()

	if proxyURL.Scheme == "https" {
		config := d.tlsConfig(proxyURL.Hostname())
		tlsConn := tls.Client(pConn, config)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return nil, fmt.Errorf("plugupgrade: HTTP proxy TLS handshake: %w", err)
		}
		pConn = tlsConn
	}

	connectReq := &http.Request{
		Method: http.MethodConnect,
		URL: &url.URL{
			Opaque: targetAddress,
		},
		Host:       targetAddress,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
	}
	if proxyURL.User != nil {
		password, _ := proxyURL.User.Password()
		auth := proxyURL.User.Username() + ":" + password
		connectReq.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
	}

	if err := connectReq.Write(pConn); err != nil {
		return nil, fmt.Errorf("plugupgrade: write CONNECT to proxy %s: %w", proxyURL.Redacted(), err)
	}

	reader := bufio.NewReader(pConn)
	connectResp, err := http.ReadResponse(reader, connectReq)
	if err != nil {
		return nil, fmt.Errorf("plugupgrade: read CONNECT response from proxy %s: %w", proxyURL.Redacted(), err)
	}
	defer connectResp.Body.Close()

	if connectResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("plugupgrade: HTTP proxy %s rejected CONNECT with status %s", proxyURL.Redacted(), connectResp.Status)
	}
	if !stopCancel() || ctx.Err() != nil {
		return nil, ctx.Err()
	}

	owned = false
	if reader.Buffered() > 0 {
		return &bufferedConn{
			Conn:   pConn,
			reader: reader,
		}, nil
	}
	return pConn, nil
}

func (d *Dialer) netDialer() *net.Dialer {
	if d.NetDialer != nil {
		return d.NetDialer
	}
	return new(net.Dialer)
}

func (d *Dialer) tlsConfig(serverName string) *tls.Config {
	config := new(tls.Config)
	if d.TLSConfig != nil {
		config = d.TLSConfig.Clone()
	}
	if config.ServerName == "" {
		config.ServerName = serverName
	}
	config.NextProtos = []string{"http/1.1"}
	return config
}

// ProxyFromEnvironment selects a proxy from HTTP_PROXY, HTTPS_PROXY, ALL_PROXY,
// and NO_PROXY (or their lowercase forms). WebSocket URLs use HTTP proxy rules.
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
	port := target.Port()
	if port == "" {
		if target.Scheme == "https" || target.Scheme == "wss" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return target, net.JoinHostPort(target.Hostname(), port), nil
}
