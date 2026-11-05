// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"fmt"
	"math"
	"math/big"
	"slices"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/plugimpl"
)

func (s *session) coerce(v value, t plug.TypeExpr) (value, error) {
	if t.Optional {
		if v.data == nil {
			return value{typ: &t}, nil
		}
		inner := t
		inner.Optional = false
		v, err := s.coerce(v, inner)
		v.typ = &t
		return v, err
	}

	bad := func() (value, error) {
		return value{}, fmt.Errorf("expected %s, got %s", schemaType(t), typeName(v))
	}

	if t.Kind == plug.TypeExprList {
		xs, ok := v.data.([]value)
		if !ok {
			return bad()
		}
		out := make([]value, len(xs))
		for i, x := range xs {
			item, err := s.coerce(x, *t.Element)
			if err != nil {
				return value{}, fmt.Errorf("element %d: %w", i, err)
			}
			out[i] = item
		}
		return value{
			data: out,
			typ:  &t,
		}, nil
	}

	if t.Kind == plug.TypeExprNamed {
		typ := s.types[t.Name]
		if typ == nil {
			return value{}, fmt.Errorf("unknown schema type %q", t.Name)
		}
		if v.typ != nil && v.typ.Kind == plug.TypeExprNamed && v.typ.Name != t.Name {
			return bad()
		}
		if typ.Kind == plug.TypeNewtype {
			v, err := s.coerce(v, plug.TypeExpr{
				Kind: plug.TypeExprPrimitive,
				Name: typ.Underlying,
			})
			v.typ = &t
			return v, err
		}
		r, ok := v.data.(*record)
		if !ok {
			return bad()
		}
		for _, ctor := range typ.Constructors {
			if r.ctor != ctor.Name && !(r.ctor == "" && len(typ.Constructors) == 1) {
				continue
			}
			out := &record{
				ctor: ctor.Name,
			}
			for _, f := range r.fields {
				if !slices.ContainsFunc(ctor.Fields, func(want plug.FieldInfo) bool {
					return f.name == want.Name
				}) {
					return value{}, fmt.Errorf("%s: unknown field %q", t.Name, f.name)
				}
			}
			for _, f := range ctor.Fields {
				x, found := r.lookup(f.Name)
				if !found && !f.Type.Optional {
					return value{}, fmt.Errorf("%s: missing field %q", t.Name, f.Name)
				}
				x, err := s.coerce(x, f.Type)
				if err != nil {
					return value{}, fmt.Errorf("%s: %w", f.Name, err)
				}
				out.fields = append(out.fields, field{
					name:  f.Name,
					value: x,
				})
			}
			return value{
				data: out,
				typ:  &t,
			}, nil
		}
		return bad()
	}

	switch t.Name {
	case "void":
		if v.data != nil {
			return bad()
		}
	case "bool":
		if _, ok := v.data.(bool); !ok {
			return bad()
		}
	case "string":
		if _, ok := v.data.(string); !ok {
			return bad()
		}
	case "bytes":
		if _, ok := v.data.([]byte); !ok {
			return bad()
		}
	case "f64":
		f, ok := number(v)
		if !ok || math.IsInf(f, 0) {
			return bad()
		}
		v.data = f
	case "i32", "i64", "u32", "u64":
		n, ok := v.data.(*big.Int)
		if !ok {
			return bad()
		}
		valid := false
		switch t.Name {
		case "i32":
			valid = n.IsInt64() && n.Int64() >= math.MinInt32 && n.Int64() <= math.MaxInt32
		case "i64":
			valid = n.IsInt64()
		case "u32":
			valid = n.IsUint64() && n.Uint64() <= math.MaxUint32
		case "u64":
			valid = n.IsUint64()
		}
		if !valid {
			return value{}, fmt.Errorf("integer %s is out of range for %s", n, t.Name)
		}
	default:
		return value{}, fmt.Errorf("unsupported schema type %q", t.Name)
	}
	v.typ = &t
	return v, nil
}

func (s *session) coerceFields(fields []plug.FieldInfo, args []value) (*record, error) {
	r := &record{}
	for i, f := range fields {
		v, err := s.coerce(args[i], f.Type)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		r.fields = append(r.fields, field{
			name:  f.Name,
			value: v,
		})
	}
	return r, nil
}

