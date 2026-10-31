// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"fmt"
	"strconv"
	"strings"

	"go.astrophena.name/plug"
)

func (g *generator) writeSchema() {
	s := g.schema
	g.line("// Schema describes the generated Plug service.")
	g.line("var Schema = plug.Schema{")
	g.line("\tPackage: %s,", strconv.Quote(s.Package))
	g.line("\tMajor: %d,", s.Major)
	g.line("\tFamily: plugimpl.FamilyFingerprint(%s, %d),", strconv.Quote(s.Package), s.Major)
	g.writeSchemaDoc("\t", s.Documentation)
	g.line("\tFingerprint: [32]byte{%s},", byteLiteral(s.Fingerprint[:]))
	g.line("\tSource: %s,", strconv.Quote(s.Source))
	g.line("\tPermissions: []plug.PermissionInfo{")
	for _, p := range s.Permissions {
		g.line("\t\t{")
		g.line("\t\t\tName: %s,", strconv.Quote(p.Name))
		g.writeSchemaDoc("\t\t\t", p.Documentation)
		g.line("\t\t},")
	}
	g.line("\t},")

	if len(s.Categories) > 0 {
		g.line("\tCategories: []plug.CategoryInfo{")
		for _, cat := range s.Categories {
			g.line("\t\t{")
			g.line("\t\t\tName: %s,", strconv.Quote(cat.Name))
			g.writeSchemaDoc("\t\t\t", cat.Documentation)
			if len(cat.Types) > 0 {
				g.line("\t\t\tTypes: []string{%s},", quotedStrings(cat.Types))
			}
			if len(cat.Methods) > 0 {
				g.line("\t\t\tMethods: []string{%s},", quotedStrings(cat.Methods))
			}
			g.line("\t\t},")
		}
		g.line("\t},")
	}

	g.line("\tTypes: []plug.TypeInfo{")
	for _, typ := range s.Types {
		g.line("\t\t{")
		g.line("\t\t\tName: %s,", strconv.Quote(typ.Name))
		g.writeSchemaDoc("\t\t\t", typ.Documentation)
		g.line("\t\t\tKind: %s,", typeKindLiteral(typ.Kind))
		if typ.Underlying != "" {
			g.line("\t\t\tUnderlying: %s,", strconv.Quote(typ.Underlying))
		}
		if typ.Error {
			g.line("\t\t\tError: true,")
		}
		if len(typ.Constructors) > 0 {
			g.line("\t\t\tConstructors: []plug.ConstructorInfo{")
			for _, c := range typ.Constructors {
				g.line("\t\t\t\t{")
				g.line("\t\t\t\t\tName: %s,", strconv.Quote(c.Name))
				g.writeSchemaDoc("\t\t\t\t\t", c.Documentation)
				g.line("\t\t\t\t\tID: 0x%08x,", c.ID)
				if c.HTTPStatus != 0 {
					g.line("\t\t\t\t\tHTTPStatus: %d,", c.HTTPStatus)
				}
				g.writeFieldInfo(c.Fields, "\t\t\t\t\t")
				g.line("\t\t\t\t},")
			}
			g.line("\t\t\t},")
		}
		g.line("\t\t},")
	}
	g.line("\t},")

	g.line("\tMethods: []plug.MethodInfo{")
	for _, m := range s.Methods {
		g.line("\t\t{")
		g.line("\t\t\tName: %s,", strconv.Quote(m.Name))
		g.writeSchemaDoc("\t\t\t", m.Documentation)
		g.line("\t\t\tID: 0x%08x,", m.ID)
		g.writeFieldInfo(m.Fields, "\t\t\t")
		g.line("\t\t\tResponse: %s,", typeExprLiteral(m.Response))
		if m.Error != "" {
			g.line("\t\t\tError: %s,", strconv.Quote(m.Error))
		}
		if m.Public {
			g.line("\t\t\tPublic: true,")
		}
		if m.Permission != "" {
			g.line("\t\t\tPermission: %s,", strconv.Quote(m.Permission))
		}
		if len(m.HTTP) > 0 {
			g.line("\t\t\tHTTP: []plug.HTTPBindingInfo{")
			for _, b := range m.HTTP {
				g.line("\t\t\t\t{")
				g.line("\t\t\t\t\tVerb: %s,", strconv.Quote(b.Verb))
				g.line("\t\t\t\t\tPath: %s,", strconv.Quote(b.Path))
				g.line("\t\t\t\t},")
			}
			g.line("\t\t\t},")
		}
		g.line("\t\t},")
	}
	g.line("\t},")
	g.line("}")
	g.line("")
}

func typeKindLiteral(kind plug.TypeKind) string {
	switch kind {
	case plug.TypeNewtype:
		return "plug.TypeNewtype"
	case plug.TypeRecord:
		return "plug.TypeRecord"
	case plug.TypeSum:
		return "plug.TypeSum"
	default:
		panic(fmt.Sprintf("unknown Plug type kind %d", kind))
	}
}

func (g *generator) writeFieldInfo(fields []plug.FieldInfo, indent string) {
	if len(fields) == 0 {
		return
	}

	g.line("%sFields: []plug.FieldInfo{", indent)
	for _, f := range fields {
		g.line("%s\t{", indent)
		g.line("%s\t\tName: %s,", indent, strconv.Quote(f.Name))
		g.writeSchemaDoc(indent+"\t\t", f.Documentation)
		g.line("%s\t\tType: %s,", indent, typeExprLiteral(f.Type))
		g.line("%s\t},", indent)
	}
	g.line("%s},", indent)
}

func (g *generator) writeSchemaDoc(indent, text string) {
	if text != "" {
		g.line("%sDocumentation: %s,", indent, strconv.Quote(text))
	}
}

func typeExprLiteral(expr plug.TypeExpr) string {
	var value string
	switch expr.Kind {
	case plug.TypeExprList:
		value = "plugimpl.ListType(" + typeExprLiteral(*expr.Element) + ")"
	case plug.TypeExprNamed:
		value = "plugimpl.NamedType(" + strconv.Quote(expr.Name) + ")"
	default:
		value = "plugimpl.PrimitiveType(" + strconv.Quote(expr.Name) + ")"
	}
	if expr.Optional {
		value = "plugimpl.OptionalType(" + value + ")"
	}
	return value
}

func quotedStrings(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = strconv.Quote(v)
	}
	return strings.Join(quoted, ", ")
}

func byteLiteral(value []byte) string {
	parts := make([]string, len(value))
	for i, b := range value {
		parts[i] = fmt.Sprintf("0x%02x", b)
	}
	return strings.Join(parts, ", ")
}
