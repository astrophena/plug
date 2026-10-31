// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"fmt"
	"slices"
	"strconv"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/internal/names"
)

func (g *generator) writeMethods() {
	for _, m := range g.methods {
		var (
			goName       = names.GoMethod(m.Name)
			name         = methodName(m.Name)
			errorsCodec  = "plugimpl.ErrorCodec{}"
			requestCodec = codecName(goName + "Request")
		)
		if m.Error != "" {
			errorsCodec = errorCodecName(m.Error)
		}
		if len(m.Fields) == 0 {
			requestCodec = "plugimpl.VoidCodec"
		}

		if isRaw(m.Response) {
			g.line("var %s = plugimpl.NewRawMethod(", name)
			g.line("\t&Schema,")
			g.line("\t%s,", methodIDName(m.Name))
			g.line("\t%s,", requestCodec)
			g.line("\t%s,", errorsCodec)
			g.line(")")
			g.line("")
			continue
		}

		g.line("var %s = plugimpl.NewMethod(", name)
		g.line("\t&Schema,")
		g.line("\t%s,", methodIDName(m.Name))
		g.line("\t%s,", requestCodec)
		g.line("\t%s,", g.codec(m.Response))
		g.line("\t%s,", errorsCodec)
		g.line(")")
		g.line("")
	}
}

func (g *generator) writeHandlers() {
	g.line("// Handlers contains the service method handlers.")
	g.line("type Handlers struct {")
	for _, m := range g.methods {
		shape := g.methodShape(m)
		g.line("\t// %s handles %s.", shape.name, m.Name)
		if shape.raw {
			if shape.noRequest {
				g.line("\t%s func(context.Context, *plug.RawStream) error", shape.name)
			} else {
				g.line("\t%s func(context.Context, %s, *plug.RawStream) error", shape.name, shape.request)
			}
			continue
		}
		if shape.noRequest && shape.void {
			g.line("\t%s func(context.Context) error", shape.name)
		} else if shape.noRequest {
			g.line("\t%s func(context.Context) (%s, error)", shape.name, shape.response)
		} else if shape.void {
			g.line("\t%s func(context.Context, %s) error", shape.name, shape.request)
		} else {
			g.line("\t%s func(context.Context, %s) (%s, error)", shape.name, shape.request, shape.response)
		}
	}
	g.line("}")
	g.line("")

	g.line("// RegisterHandlers registers every handler with server atomically. All fields must be non-nil.")
	g.line("func RegisterHandlers(server *plug.Server, h Handlers) error {")
	g.line("\tbatch := make([]plugimpl.Registration, 0, %d)", len(g.methods))
	for _, m := range g.methods {
		shape := g.methodShape(m)
		g.line("\tif err := plugimpl.%s(&batch, %s, h.%s, %q); err != nil {", shape.registrationFunc(), methodName(m.Name), shape.name, shape.name)
		g.line("\t\treturn err")
		g.line("\t}")
	}
	g.line("")

	g.line("\treturn server.RegisterHandlers(batch)")
	g.line("}")
	g.line("")
}

func (g *generator) writeClient() {
	g.line("// Client is the typed %s client.", g.schema.Package)
	g.line("type Client struct {")
	g.line("\traw *plug.Client")
	g.line("}")
	g.line("")

	g.line("// NewClient returns a typed wrapper around raw.")
	g.line("func NewClient(raw *plug.Client) *Client { return &Client{raw: raw} }")
	g.line("")

	g.line("// Close closes the underlying Plug client.")
	g.line("func (c *Client) Close() error { return c.raw.Close() }")
	g.line("")

	for _, m := range g.methods {
		shape := g.methodShape(m)
		if shape.raw {
			g.doc("Open"+shape.name, m.Name, fmt.Sprintf("opens a dedicated connection for %s.", m.Name), m.Documentation)
			if shape.noRequest {
				g.line("func Open%s(ctx context.Context, conn net.Conn, opts plug.ClientOptions) (plug.RawConn, error) {", shape.name)
				g.line("\treturn plug.OpenRaw(ctx, conn, %s, plugimpl.Void{}, opts)", methodName(m.Name))
			} else {
				g.line("func Open%s(ctx context.Context, conn net.Conn, req %s, opts plug.ClientOptions) (plug.RawConn, error) {", shape.name, shape.request)
				g.line("\treturn plug.OpenRaw(ctx, conn, %s, req, opts)", methodName(m.Name))
			}
			g.line("}")
			g.line("")
			continue
		}

		g.doc(shape.name, m.Name, fmt.Sprintf("calls %s.", m.Name), m.Documentation)
		if shape.noRequest && shape.void {
			g.line("func (c *Client) %s(ctx context.Context) error {", shape.name)
			g.line("\t_, err := plug.Call(ctx, c.raw, %s, plugimpl.Void{})", methodName(m.Name))
			g.line("\treturn err")
			g.line("}")
		} else if shape.noRequest {
			g.line("func (c *Client) %s(ctx context.Context) (%s, error) {", shape.name, shape.response)
			g.line("\treturn plug.Call(ctx, c.raw, %s, plugimpl.Void{})", methodName(m.Name))
			g.line("}")
		} else if shape.void {
			g.line("func (c *Client) %s(ctx context.Context, req %s) error {", shape.name, shape.request)
			g.line("\t_, err := plug.Call(ctx, c.raw, %s, req)", methodName(m.Name))
			g.line("\treturn err")
			g.line("}")
		} else {
			g.line("func (c *Client) %s(ctx context.Context, req %s) (%s, error) {", shape.name, shape.request, shape.response)
			g.line("\treturn plug.Call(ctx, c.raw, %s, req)", methodName(m.Name))
			g.line("}")
		}
		g.line("")
	}
}

func (g *generator) hasRawMethods() bool {
	return slices.ContainsFunc(g.schema.Methods, func(m plug.MethodInfo) bool {
		return isRaw(m.Response)
	})
}

func (g *generator) writeHTTPRoutes() {
	if !g.hasHTTP {
		return
	}

	g.line("// HTTPRoutes returns every HTTP binding declared by the schema.")
	g.line("func HTTPRoutes(server *plug.Server) []plughttp.Route {")
	g.line("\treturn []plughttp.Route{")
	for _, m := range g.methods {
		for _, b := range m.HTTP {
			g.line("\t\tplughttp.NewRoute(server, %s, plughttp.Binding{", methodName(m.Name))
			g.line("\t\t\tVerb: %s,", strconv.Quote(b.Verb))
			g.line("\t\t\tPath: %s,", strconv.Quote(b.Path))
			g.line("\t\t}),")
		}
	}
	g.line("\t}")
	g.line("}")
	g.line("")
}

func codecName(name string) string { return "codec" + names.Go(name) }

func errorCodecName(name string) string { return "errors" + names.Go(name) }

func constructorIDName(name string) string { return "constructorID" + names.Go(name) }

func methodIDName(name string) string { return "methodID" + names.GoMethod(name) }

func methodName(name string) string { return "method" + names.GoMethod(name) }
