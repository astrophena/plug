// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"fmt"
	"go/format"
	"strconv"
	"strings"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/internal/doc"
	"go.astrophena.name/plug/internal/names"
)

func formatGo(source []byte, target string) ([]byte, error) {
	formatted, err := format.Source(source)
	if err != nil {
		return nil, fmt.Errorf("formatting generated %s: %w\n%s", target, err, source)
	}
	return formatted, nil
}

// goGenerator emits declarations shared by the native and standalone Go
// generators.
type goGenerator struct {
	model
	emitter
	optPrefix string
	native    bool
}

func newGoGenerator(m model, native bool) goGenerator {
	optPrefix := ""
	if native {
		optPrefix = "plug."
	}
	return goGenerator{
		model:     m,
		indent:    "\t",
		docs:      doc.NewParser(&m.schema),
		optPrefix: optPrefix,
		native:    native,
	}
}

func (g *goGenerator) goType(ref plug.TypeExpr) string {
	var base string
	if ref.Element != nil {
		base = "[]" + g.goType(*ref.Element)
	} else if ref.Kind == plug.TypeExprPrimitive {
		base = primitiveGoType(ref.Name)
	} else {
		base = names.Go(ref.Name)
	}

	if ref.Optional {
		base = g.optPrefix + "Option[" + base + "]"
	}
	return base
}

func (g *goGenerator) typeContainsSum(ref plug.TypeExpr) bool {
	if ref.Element != nil {
		return g.typeContainsSum(*ref.Element)
	}
	return len(g.types[ref.Name].Constructors) > 1
}

func (g *goGenerator) writeStruct(name string, fields []plug.FieldInfo) {
	if len(fields) == 0 {
		g.line("type %s struct{}", name)
		g.line("")
		return
	}

	g.line("type %s struct {", name)
	for _, f := range fields {
		var (
			fieldName = names.Go(f.Name)
			tag       = "`json:\"" + f.Name
		)
		if f.Type.Optional {
			tag += ",omitzero"
		}
		tag += "\"`"

		g.docIndented(fieldName, f.Name, fmt.Sprintf("is the %s schema field.", f.Name), f.Documentation)
		g.line("\t%s %s %s", fieldName, g.goType(f.Type), tag)
	}
	g.line("}")
	g.line("")
}

func (g *goGenerator) writeErrorMethod(goName, ctorName string, fields []plug.FieldInfo) {
	if len(fields) == 0 {
		g.line("func (*%s) Error() string { return %q }", goName, ctorName)
		return
	}

	g.line("func (e *%s) Error() string {", goName)

	var (
		format strings.Builder
		args   []string
	)
	format.WriteString(ctorName)
	format.WriteString(":")
	for _, f := range fields {
		format.WriteString(" ")
		format.WriteString(f.Name)
		format.WriteString("=%v")
		args = append(args, "e."+names.Go(f.Name))
	}

	g.line("\treturn fmt.Sprintf(%s, %s)", strconv.Quote(format.String()), strings.Join(args, ", "))
	g.line("}")
}

func (g *goGenerator) writeRequests() {
	for _, m := range g.methods {
		if len(m.Fields) == 0 {
			continue
		}
		name := names.GoMethod(m.Name) + "Request"
		g.line("// %s contains the request fields for %s.", name, m.Name)
		g.writeStruct(name, m.Fields)
	}
}

