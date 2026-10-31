// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"encoding/json/v2"
	"fmt"
	"slices"
	"strconv"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/internal/names"
)

func (g *goGenerator) writeJSON() {
	if !g.hasHTTP {
		return
	}

	for _, name := range g.groupNames {
		ctors := g.types[name].Constructors
		if len(ctors) < 2 {
			continue
		}

		goName := names.Go(name)
		for _, c := range ctors {
			variant := names.Go(c.Name)
			g.line("// MarshalJSON includes the Plug constructor name in the type field.")
			if len(c.Fields) == 0 {
				encoded, _ := json.Marshal(map[string]string{"type": c.Name})
				g.line("func (v %s) MarshalJSON() ([]byte, error) { return []byte(%q), nil }", variant, encoded)
				g.line("")
				continue
			}
			g.line("func (v %s) MarshalJSON() ([]byte, error) {", variant)
			g.line("\ttype fields %s", variant)
			g.line("\treturn json.Marshal(struct {")
			g.line("\t\tConstructor string `json:\"type\"`")
			g.line("\t\tfields")
			g.line("\t}{")
			g.line("\t\tConstructor: %s,", strconv.Quote(c.Name))
			g.line("\t\tfields: fields(v),")
			g.line("\t})")
			g.line("}")
			g.line("")
		}

		unknown := "Unknown" + goName
		if g.native {
			g.line("// MarshalJSON preserves an unknown Plug constructor for diagnostics.")
			g.line("func (v %s) MarshalJSON() ([]byte, error) {", unknown)
			g.line("\treturn json.Marshal(struct {")
			g.line("\t\tConstructor string `json:\"type\"`")
			g.line("\t\tConstructorID uint32 `json:\"constructorID\"`")
			g.line("\t\tPayload []byte `json:\"payload\"`")
			g.line("\t}{")
			g.line("\t\tConstructor: \"unknown\",")
			g.line("\t\tConstructorID: v.ConstructorID,")
			g.line("\t\tPayload: v.Payload,")
			g.line("\t})")
			g.line("}")
			g.line("")
		} else {
			g.line("// MarshalJSON returns the unfamiliar constructor's complete JSON object.")
			g.line("func (v %s) MarshalJSON() ([]byte, error) { return v.Payload, nil }", unknown)
			g.line("")
		}

		g.line("func decode%sJSON(data []byte) (%s, error) {", goName, goName)
		g.line("\tvar header struct {")
		g.line("\t\tConstructor string `json:\"type\"`")
		g.line("\t}")
		g.line("\tif err := json.Unmarshal(data, &header); err != nil {")
		g.line("\t\treturn nil, err")
		g.line("\t}")
		g.line("\tif header.Constructor == \"\" {")
		g.line("\t\treturn nil, fmt.Errorf(%q)", name+" constructor type is required")
		g.line("\t}")
		g.line("\tvar v %s", goName)
		g.line("\tswitch header.Constructor {")
		for _, c := range ctors {
			variant := names.Go(c.Name)
			g.line("\tcase %s:", strconv.Quote(c.Name))
			g.line("\t\tv = new(%s)", variant)
		}
		g.line("\tdefault:")
		if g.native {
			g.line("\t\treturn nil, fmt.Errorf(%q, header.Constructor)", "unknown "+name+" constructor %q")
		} else {
			g.line("\t\treturn &%s{Constructor: header.Constructor, Payload: jsontext.Value(data).Clone()}, nil", unknown)
		}
		g.line("\t}")
		g.line("\tif err := json.Unmarshal(data, v); err != nil {")
		g.line("\t\treturn nil, err")
		g.line("\t}")
		g.line("\treturn v, nil")
		g.line("}")
		g.line("")
	}

	for _, name := range g.groupNames {
		for _, c := range g.types[name].Constructors {
			targetName := names.Go(name)
			if len(g.types[name].Constructors) > 1 {
				targetName = names.Go(c.Name)
			}
			g.writeJSONUnmarshal(targetName, c.Fields)
		}
	}
	for _, m := range g.methods {
		g.writeJSONUnmarshal(names.GoMethod(m.Name)+"Request", m.Fields)
	}
}