// codec delegates scalar validation and allocation limits to Plug.
func (s *session) codec(t plug.TypeExpr) plugimpl.Codec[value] {
	var c plugimpl.Codec[value]
	switch {
	case t.Optional:
		inner := t
		inner.Optional = false
		optional := plugimpl.OptionalCodec(s.codec(inner))
		c = plugimpl.Codec[value]{
			Encode: func(e *plugimpl.Encoder, v value) {
				if v.data == nil {
					optional.Encode(e, plugimpl.None[value]())
				} else {
					optional.Encode(e, plugimpl.Some(v))
				}
			},
			Decode: func(d *plugimpl.Decoder) value {
				v, _ := optional.Decode(d).Value()
				return v
			},
		}
	case t.Kind == plug.TypeExprList:
		list := plugimpl.ListCodec(s.codec(*t.Element))
		c = plugimpl.Codec[value]{
			Encode: func(e *plugimpl.Encoder, v value) { list.Encode(e, v.data.([]value)) },
			Decode: func(d *plugimpl.Decoder) value {
				if !plugimpl.Reserve[[]value](d, 1) {
					return value{}
				}
				return value{data: list.Decode(d)}
			},
		}
	case t.Kind == plug.TypeExprNamed:
		typ := s.types[t.Name]
		if typ.Kind == plug.TypeNewtype {
			c = s.codec(plug.TypeExpr{
				Kind: plug.TypeExprPrimitive,
				Name: typ.Underlying,
			})
			break
		}

		codecs := make(map[string]plugimpl.Codec[value])
		for _, ctor := range typ.Constructors {
			codecs[ctor.Name] = s.fieldsCodec(ctor.Fields)
		}
		c = plugimpl.Codec[value]{
			Encode: func(e *plugimpl.Encoder, v value) {
				r := v.data.(*record)
				for _, ctor := range typ.Constructors {
					if ctor.Name != r.ctor {
						continue
					}

					var mark int
					if typ.Kind == plug.TypeSum {
						mark = e.BeginSum(ctor.ID)
					} else {
						mark = e.BeginRecord()
					}
					codecs[ctor.Name].Encode(e, v)
					e.EndValue(mark)
					return
				}
				e.Fail(fmt.Errorf("unknown %s constructor %q", typ.Name, r.ctor))
			},
			Decode: func(d *plugimpl.Decoder) value {
				var (
					id   uint32
					mark int
				)
				if typ.Kind == plug.TypeSum {
					id, mark = d.BeginSum()
				} else {
					mark = d.BeginRecord()
					id = typ.Constructors[0].ID
				}

				for _, ctor := range typ.Constructors {
					if ctor.ID != id {
						continue
					}
					v := codecs[ctor.Name].Decode(d)
					if d.Err() != nil {
						return value{}
					}
					v.data.(*record).ctor = ctor.Name
					d.FinishValue(mark)
					return v
				}
				d.UnknownConstructor(typ.Name, id)
				return value{}
			},
		}
	default:
		c = scalarCodec(t.Name)
	}

	decode := c.Decode
	c.Decode = func(d *plugimpl.Decoder) value {
		v := decode(d)
		v.typ = &t
		return v
	}
	return c
}

func scalarCodec(name string) plugimpl.Codec[value] {
	return plugimpl.Codec[value]{
		Encode: func(e *plugimpl.Encoder, v value) {
			switch name {
			case "void":
			case "bool":
				e.Bool(v.data.(bool))
			case "string":
				e.String(v.data.(string))
			case "bytes":
				e.Bytes(v.data.([]byte))
			case "f64":
				e.F64(v.data.(float64))
			case "i32":
				e.I32(int32(v.data.(*big.Int).Int64()))
			case "i64":
				e.I64(v.data.(*big.Int).Int64())
			case "u32":
				e.U32(uint32(v.data.(*big.Int).Uint64()))
			case "u64":
				e.U64(v.data.(*big.Int).Uint64())
			}
		},
		Decode: func(d *plugimpl.Decoder) value {
			var v value
			switch name {
			case "string":
				if !plugimpl.Reserve[string](d, 1) {
					return v
				}
			case "bytes":
				if !plugimpl.Reserve[[]byte](d, 1) {
					return v
				}
			case "f64":
				if !plugimpl.Reserve[float64](d, 1) {
					return v
				}
			case "i32", "i64", "u32", "u64":
				if !plugimpl.Reserve[big.Int](d, 1) || !plugimpl.Reserve[uint64](d, 1) {
					return v
				}
			}

			switch name {
			case "bool":
				v.data = d.Bool()
			case "string":
				v.data = d.String()
			case "bytes":
				v.data = d.Bytes()
			case "f64":
				v.data = d.F64()
			case "i32":
				return integer(int64(d.I32()))
			case "i64":
				return integer(d.I64())
			case "u32":
				return integer(int64(d.U32()))
			case "u64":
				v.data = new(big.Int).SetUint64(d.U64())
			}
			return v
		},
	}
}

func (s *session) fieldsCodec(fields []plug.FieldInfo) plugimpl.Codec[value] {
	codecs := make([]plugimpl.Codec[value], len(fields))
	for i, f := range fields {
		codecs[i] = s.codec(f.Type)
	}

	return plugimpl.Codec[value]{
		Encode: func(e *plugimpl.Encoder, v value) {
			r := v.data.(*record)
			last := len(fields)
			for last > 0 && fields[last-1].Type.Optional {
				x, _ := r.lookup(fields[last-1].Name)
				if x.data != nil {
					break
				}
				last--
			}
			for i, f := range fields[:last] {
				x, _ := r.lookup(f.Name)
				codecs[i].Encode(e, x)
			}
		},
		Decode: func(d *plugimpl.Decoder) value {
			if !plugimpl.Reserve[record](d, 1) || !plugimpl.Reserve[field](d, len(fields)) {
				return value{}
			}
			r := &record{
				fields: make([]field, 0, len(fields)),
			}
			for i, f := range fields {
				r.fields = append(r.fields, field{
					name:  f.Name,
					value: codecs[i].Decode(d),
				})
				if d.Err() != nil {
					return value{}
				}
			}
			return value{data: r}
		},
	}
}

type appError struct {
	value value
}

func (e *appError) Error() string { return format(e.value, 0) }

func (s *session) errorCodec(name string) plugimpl.ErrorCodec {
	return plugimpl.ErrorCodec{
		Decode: func(d *plugimpl.Decoder, id uint32) error {
			if typ := s.types[name]; typ != nil {
				for _, ctor := range typ.Constructors {
					if ctor.ID != id {
						continue
					}
					v := s.fieldsCodec(ctor.Fields).Decode(d)
					if !plugimpl.Reserve[appError](d, 1) || !plugimpl.Reserve[plug.TypeExpr](d, 1) {
						return nil
					}
					v.data.(*record).ctor = ctor.Name
					v.typ = &plug.TypeExpr{
						Kind: plug.TypeExprNamed,
						Name: name,
					}
					d.IgnoreRemaining()
					return &appError{
						value: v,
					}
				}
			}
			d.UnknownConstructor(name, id)
			return nil
		},
	}
}