func (g *goGenerator) writeTypes() {
	for _, nt := range g.newtypes {
		goName := names.Go(nt.Name)
		g.doc(goName, nt.Name, fmt.Sprintf("is the distinct Plug scalar %s.", nt.Name), nt.Documentation)
		g.line("type %s %s", goName, primitiveGoType(nt.Underlying))
		g.line("")
	}

	for _, name := range g.groupNames {
		ctors := g.types[name].Constructors
		if len(ctors) == 1 {
			var (
				c      = ctors[0]
				goName = names.Go(name)
			)

			g.doc(goName, c.Name, fmt.Sprintf("is the Plug record %s.", name), c.Documentation)
			g.writeStruct(goName, c.Fields)

			if g.types[name].Error {
				g.line("// Error returns the constructor name and fields.")
				g.writeErrorMethod(goName, c.Name, c.Fields)
				g.line("")
			}
			continue
		}

		var (
			goName = names.Go(name)
			sumDoc = g.types[name].Documentation
		)

		g.doc(goName, name, fmt.Sprintf("is the sealed Plug sum %s.", name), sumDoc)
		g.line("type %s interface {", goName)
		g.line("\tplug%s()", goName)
		if g.types[name].Error {
			g.line("\terror")
		}
		g.line("}")
		g.line("")

		for _, c := range ctors {
			variant := names.Go(c.Name)

			g.doc(variant, c.Name, fmt.Sprintf("is the %s variant of %s.", c.Name, name), c.Documentation)
			g.writeStruct(variant, c.Fields)
			g.line("func (*%s) plug%s() {}", variant, goName)
			g.line("")

			if g.types[name].Error {
				g.line("// Error returns the constructor name and fields.")
				g.writeErrorMethod(variant, c.Name, c.Fields)
				g.line("")
			}
		}

		unknown := "Unknown" + goName
		g.line("// %s preserves a sum constructor unknown to this schema generation.", unknown)
		g.line("type %s struct {", unknown)
		if g.native {
			g.line("\t// ConstructorID is the unknown wire constructor ID.")
			g.line("\tConstructorID uint32")
			g.line("\t// Payload is the uninterpreted constructor body.")
			g.line("\tPayload []byte")
		} else {
			g.line("\t// Constructor is the unfamiliar JSON constructor name.")
			g.line("\tConstructor string")
			g.line("\t// Payload is the complete JSON object received from the server.")
			g.line("\tPayload jsontext.Value")
		}
		g.line("}")
		g.line("")
		g.line("func (*%s) plug%s() {}", unknown, goName)
		g.line("")

		if g.types[name].Error {
			g.line("// Error reports an unknown constructor.")
			if g.native {
				g.line("func (*%s) Error() string { return %q }", unknown, "unknown "+name)
			} else {
				g.line("func (u *%s) Error() string {", unknown)
				g.line("\treturn fmt.Sprintf(\"unknown %s constructor %%q\", u.Constructor)", name)
				g.line("}")
			}
			g.line("")
		}
	}
}

type methodShape struct {
	name      string
	request   string
	response  string
	raw       bool
	void      bool
	noRequest bool
}

func (m methodShape) registrationFunc() string {
	switch {
	case m.raw && m.noRequest:
		return "AppendRawHandlerNoReq"
	case m.raw:
		return "AppendRawHandler"
	case m.noRequest && m.void:
		return "AppendHandlerNoReqVoid"
	case m.noRequest:
		return "AppendHandlerNoReq"
	case m.void:
		return "AppendHandlerVoid"
	default:
		return "AppendHandler"
	}
}

func (g *goGenerator) methodShape(m plug.MethodInfo) methodShape {
	var (
		name     = names.GoMethod(m.Name)
		raw      = isRaw(m.Response)
		response string
	)
	if !raw {
		response = g.goType(m.Response)
	}

	request := name + "Request"
	if len(m.Fields) == 0 {
		request = "plugimpl.Void"
	}

	return methodShape{
		name:      name,
		request:   request,
		response:  response,
		raw:       raw,
		void:      m.Response.Name == "void" && m.Response.Element == nil && !m.Response.Optional,
		noRequest: len(m.Fields) == 0,
	}
}

func primitiveGoType(name string) string {
	switch name {
	case "bool":
		return "bool"
	case "bytes":
		return "[]byte"
	case "f64":
		return "float64"
	case "i32":
		return "int32"
	case "i64":
		return "int64"
	case "string":
		return "string"
	case "u32":
		return "uint32"
	case "u64":
		return "uint64"
	case "void":
		return "plugimpl.Void"
	default:
		panic("unknown primitive " + name)
	}
}
