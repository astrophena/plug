// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plugupgrade

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const (
	pingInterval = 30 * time.Second
	pingTimeout  = 10 * time.Second
)

func (d *Dialer) dialWS(ctx context.Context, target *url.URL) (net.Conn, error) {
	// Proxy selection uses HTTP schemes for both transports.
	if target.Scheme == "wss" {
		target.Scheme = "https"
	} else {
		target.Scheme = "http"
	}

	tr := &http.Transport{
		// Bind proxy CONNECT to ctx; http.Transport detaches its dial context.
		DialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			return d.dialConn(ctx, target, address)
		},
		TLSClientConfig: d.tlsConfig(target.Hostname()),
	}
	defer tr.CloseIdleConnections()

	client := &http.Client{
		Transport: tr,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	ws, resp, err := websocket.Dial(ctx, target.String(), &websocket.DialOptions{
		HTTPClient: client,
		HTTPHeader: d.Header,
	})
	if err != nil {
		if resp != nil && resp.Body != nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if detail := strings.TrimSpace(string(body)); detail != "" {
				err = fmt.Errorf("%w: %s", err, detail)
			}
		}
		return nil, fmt.Errorf("plugupgrade: dial WebSocket: %w", err)
	}
	if err := ctx.Err(); err != nil {
		ws.CloseNow()
		return nil, err
	}
	return newWSConn(context.WithoutCancel(ctx), ws), nil
}

type wsConn struct {
	net.Conn
	cancel context.CancelFunc
	done   chan struct{}
}

func newWSConn(ctx context.Context, ws *websocket.Conn) *wsConn {
	ctx, cancel := context.WithCancel(ctx)
	c := &wsConn{
		Conn:   websocket.NetConn(ctx, ws, websocket.MessageBinary),
		cancel: cancel,
		done:   make(chan struct{}),
	}

	go func() {
		defer close(c.done)
		keepAlive(ctx, ws)
	}()
	return c
}

func (c *wsConn) Close() error {
	c.cancel()
	<-c.done

	err := c.Conn.Close()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func keepAlive(ctx context.Context, ws *websocket.Conn) {
	defer ws.CloseNow()
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
			err := ws.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}
