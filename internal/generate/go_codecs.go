// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"strings"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/internal/names"
)

func (g *generator) writeCodecs() {
	for _, nt := range g.newtypes {
		g.writeNewtypeCodec(nt)
	}

	for _, name := range g.groupNames {
		ctors := g.types[name].Constructors
		if g.types[name].Error {
			g.writeErrorCodec(name, ctors)
			continue
		}
		if len(ctors) == 1 {
			g.writeRecordCodec(name, ctors[0])
			continue
		}
		g.writeSumCodec(name, ctors)
	}

	for _, m := range g.methods {
		if len(m.Fields) == 0 {
			continue
		}
		g.writeRequestCodec(names.GoMethod(m.Name)+"Request", m.Fields)
	}
}

func (g *generator) writeNewtypeCodec(nt plug.TypeInfo) {
	var (
		goName    = names.Go(nt.Name)
		codec     = codecName(nt.Name)
		primitive = primitiveCodec(nt.Underlying)
	)

	g.line("var %s = plugimpl.Codec[%s]{", codec, goName)
	g.line("\tEncode: func(enc *plugimpl.Encoder, v %s) {", goName)
	g.line("\t\t%s.Encode(enc, %s(v))", primitive, primitiveGoType(nt.Underlying))
	g.line("\t},")
	g.line("\tDecode: func(dec *plugimpl.Decoder) %s {", goName)
	g.line("\t\treturn %s(%s.Decode(dec))", goName, primitive)
	g.line("\t},")
	g.line("}")
	g.line("")
}

func (g *generator) writeRequestCodec(name string, fields []plug.FieldInfo) {
	goName := names.Go(name)

	g.line("var %s = plugimpl.Codec[%s]{", codecName(name), goName)
	g.line("\tEncode: func(enc *plugimpl.Encoder, v %s) {", goName)
	g.writeEncodeFields("\t\t", fields)
	g.line("\t},")
	g.line("\tDecode: func(dec *plugimpl.Decoder) (v %s) {", goName)
	g.writeDecodeFields("\t\t", fields)
	g.line("\t\tdec.IgnoreRemaining()")
	g.line("\t\treturn v")
	g.line("\t},")
	g.line("}")
	g.line("")
}

func (g *generator) writeRecordCodec(name string, c plug.ConstructorInfo) {
	goName := names.Go(name)

	g.line("var %s = plugimpl.Codec[%s]{", codecName(name), goName)
	g.line("\tEncode: func(enc *plugimpl.Encoder, v %s) {", goName)
	g.line("\t\tbody := enc.BeginRecord()")
	g.writeEncodeFields("\t\t", c.Fields)
	g.line("\t\tenc.EndValue(body)")
	g.line("\t},")
	g.line("\tDecode: func(dec *plugimpl.Decoder) (v %s) {", goName)
	g.line("\t\tbody := dec.BeginRecord()")
	g.writeDecodeFields("\t\t", c.Fields)
	g.line("\t\tdec.FinishValue(body)")
	g.line("\t\treturn v")
	g.line("\t},")
	g.line("}")
	g.line("")
}

