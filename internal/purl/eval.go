// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net"
	"slices"
	"strings"
	"text/scanner"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/plugimpl"
)

type session struct {
	client *plug.Client
	dial   func(context.Context) (net.Conn, error)
	schema *plug.Schema
	types  map[string]*plug.TypeInfo
	env    *scope
	depth  int
	names  map[string]*binding // Lookups in the current top-level scope only.
}

type scope struct {
	parent   *scope
	bindings map[string]*binding
}

type binding struct {
	x     expr
	env   *scope
	done  bool
	value value
	err   error
}

type function struct {
	name     string
	params   []string
	doc      string
	fields   []plug.FieldInfo // Schema parameters: named arguments and optional defaults.
	response *plug.TypeExpr
	method   *plug.MethodInfo
	invoke   func(context.Context, []value) (value, error)
	closure  *closure
}

type closure struct {
	s   *session
	env *scope
	x   expr
}

func (f *function) scope(args []value) *scope {
	if len(f.params) == 0 {
		return f.closure.env
	}
	var (
		env = &scope{
			parent:   f.closure.env,
			bindings: make(map[string]*binding, len(f.params)),
		}
		bs = make([]binding, len(f.params))
	)

	for i, name := range f.params {
		bs[i] = binding{
			done:  true,
			value: args[i],
		}
		env.bindings[name] = &bs[i]
	}
	return env
}

func (f *function) call(ctx context.Context, args []value) (value, error) {
	if f.closure != nil {
		return f.closure.s.eval(ctx, f.scope(args), f.closure.x)
	}
	return f.invoke(ctx, args)
}

type evalError struct {
	pos scanner.Position
	err error
}

func (e *evalError) Error() string { return fmt.Sprintf("%d:%d: %v", e.pos.Line, e.pos.Column, e.err) }
func (e *evalError) Unwrap() error { return e.err }

func newSession(client *plug.Client, schema *plug.Schema) *session {
	s := &session{
		client: client,
		schema: schema,
		types:  make(map[string]*plug.TypeInfo),
		env: &scope{
			bindings: make(map[string]*binding),
		},
	}
	for i := range schema.Types {
		s.types[schema.Types[i].Name] = &schema.Types[i]
	}

	for name, fn := range builtins() {
		s.env.bindings[name] = ready(value{data: fn})
	}

	for _, typ := range schema.Types {
		for _, ctor := range typ.Constructors {
			t := plug.TypeExpr{
				Kind: plug.TypeExprNamed,
				Name: typ.Name,
			}
			fn := &function{
				name:     ctor.Name,
				fields:   ctor.Fields,
				response: &t,
			}
			for _, f := range ctor.Fields {
				fn.params = append(fn.params, f.Name)
			}
			fn.invoke = func(ctx context.Context, args []value) (value, error) {
				r, err := s.coerceFields(ctor.Fields, args)
				if err != nil {
					return value{}, err
				}
				r.ctor = ctor.Name
				return value{
					data: r,
					typ:  &t,
				}, nil
			}
			s.env.bindings[ctor.Name] = ready(value{data: fn})
		}
	}

	// A method namespace is a record of functions.
	namespaces := make(map[string]*record)
	for _, info := range schema.Methods {
		ns, name, _ := strings.Cut(info.Name, ".")
		if namespaces[ns] == nil {
			namespaces[ns] = &record{}
		}
		namespaces[ns].fields = append(namespaces[ns].fields, field{
			name:  name,
			value: value{data: s.method(info)},
		})
	}
	for name, r := range namespaces {
		s.env.bindings[name] = ready(value{data: r})
	}
	return s
}

func ready(v value) *binding {
	return &binding{
		done:  true,
		value: v,
	}
}

func (env *scope) lookup(name string) (*binding, bool) {
	for ; env != nil; env = env.parent {
		if b, ok := env.bindings[name]; ok {
			return b, true
		}
	}
	return nil, false
}

func (s *session) bind(name string, b *binding) {
	if _, cached := s.names[name]; cached {
		s.names[name] = b
	}
	s.env = &scope{
		parent:   s.env,
		bindings: map[string]*binding{name: b},
	}
}

func (s *session) lookup(env *scope, name string) (*binding, bool) {
	if env != s.env {
		return env.lookup(name)
	}
	if b, cached := s.names[name]; cached {
		return b, true
	}
	b, found := env.lookup(name)
	if !found {
		return nil, false
	}
	if s.names == nil {
		s.names = make(map[string]*binding)
	}
	s.names[name] = b
	return b, true
}

