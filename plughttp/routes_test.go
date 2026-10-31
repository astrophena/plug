// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plughttp

import (
	"context"
	"testing"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/plugimpl"
)

func TestConflictingRoutes(t *testing.T) {
	for name, paths := range map[string][]string{
		"invalid":    {"/users/prefix{id}"},
		"equivalent": {"/users/{id}", "/users/{name}"},
		"overlap":    {"/users/{id}", "/{name}/settings"},
	} {
		t.Run(name, func(t *testing.T) {
			var bindings []Binding
			for _, path := range paths {
				bindings = append(bindings, Binding{
					Verb: "GET",
					Path: path,
				})
			}
			schema := plug.Schema{
				Package: "routes",
				Major:   1,
				Family:  plugimpl.FamilyFingerprint("routes", 1),
				Source:  "package routes version 1",
				Methods: []plug.MethodInfo{
					{
						ID:     1,
						Name:   "routes.call",
						Public: true,
						HTTP:   bindings,
					},
				},
			}
			s, err := plug.NewServer(&schema, plug.ServerOptions{})
			if err != nil {
				t.Fatal(err)
			}
			method := plugimpl.NewMethod(&schema, 1, plugimpl.VoidCodec, plugimpl.VoidCodec, plugimpl.ErrorCodec{})
			var batch []plugimpl.Registration
			if err := plugimpl.AppendHandler(&batch, method, func(context.Context, plugimpl.Void) (plugimpl.Void, error) { return plugimpl.Void{}, nil }, "Call"); err != nil {
				t.Fatal(err)
			}
			if err := s.RegisterHandlers(batch); err != nil {
				t.Fatal(err)
			}
			var routes []Route
			for _, binding := range bindings {
				routes = append(routes, NewRoute(s, method, binding))
			}
			if _, err := New(s, routes, Options{}); err == nil {
				t.Fatal("New accepted invalid or conflicting routes")
			}
		})
	}
}