func (g *generator) writeSumCodec(result string, ctors []plug.ConstructorInfo) {
	goName := names.Go(result)

	g.line("var %s = plugimpl.Codec[%s]{", codecName(result), goName)
	g.line("\tEncode: func(enc *plugimpl.Encoder, v %s) {", goName)
	g.line("\t\tswitch v := v.(type) {")
	for _, c := range ctors {
		variant := names.Go(c.Name)
		g.line("\t\tcase *%s:", variant)
		g.line("\t\t\tif v == nil {")
		g.line("\t\t\t\tenc.Unsupported(%q, v)", result)
		g.line("\t\t\t\treturn")
		g.line("\t\t\t}")
		g.line("\t\t\tbody := enc.BeginSum(%s)", constructorIDName(c.Name))
		g.writeEncodeFields("\t\t\t", c.Fields)
		g.line("\t\t\tenc.EndValue(body)")
	}

	unknownName := "Unknown" + goName
	g.line("\t\tcase *%s:", unknownName)
	g.line("\t\t\tif v == nil {")
	g.line("\t\t\t\tenc.Unsupported(%q, v)", result)
	g.line("\t\t\t\treturn")
	g.line("\t\t\t}")
	g.line("\t\t\tbody := enc.BeginSum(v.ConstructorID)")
	g.line("\t\t\tenc.Raw(v.Payload)")
	g.line("\t\t\tenc.EndValue(body)")
	g.line("\t\tdefault:")
	g.line("\t\t\tenc.Unsupported(%q, v)", result)
	g.line("\t\t}")
	g.line("\t},")
	g.line("\tDecode: func(dec *plugimpl.Decoder) %s {", goName)
	g.line("\t\tid, body := dec.BeginSum()")
	g.line("\t\tswitch id {")
	for _, c := range ctors {
		variant := names.Go(c.Name)
		g.line("\t\tcase %s:", constructorIDName(c.Name))
		g.line("\t\t\tif !plugimpl.Reserve[%s](dec, 1) {", variant)
		g.line("\t\t\t\treturn nil")
		g.line("\t\t\t}")
		g.line("\t\t\tv := new(%s)", variant)
		g.writeDecodeFields("\t\t\t", c.Fields)
		g.line("\t\t\tdec.FinishValue(body)")
		g.line("\t\t\treturn v")
	}
	g.line("\t\tdefault:")
	g.line("\t\t\tif !plugimpl.Reserve[%s](dec, 1) {", unknownName)
	g.line("\t\t\t\treturn nil")
	g.line("\t\t\t}")
	g.line("\t\t\tv := &%s{", unknownName)
	g.line("\t\t\t\tConstructorID: id,")
	g.line("\t\t\t\tPayload: dec.CopyRemaining(),")
	g.line("\t\t\t}")
	g.line("\t\t\tdec.FinishValue(body)")
	g.line("\t\t\treturn v")
	g.line("\t\t}")
	g.line("\t},")
	g.line("}")
	g.line("")
}

func (g *generator) writeErrorCodec(result string, ctors []plug.ConstructorInfo) {
	var (
		goName = names.Go(result)
		codec  = errorCodecName(result)
	)

	g.line("var %s = plugimpl.ErrorCodec{", codec)
	g.line("\tEncode: func(enc *plugimpl.Encoder, err error) (uint32, bool) {")
	if len(ctors) == 1 {
		g.line("\t\tvar v *%s", goName)
	} else {
		g.line("\t\tvar v %s", goName)
	}
	g.line("\t\tif !errors.As(err, &v) {")
	g.line("\t\t\treturn 0, false")
	g.line("\t\t}")

	if len(ctors) == 1 {
		c := ctors[0]
		g.line("\t\tif v == nil {")
		g.line("\t\t\tenc.Unsupported(%q, v)", result)
		g.line("\t\t\treturn 0, true")
		g.line("\t\t}")
		g.writeEncodeFields("\t\t", c.Fields)
		g.line("\t\treturn %s, true", constructorIDName(c.Name))
	} else {
		g.line("\t\tswitch v := v.(type) {")
		for _, c := range ctors {
			variant := names.Go(c.Name)
			g.line("\t\tcase *%s:", variant)
			g.line("\t\t\tif v == nil {")
			g.line("\t\t\t\tenc.Unsupported(%q, v)", result)
			g.line("\t\t\t\treturn 0, true")
			g.line("\t\t\t}")
			g.writeEncodeFields("\t\t\t", c.Fields)
			g.line("\t\t\treturn %s, true", constructorIDName(c.Name))
		}
		g.line("\t\tcase *Unknown%s:", goName)
		g.line("\t\t\tif v == nil {")
		g.line("\t\t\t\tenc.Unsupported(%q, v)", result)
		g.line("\t\t\t\treturn 0, true")
		g.line("\t\t\t}")
		g.line("\t\t\tenc.Raw(v.Payload)")
		g.line("\t\t\treturn v.ConstructorID, true")
		g.line("\t\tdefault:")
		g.line("\t\t\treturn 0, false")
		g.line("\t\t}")
	}
	g.line("\t},")

	g.line("\tIdentify: func(err error) (uint32, bool) {")
	if len(ctors) == 1 {
		g.line("\t\tvar v *%s", goName)
	} else {
		g.line("\t\tvar v %s", goName)
	}
	g.line("\t\tif !errors.As(err, &v) {")
	g.line("\t\t\treturn 0, false")
	g.line("\t\t}")

	if len(ctors) == 1 {
		g.line("\t\tif v == nil {")
		g.line("\t\t\treturn 0, false")
		g.line("\t\t}")
		g.line("\t\treturn %s, true", constructorIDName(ctors[0].Name))
	} else {
		g.line("\t\tswitch v := v.(type) {")
		for _, c := range ctors {
			g.line("\t\tcase *%s:", names.Go(c.Name))
			g.line("\t\t\treturn %s, true", constructorIDName(c.Name))
		}
		g.line("\t\tcase *Unknown%s:", goName)
		g.line("\t\t\treturn v.ConstructorID, true")
		g.line("\t\tdefault:")
		g.line("\t\t\treturn 0, false")
		g.line("\t\t}")
	}
	g.line("\t},")

	g.line("\tDecode: func(dec *plugimpl.Decoder, id uint32) error {")
	g.line("\t\tswitch id {")
	for _, c := range ctors {
		variant := names.Go(c.Name)
		returnType := variant
		if len(ctors) == 1 {
			returnType = goName
		}
		g.line("\t\tcase %s:", constructorIDName(c.Name))
		g.line("\t\t\tif !plugimpl.Reserve[%s](dec, 1) {", returnType)
		g.line("\t\t\t\treturn nil")
		g.line("\t\t\t}")
		g.line("\t\t\tv := new(%s)", returnType)
		g.writeDecodeFields("\t\t\t", c.Fields)
		g.line("\t\t\tdec.IgnoreRemaining()")
		g.line("\t\t\treturn v")
	}
	g.line("\t\tdefault:")
	if len(ctors) == 1 {
		g.line("\t\t\tdec.UnknownConstructor(%q, id)", result)
		g.line("\t\t\treturn nil")
	} else {
		g.line("\t\t\tif !plugimpl.Reserve[Unknown%s](dec, 1) {", goName)
		g.line("\t\t\t\treturn nil")
		g.line("\t\t\t}")
		g.line("\t\t\tpayload := dec.CopyRemaining()")
		g.line("\t\t\treturn &Unknown%s{ConstructorID: id, Payload: payload}", goName)
	}
	g.line("\t\t}")
	g.line("\t},")
	g.line("}")
	g.line("")
}