func (s *session) execute(ctx context.Context, st stmt) (value, bool, error) {
	if st.name != "" {
		s.bind(st.name, &binding{
			x:   st.x,
			env: s.env,
		})
		return value{}, false, nil
	}

	v, err := s.eval(ctx, s.env, st.x)
	if err != nil {
		return value{}, false, err
	}
	s.bind("_", ready(v))
	return v, true, nil
}

// Deferred bindings capture only older scopes, so they cannot reach themselves.
func (s *session) force(ctx context.Context, b *binding) (value, error) {
	if !b.done {
		b.value, b.err = s.eval(ctx, b.env, b.x)
		b.done = true

		// The result replaces the captured scope and expression.
		b.env = nil
		b.x = expr{}
	}
	return b.value, b.err
}

func (s *session) eval(ctx context.Context, env *scope, x expr) (v value, err error) {
	if err := ctx.Err(); err != nil {
		return value{}, err
	}

	var (
		op  string
		pos scanner.Position
	)

	s.depth++
	defer func() {
		s.depth--
		if err == nil && op != "" {
			if _, ok := v.data.(bool); !ok {
				err = &evalError{
					pos: pos,
					err: fmt.Errorf("operator %q requires bool", op),
				}
			}
		}

		if err != nil {
			var loc *evalError
			if !errors.As(err, &loc) {
				err = &evalError{
					pos: x.pos,
					err: err,
				}
			}
		}
	}()

	if s.depth > maxDepth {
		return value{}, errors.New("evaluation is too deeply nested")
	}

	for {
		var (
			fn   *function
			args []value
		)

		switch n := x.node.(type) {
		case *literalExpr:
			return n.load()
		case nameExpr:
			switch n.name {
			case "nil", "null":
				return value{}, nil
			case "true":
				return value{data: true}, nil
			case "false":
				return value{data: false}, nil
			}
			b, ok := s.lookup(env, n.name)
			if !ok {
				return value{}, fmt.Errorf("unknown name %q", n.name)
			}
			return s.force(ctx, b)
		case unaryExpr:
			v, err := s.eval(ctx, env, n.x)
			if err != nil {
				return value{}, err
			}

			switch n.op {
			case "!":
				if b, ok := v.data.(bool); ok {
					return value{data: !b}, nil
				}
			case "+":
				if _, ok := number(v); ok {
					return v, nil
				}
			case "-":
				if z, ok := v.data.(*big.Int); ok {
					return value{data: new(big.Int).Neg(z)}, nil
				}
				if f, ok := v.data.(float64); ok {
					return value{data: -f}, nil
				}
			}
			return value{}, fmt.Errorf("operator %q does not accept %s", n.op, typeName(v))
		case binaryExpr:
			a, err := s.eval(ctx, env, n.x)
			if err != nil {
				return value{}, err
			}
			if n.op == "|" {
				call, ok := n.y.node.(callExpr)
				if !ok {
					call = callExpr{
						fn: n.y,
					}
				}
				fn, args, err = s.callArgs(ctx, env, call, &a)
				if err != nil {
					return value{}, err
				}
				break
			}

			if n.op == "&&" || n.op == "||" {
				b, ok := a.data.(bool)
				if !ok {
					return value{}, fmt.Errorf("operator %q requires bool", n.op)
				}
				if n.op == "&&" && !b || n.op == "||" && b {
					return a, nil
				}
				// Keep the innermost check; outer operators require the same bool.
				op, pos = n.op, x.pos
				x = n.y
				break
			}

			b, err := s.eval(ctx, env, n.y)
			if err != nil {
				return value{}, err
			}
			return binary(n.op, a, b)
		case selectExpr:
			v, err := s.eval(ctx, env, n.x)
			if err != nil {
				return value{}, err
			}
			if r, ok := v.data.(*record); ok {
				if f, ok := r.lookup(n.name); ok {
					return f, nil
				}
			}
			return value{}, fmt.Errorf("%s has no field %q", typeName(v), n.name)
		case indexExpr:
			v, err := s.eval(ctx, env, n.x)
			if err != nil {
				return value{}, err
			}
			i, err := s.eval(ctx, env, n.index)
			if err != nil {
				return value{}, err
			}
			if r, ok := v.data.(*record); ok {
				if key, ok := i.data.(string); ok {
					if v, ok := r.lookup(key); ok {
						return v, nil
					}
					return value{}, fmt.Errorf("no field %q", key)
				}
			}
			xs, ok := v.data.([]value)
			if !ok {
				return value{}, fmt.Errorf("cannot index %s", typeName(v))
			}
			idx, ok := i.data.(*big.Int)
			if !ok || !idx.IsInt64() || idx.Sign() < 0 || idx.Int64() >= int64(len(xs)) {
				return value{}, fmt.Errorf("list index out of range")
			}
			return xs[idx.Int64()], nil
		case listExpr:
			xs := make([]value, 0, len(n.items))
			for _, item := range n.items {
				v, err := s.eval(ctx, env, item)
				if err != nil {
					return value{}, err
				}
				xs = append(xs, v)
			}
			return value{data: xs}, nil
		case recordExpr:
			r := &record{}
			for _, arg := range n.fields {
				if _, ok := r.lookup(arg.name); ok {
					return value{}, fmt.Errorf("duplicate field %q", arg.name)
				}

				v, err := s.eval(ctx, env, arg.x)
				if err != nil {
					return value{}, err
				}
				r.fields = append(r.fields, field{
					name:  arg.name,
					value: v,
				})
			}
			return value{data: r}, nil
		case lambdaExpr:
			fn := &function{
				name:   "lambda",
				params: n.params,
				closure: &closure{
					s:   s,
					env: env,
					x:   n.body,
				},
			}
			return value{data: fn}, nil
		case callExpr:
			fn, args, err = s.callArgs(ctx, env, n, nil)
			if err != nil {
				return value{}, err
			}
		default:
			return value{}, fmt.Errorf("invalid expression")
		}

		if fn != nil {
			if fn.closure == nil {
				return fn.invoke(ctx, args)
			}
			env = fn.scope(args)
			x = fn.closure.x
		}

		if err := ctx.Err(); err != nil {
			return value{}, err
		}
	}
}

