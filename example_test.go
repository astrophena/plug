// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/example/greeter"
)

func ExampleNewServer() {
	server, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{
		Security: plug.Security{
			Authenticate: func(context.Context, net.Conn) (plug.Principal, error) {
				return plug.Principal{
					Subject: "local-peer",
				}, nil
			},
			Authorize: func(_ context.Context, _ plug.Principal, permission string, _ plug.MethodInfo, _ any) error {
				if permission != greeter.PermissionUserRead {
					return fmt.Errorf("unknown permission %q", permission)
				}
				return nil
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := greeter.RegisterHandlers(server, greeter.Handlers{
		GetUser: func(_ context.Context, req greeter.GetUserRequest) (greeter.User, error) {
			return greeter.User{
				ID:   req.ID,
				Name: "Ada",
				Role: &greeter.Guest{},
			}, nil
		},
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) { return nil, nil },
		Ping:      func(context.Context) error { return nil },
	}); err != nil {
		log.Fatal(err)
	}
	listener, err := net.Listen("unix", "/run/greeter/plug.sock")
	if err != nil {
		log.Fatal(err)
	}
	if err := server.Serve(context.Background(), listener); err != nil {
		log.Fatal(err)
	}
}

func ExampleDial() {
	ctx := context.Background()
	c, err := plug.Dial(ctx, "unix", "/run/greeter/plug.sock", &greeter.Schema, plug.ClientOptions{})
	if err != nil {
		log.Print(err)
		return
	}
	defer c.Close()
	user, err := greeter.NewClient(c).GetUser(ctx, greeter.GetUserRequest{
		ID: 42,
	})
	if err != nil {
		if notFound, ok := errors.AsType[*greeter.NotFound](err); ok {
			log.Printf("user %d was not found", notFound.ID)
		} else {
			log.Print(err)
		}
		return
	}
	log.Printf("received %s", user.Name)
}

func ExampleInspect() {
	conn, err := net.Dial("unix", "/run/greeter/plug.sock")
	if err != nil {
		log.Print(err)
		return
	}
	remote, err := plug.Inspect(context.Background(), conn)
	if err != nil {
		log.Print(err)
		return
	}
	log.Printf("wire version %d, schema family %x", remote.Version, remote.Family)
}