func (g *goGenerator) hasSumFields() bool {
	for _, c := range g.constructors {
		if g.fieldsContainSum(c.Fields) {
			return true
		}
	}
	for _, method := range g.methods {
		if g.fieldsContainSum(method.Fields) {
			return true
		}
	}
	return false
}

func (g *goGenerator) fieldsContainSum(fields []plug.FieldInfo) bool {
	return slices.ContainsFunc(fields, func(field plug.FieldInfo) bool {
		return g.typeContainsSum(field.Type)
	})
}

func (g *goGenerator) writeJSONUnmarshal(name string, fields []plug.FieldInfo) {
	if !g.fieldsContainSum(fields) {
		return
	}

	g.line("// UnmarshalJSON decodes sum fields using the type discriminator.")
	g.line("func (v *%s) UnmarshalJSON(data []byte) error {", name)
	g.line("\tvar raw struct {")
	for _, f := range fields {
		typ := g.goType(f.Type)
		if g.typeContainsSum(f.Type) {
			typ = "jsontext.Value"
		}
		g.line("\t\t%s %s `json:%s`", names.Go(f.Name), typ, strconv.Quote(f.Name))
	}
	g.line("\t}")
	g.line("\tif err := json.Unmarshal(data, &raw); err != nil {")
	g.line("\t\treturn err")
	g.line("\t}")

	var counter int
	for _, f := range fields {
		var (
			target = "v." + names.Go(f.Name)
			raw    = "raw." + names.Go(f.Name)
		)
		if g.typeContainsSum(f.Type) {
			g.writeJSONDecode(f.Type, target, raw, "\t", f.Name, "", &counter)
		} else {
			g.line("\t%s = %s", target, raw)
		}
	}
	g.line("\treturn nil")
	g.line("}")
	g.line("")
}

func (g *goGenerator) writeJSONDecode(ref plug.TypeExpr, target, raw, indent, fieldName, errorPrefix string, counter *int) {
	if ref.Optional {
		var (
			inner   = ref
			decoded = fmt.Sprintf("decoded%d", *counter)
		)
		inner.Optional = false
		*counter++

		g.line("%sif len(%s) == 0 || string(%s) == \"null\" {", indent, raw, raw)
		g.line("%s\t%s = %s{}", indent, target, g.goType(ref))
		g.line("%s} else {", indent)
		g.line("%s\tvar %s %s", indent, decoded, g.goType(inner))
		g.writeJSONDecode(inner, decoded, raw, indent+"\t", fieldName, errorPrefix, counter)
		g.line("%s\t%s = %sSome(%s)", indent, target, g.optPrefix, decoded)
		g.line("%s}", indent)
		return
	}

	if ref.Element != nil {
		*counter++
		var (
			items = fmt.Sprintf("items%d", *counter)
			index = fmt.Sprintf("index%d", *counter)
		)

		g.line("%svar %s []jsontext.Value", indent, items)
		g.line("%sif err := json.Unmarshal(%s, &%s); err != nil {", indent, raw, items)
		g.line("%s\treturn %sfmt.Errorf(%q, err)", indent, errorPrefix, "decode field "+fieldName+": %w")
		g.line("%s}", indent)
		g.line("%s%s = make(%s, len(%s))", indent, target, g.goType(ref), items)
		g.line("%sfor %s := range %s {", indent, index, items)
		g.writeJSONDecode(*ref.Element, target+"["+index+"]", items+"["+index+"]", indent+"\t", fieldName, errorPrefix, counter)
		g.line("%s}", indent)
		return
	}

	if len(g.types[ref.Name].Constructors) > 1 {
		*counter++
		decoded := fmt.Sprintf("decoded%d", *counter)

		g.line("%s%s, err := decode%sJSON(%s)", indent, decoded, names.Go(ref.Name), raw)
		g.line("%sif err != nil {", indent)
		g.line("%s\treturn %sfmt.Errorf(%q, err)", indent, errorPrefix, "decode field "+fieldName+": %w")
		g.line("%s}", indent)
		g.line("%s%s = %s", indent, target, decoded)
		return
	}

	g.line("%sif err := json.Unmarshal(%s, &%s); err != nil {", indent, raw, target)
	g.line("%s\treturn %sfmt.Errorf(%q, err)", indent, errorPrefix, "decode field "+fieldName+": %w")
	g.line("%s}", indent)
}
