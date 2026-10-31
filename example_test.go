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
	opts := plug.ServerOptions{
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
	}

	server, err := plug.NewServer(&greeter.Schema, opts)
	if err != nil {
		log.Fatal(err)
	}

	if err := greeter.RegisterHandlers(server, greeter.Handlers{
		GetUser: func(_ context.Context, request greeter.GetUserRequest) (greeter.User, error) {
			if request.ID == 0 {
				return greeter.User{}, &greeter.NotFound{
					ID: request.ID,
				}
			}
			return greeter.User{
				ID:   request.ID,
				Name: "Ada",
				Role: &greeter.Guest{},
			}, nil
		},
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) {
			return []greeter.User{}, nil
		},
		Ping: func(context.Context) error {
			return nil
		},
	}); err != nil {
		log.Fatal(err)
		return
	}

	lis, err := net.Listen("unix", "/run/greeter/plug.sock")
	if err != nil {
		log.Fatal(err)
	}
	if err := server.Serve(context.Background(), lis); err != nil {
		log.Fatal(err)
	}
}

func ExampleDial() {
	c, err := plug.Dial(context.Background(), "unix", "/run/greeter/plug.sock", &greeter.Schema, plug.ClientOptions{})
	if err != nil {
		log.Print(err)
		return
	}
	defer func() {
		if err := c.Close(); err != nil {
			log.Fatalf("close Plug client: %v", err)
		}
	}()

	client := greeter.NewClient(c)

	user, err := client.GetUser(context.Background(), greeter.GetUserRequest{
		ID: 42,
	})
	if err != nil {
		if notFound, ok := errors.AsType[*greeter.NotFound](err); ok {
			log.Fatalf("user %d was not found", notFound.ID)
		}
		log.Fatal(err)
	}

	log.Printf("received %s", user.Name)
}

func ExampleInspect() {
	conn, err := net.Dial("unix", "/run/greeter/plug.sock")
	if err != nil {
		log.Fatal(err)
	}

	remote, err := plug.Inspect(context.Background(), conn)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("server uses Plug wire version %d and schema family %x", remote.Version, remote.Family)
}