func (s *session) callArgs(ctx context.Context, env *scope, call callExpr, first *value) (*function, []value, error) {
	v, err := s.eval(ctx, env, call.fn)
	if err != nil {
		return nil, nil, err
	}
	fn, ok := v.data.(*function)
	if !ok {
		return nil, nil, fmt.Errorf("%s is not callable", typeName(v))
	}

	var (
		args  = make([]value, len(fn.params))
		used  [8]bool
		slots [8]int
	)
	seen := used[:min(len(args), len(used))]
	if len(args) > len(used) {
		seen = make([]bool, len(args))
	}

	pos := slots[:min(len(call.args), len(slots))]
	if len(call.args) > len(slots) {
		pos = make([]int, len(call.args))
	}

	if first != nil {
		if len(args) == 0 {
			return nil, nil, fmt.Errorf("%s takes no arguments", fn.name)
		}
		args[0], seen[0] = *first, true
	}

	// Check names and arity before evaluating any argument.
	for i, arg := range call.args {
		at := -1
		if arg.name != "" {
			at = slices.Index(fn.params, arg.name)
		} else if fn.fields == nil {
			at = slices.Index(seen, false)
		}
		if at < 0 {
			return nil, nil, fmt.Errorf("%s: unknown argument %q or too many positional arguments; expected %v", fn.name, arg.name, fn.params)
		}
		if seen[at] {
			return nil, nil, fmt.Errorf("%s: duplicate argument %q", fn.name, fn.params[at])
		}
		seen[at], pos[i] = true, at
	}
	for i, used := range seen {
		if !used && (fn.fields == nil || !fn.fields[i].Type.Optional) {
			return nil, nil, fmt.Errorf("%s: missing argument %q", fn.name, fn.params[i])
		}
	}

	for i, arg := range call.args {
		v, err := s.eval(ctx, env, arg.x)
		if err != nil {
			return nil, nil, err
		}
		args[pos[i]] = v
	}
	return fn, args, nil
}

func (s *session) method(info plug.MethodInfo) *function {
	var method *plugimpl.Method[value, value]
	fn := &function{
		name:     info.Name,
		method:   &info,
		fields:   info.Fields,
		response: &info.Response,
	}
	for _, f := range info.Fields {
		fn.params = append(fn.params, f.Name)
	}
	fn.invoke = func(ctx context.Context, args []value) (value, error) {
		if info.Response.Name == "raw" {
			return value{}, fmt.Errorf("%s is a raw method; raw streams are not supported", info.Name)
		}
		r, err := s.coerceFields(info.Fields, args)
		if err != nil {
			return value{}, err
		}

		if s.dial != nil && s.client.Err() != nil {
			if err := s.reconnect(ctx); err != nil {
				return value{}, err
			}
		}

		if method == nil {
			m := plugimpl.NewMethod(s.schema, info.ID, s.fieldsCodec(info.Fields), s.codec(info.Response), s.errorCodec(info.Error))
			method = &m
		}
		return plug.Call(ctx, s.client, *method, value{data: r})
	}
	return fn
}
