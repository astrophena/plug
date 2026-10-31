// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"go.astrophena.name/base/version"
	"go.astrophena.name/plug"
	"go.astrophena.name/plug/internal/schema"
	"go.astrophena.name/plug/plugupgrade"
)

func connect(ctx context.Context, endpoint string, headers http.Header) (*session, error) {
	dial, err := dialer(endpoint, headers)
	if err != nil {
		return nil, err
	}

	conn, err := dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	remote, err := plug.Inspect(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("inspect: %w", err)
	}

	parsed, err := schema.Parse("remote.plug", []byte(remote.Source))
	if err != nil {
		return nil, fmt.Errorf("parse remote schema: %w", err)
	}
	sc := schema.RuntimeSchema(parsed)
	if sc.Family != remote.Family || sc.Fingerprint != remote.Fingerprint {
		return nil, fmt.Errorf("remote schema does not match its advertised hashes")
	}

	s := newSession(nil, &sc)
	s.dial = dial
	if err := s.reconnect(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *session) reconnect(ctx context.Context) error {
	conn, err := s.dial(ctx)
	if err != nil {
		return fmt.Errorf("reconnect: %w", err)
	}
	client, err := plug.NewClient(ctx, conn, s.schema, plug.ClientOptions{
		StrictSchema: true,
	})
	if err != nil {
		return fmt.Errorf("handshake: %w", err)
	}
	s.client = client
	return nil
}

var errOverrideUA = errors.New("overriding User-Agent header is not allowed")

func dialer(endpoint string, headers http.Header) (func(context.Context) (net.Conn, error), error) {
	// Normalize native addresses before URL parsing treats host:port as a scheme.
	switch {
	case strings.HasPrefix(endpoint, "/"):
		endpoint = (&url.URL{
			Scheme: "unix",
			Path:   endpoint,
		}).String()
	case !strings.Contains(endpoint, "://"):
		_, port, err := net.SplitHostPort(endpoint)
		if err != nil {
			return nil, fmt.Errorf("TCP endpoint: %w", err)
		}
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			return nil, fmt.Errorf("TCP endpoint port: %w", err)
		}
		endpoint = (&url.URL{
			Scheme: "tcp",
			Host:   endpoint,
		}).String()
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	if u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return nil, fmt.Errorf("endpoint must not contain credentials, a fragment, or an opaque address")
	}

	var network, address string
	switch u.Scheme {
	case "http", "https", "ws", "wss":
		if u.Host == "" {
			return nil, fmt.Errorf("HTTP endpoint requires a host")
		}

		hdrs := headers.Clone()
		if hdrs == nil {
			hdrs = make(http.Header)
		}
		if hdrs.Get("User-Agent") != "" {
			return nil, errOverrideUA
		}
		hdrs.Set("User-Agent", version.UserAgent())

		d := &plugupgrade.Dialer{
			Header: hdrs,
		}
		return func(ctx context.Context) (net.Conn, error) { return d.DialContext(ctx, endpoint) }, nil
	case "unix":
		if u.Host != "" || !strings.HasPrefix(u.Path, "/") || u.Path == "/" || u.RawQuery != "" {
			return nil, fmt.Errorf("unix endpoint must be unix:///absolute/socket/path")
		}
		network, address = "unix", u.Path
	case "tcp":
		if u.Host == "" || u.Path != "" || u.RawQuery != "" {
			return nil, fmt.Errorf("TCP endpoint must be tcp://host:port")
		}
		if _, _, err := net.SplitHostPort(u.Host); err != nil {
			return nil, fmt.Errorf("TCP endpoint: %w", err)
		}
		network, address = "tcp", u.Host
	default:
		return nil, fmt.Errorf("unsupported endpoint scheme %q; use unix, tcp, http, https, ws, or wss", u.Scheme)
	}

	if len(headers) != 0 {
		return nil, fmt.Errorf("HTTP headers require an http, https, ws, or wss endpoint")
	}
	return func(ctx context.Context) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, address)
	}, nil
}
