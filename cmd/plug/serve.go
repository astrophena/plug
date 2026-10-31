// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/web"
	"go.astrophena.name/plug/internal/schema"
	"go.astrophena.name/plug/plughttp"
)

func runDoc(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("plug doc", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "localhost:8080", "listen on host:port or Unix socket")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%w: usage: plug doc [-addr ADDRESS] SCHEMA.plug", cli.ErrInvalidArgs)
	}

	server, err := docServer(fs.Arg(0), *addr)
	if err != nil {
		return err
	}
	return server.ListenAndServe(ctx)
}

func docServer(path, addr string) (*web.Server, error) {
	s, err := readSchema(path)
	if err != nil {
		return nil, err
	}

	meta := schema.RuntimeSchema(s)
	h, err := plughttp.NewBrowser(&meta)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("/", h)
	return &web.Server{
		Addr:     addr,
		Mux:      mux,
		StaticFS: plughttp.StaticFS(),
	}, nil
}
