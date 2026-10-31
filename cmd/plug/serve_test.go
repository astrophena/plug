// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/cli/clitest"
)

func TestDocServer(t *testing.T) {
	server, err := docServer("../../example/greeter/schema.plug", filepath.Join(t.TempDir(), "web.sock"))
	if err != nil {
		t.Fatal(err)
	}

	var (
		ready = make(chan struct{})
		done  = make(chan error, 1)
	)
	server.Ready = func() { close(ready) }
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go func() { done <- server.ListenAndServe(ctx) }()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("start documentation server: %v", err)
	case <-ctx.Done():
		cancel()
		<-done
		t.Fatal(ctx.Err())
	}
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return new(net.Dialer).DialContext(ctx, "unix", server.Addr)
		},
	}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{
		Transport: transport,
	}
	cases := map[string]string{
		"/methods/greeter.getUser":                                "greeter.getUser returns the requested user.",
		"/" + server.StaticHashName("static/css/plug-schema.css"): "--blue",
	}
	for path, want := range cases {
		response, err := client.Get("http://example.test" + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK || !strings.Contains(string(body), want) {
			t.Errorf("GET %s = %d %s", path, response.StatusCode, body)
		}
	}
}

func TestDocUsage(t *testing.T) {
	clitest.Run(t, func(*testing.T) cli.AppFunc { return run }, map[string]clitest.Case[cli.AppFunc]{
		"missing schema": {
			Args:    []string{"doc"},
			WantErr: cli.ErrInvalidArgs,
		},
		"extra schema": {
			Args:    []string{"doc", "a.plug", "b.plug"},
			WantErr: cli.ErrInvalidArgs,
		},
		"missing file": {
			Args:    []string{"doc", filepath.Join(t.TempDir(), "missing.plug")},
			WantErr: os.ErrNotExist,
		},
	})
}

func TestDocInvalidSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.plug")
	if err := os.WriteFile(path, []byte("package example\nexample.ping -> Missing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runApp(t.Context(), []string{"doc", path}, io.Discard); err == nil || errors.Is(err, cli.ErrInvalidArgs) {
		t.Fatalf("invalid schema: %v", err)
	}
}