func (g *generator) writeEncodeFields(indent string, fields []plug.FieldInfo) {
	first := len(fields)
	for i, f := range fields {
		if f.Type.Optional {
			first = i
			break
		}
		g.line("%s%s.Encode(enc, v.%s)", indent, g.codec(f.Type), names.Go(f.Name))
	}

	fields = fields[first:]
	if len(fields) > 1 {
		var present []string
		for _, f := range fields {
			present = append(present, "v."+names.Go(f.Name)+".Present()")
		}
		g.line("%slast := plugimpl.LastPresent(%s)", indent, strings.Join(present, ", "))
	}

	for i, f := range fields {
		if len(fields) == 1 {
			g.line("%sif v.%s.Present() {", indent, names.Go(f.Name))
		} else {
			g.line("%sif last >= %d {", indent, i)
		}
		g.line("%s\t%s.Encode(enc, v.%s)", indent, g.codec(f.Type), names.Go(f.Name))
		g.line("%s}", indent)
	}
}

func (g *generator) writeDecodeFields(indent string, fields []plug.FieldInfo) {
	for _, f := range fields {
		g.line("%sv.%s = %s.Decode(dec)", indent, names.Go(f.Name), g.codec(f.Type))
	}
}

func (g *generator) codec(ref plug.TypeExpr) string {
	var result string
	if ref.Element != nil {
		result = "plugimpl.ListCodec(" + g.codec(*ref.Element) + ")"
	} else if ref.Kind == plug.TypeExprPrimitive {
		result = primitiveCodec(ref.Name)
	} else {
		result = codecName(ref.Name)
	}

	if ref.Optional {
		result = "plugimpl.OptionalCodec(" + result + ")"
	}
	return result
}

func primitiveCodec(name string) string {
	switch name {
	case "bool":
		return "plugimpl.BoolCodec"
	case "bytes":
		return "plugimpl.BytesCodec"
	case "f64":
		return "plugimpl.F64Codec"
	case "i32":
		return "plugimpl.I32Codec"
	case "i64":
		return "plugimpl.I64Codec"
	case "string":
		return "plugimpl.StringCodec"
	case "u32":
		return "plugimpl.U32Codec"
	case "u64":
		return "plugimpl.U64Codec"
	case "void":
		return "plugimpl.VoidCodec"
	default:
		panic("unknown primitive " + name)
	}
}
