// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug_test

import (
	"testing"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/example/greeter"
	"go.astrophena.name/plug/plugimpl"
)

func TestServerOwnsSchema(t *testing.T) {
	schema := plugimpl.CopySchema(&greeter.Schema)
	server, err := plug.NewServer(schema, plug.ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Shutdown(t.Context()) })

	schema.Categories[0].Types[0] = "changed"
	if server.Schema().Categories[0].Types[0] == "changed" {
		t.Fatal("input changed server categories")
	}

	for i, method := range schema.Methods {
		if method.Name != "greeter.listUsers" {
			continue
		}

		schema.Methods[i].Response.Element.Name = "changed input"
		snapshot := server.Schema()
		snapshot.Categories[0].Types[0] = "changed snapshot"
		if server.Schema().Categories[0].Types[0] == "changed snapshot" {
			t.Fatal("snapshot changed server categories")
		}
		if got := snapshot.Methods[i].Response.Element.Name; got != "User" {
			t.Fatalf("input changed server response type to %q", got)
		}

		snapshot.Methods[i].Response.Element.Name = "changed snapshot"
		snapshot.Methods[i].Fields[0].Name = "changed field"
		snapshot.Methods[i].HTTP[0].Path = "/changed"
		next := server.Schema().Methods[i]
		if next.Response.Element.Name != "User" || next.Fields[0].Name != method.Fields[0].Name || next.HTTP[0].Path != method.HTTP[0].Path {
			t.Fatalf("snapshot changed server method: %+v", next)
		}
		return
	}
	t.Fatal("greeter.listUsers is absent from schema")
}

func TestCopySchemaOwnsFieldTypes(t *testing.T) {
	expr := plug.TypeExpr{
		Kind: plug.TypeExprList,
		Element: &plug.TypeExpr{
			Kind: plug.TypeExprNamed,
			Name: "Item",
		},
	}
	schema := &plug.Schema{
		Methods: []plug.MethodInfo{
			{
				Fields: []plug.FieldInfo{
					{
						Type: expr,
					},
				},
			},
		},
		Types: []plug.TypeInfo{
			{
				Constructors: []plug.ConstructorInfo{
					{
						Fields: []plug.FieldInfo{
							{
								Type: expr,
							},
						},
					},
				},
			},
		},
	}

	copy := plugimpl.CopySchema(schema)
	copy.Methods[0].Fields[0].Type.Element.Name = "changed request"
	copy.Types[0].Constructors[0].Fields[0].Type.Element.Name = "changed record"
	if got := expr.Element.Name; got != "Item" {
		t.Fatalf("copy changed source field type to %q", got)
	}
}
