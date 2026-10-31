// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plughttp_test

import (
	"context"
	"log"
	"net/http"

	"go.astrophena.name/base/web"
	"go.astrophena.name/plug"
	"go.astrophena.name/plug/example/greeter"
	"go.astrophena.name/plug/plughttp"
)

func ExampleNew() {
	server, err := exampleServer()
	if err != nil {
		log.Print(err)
		return
	}
	api, err := plughttp.New(server, greeter.HTTPRoutes(server), plughttp.Options{
		Authenticate: func(*http.Request) (plug.Principal, error) {
			return plug.Principal{
				Subject: "http-client",
			}, nil
		},
	})
	if err != nil {
		log.Print(err)
		return
	}

	mux := http.NewServeMux()
	mux.Handle("/", api)
	if err := http.ListenAndServe("localhost:8080", mux); err != nil {
		log.Print(err)
	}
}

func ExampleNewBrowser() {
	documentation, err := plughttp.NewBrowser(&greeter.Schema)
	if err != nil {
		log.Print(err)
		return
	}

	mux := http.NewServeMux()
	mux.Handle("/docs/", http.StripPrefix("/docs", documentation))
	server := web.Server{
		Addr:     "localhost:8080",
		Mux:      mux,
		StaticFS: plughttp.StaticFS(),
	}
	if err := server.ListenAndServe(context.Background()); err != nil {
		log.Print(err)
	}
}

func exampleServer() (*plug.Server, error) {
	srv, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{
		Security: plug.Security{
			Authorize: func(context.Context, plug.Principal, string, plug.MethodInfo, any) error {
				return nil
			},
		},
	})
	if err != nil {
		return nil, err
	}

	if err := greeter.RegisterHandlers(srv, greeter.Handlers{
		GetUser: func(_ context.Context, request greeter.GetUserRequest) (greeter.User, error) {
			return greeter.User{
				ID:   request.ID,
				Name: "Ada",
				Role: &greeter.Guest{},
			}, nil
		},
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) {
			return []greeter.User{}, nil
		},
		Ping: func(context.Context) error { return nil },
	}); err != nil {
		return nil, err
	}

	return srv, nil
}
