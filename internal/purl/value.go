// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
	"strings"

	"go.astrophena.name/plug"
)

// Values and their contents are immutable after publication. typ preserves
// schema names for completion and display without a separate type checker.
type value struct {
	data any
	typ  *plug.TypeExpr
}

type field struct {
	name  string
	value value
}

type record struct {
	ctor   string
	fields []field
}

func (r *record) lookup(name string) (value, bool) {
	for _, f := range r.fields {
		if f.name == name {
			return f.value, true
		}
	}
	return value{}, false
}

func integer(n int64) value {
	return value{
		data: big.NewInt(n),
	}
}

func typeName(v value) string {
	if v.typ != nil {
		return schemaType(*v.typ)
	}
	switch v.data.(type) {
	case nil:
		return "nil"
	case bool:
		return "bool"
	case string:
		return "string"
	case []byte:
		return "bytes"
	case *big.Int:
		return "integer"
	case float64:
		return "f64"
	case []value:
		return "list"
	case *record:
		return "record"
	case *function:
		return "function"
	}
	return "value"
}

func schemaType(t plug.TypeExpr) string {
	var name string
	if t.Kind == plug.TypeExprList {
		name = "list<" + schemaType(*t.Element) + ">"
	} else {
		name = t.Name
	}
	if t.Optional {
		name += "?"
	}
	return name
}

func number(v value) (float64, bool) {
	switch n := v.data.(type) {
	case float64:
		return n, true
	case *big.Int:
		f, _ := n.Float64()
		return f, true
	}
	return 0, false
}

func compare(a, b value) (int, error) {
	if x, ok := a.data.(*big.Int); ok {
		if y, ok := b.data.(*big.Int); ok {
			return x.Cmp(y), nil
		}
	}
	if x, ok := number(a); ok {
		if y, ok := number(b); ok {
			// Compare mixed numbers exactly rather than rounding large integers.
			if math.IsNaN(x) || math.IsNaN(y) {
				return 0, fmt.Errorf("cannot order NaN")
			}
			_, aFloat := a.data.(float64)
			_, bFloat := b.data.(float64)
			if aFloat && math.IsInf(x, 0) || bFloat && math.IsInf(y, 0) {
				if aFloat && bFloat && x == y {
					return 0, nil
				}
				if aFloat && math.IsInf(x, -1) || bFloat && math.IsInf(y, 1) {
					return -1, nil
				}
				return 1, nil
			}
			toRat := func(v value) *big.Rat {
				if n, ok := v.data.(*big.Int); ok {
					return new(big.Rat).SetInt(n)
				}
				return new(big.Rat).SetFloat64(v.data.(float64))
			}
			return toRat(a).Cmp(toRat(b)), nil
		}
	}
	if x, ok := a.data.(string); ok {
		if y, ok := b.data.(string); ok {
			return strings.Compare(x, y), nil
		}
	}
	return 0, fmt.Errorf("cannot compare %s and %s", typeName(a), typeName(b))
}

func equal(a, b value) bool {
	switch x := a.data.(type) {
	case nil:
		return b.data == nil
	case bool:
		y, ok := b.data.(bool)
		return ok && x == y
	case string:
		y, ok := b.data.(string)
		return ok && x == y
	case []byte:
		y, ok := b.data.([]byte)
		return ok && bytes.Equal(x, y)
	case *big.Int, float64:
		cmp, err := compare(a, b)
		return err == nil && cmp == 0
	case []value:
		y, ok := b.data.([]value)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equal(x[i], y[i]) {
				return false
			}
		}
		return true
	case *record:
		y, ok := b.data.(*record)
		if !ok || x.ctor != y.ctor || len(x.fields) != len(y.fields) {
			return false
		}
		for _, f := range x.fields {
			v, ok := y.lookup(f.name)
			if !ok || !equal(f.value, v) {
				return false
			}
		}
		return true
	}
	return false
}

func binary(op string, a, b value) (value, error) {
	if op == "==" {
		return value{
			data: equal(a, b),
		}, nil
	}
	if op == "!=" {
		return value{
			data: !equal(a, b),
		}, nil
	}
	switch op {
	case "<", "<=", ">", ">=":
		n, err := compare(a, b)
		if err != nil {
			return value{}, err
		}
		return value{
			data: op == "<" && n < 0 || op == "<=" && n <= 0 || op == ">" && n > 0 || op == ">=" && n >= 0,
		}, nil
	case "+":
		if x, ok := a.data.(string); ok {
			if y, ok := b.data.(string); ok {
				return value{
					data: x + y,
				}, nil
			}
		}
	}
	if x, ok := a.data.(*big.Int); ok {
		if y, ok := b.data.(*big.Int); ok {
			z := new(big.Int)
			switch op {
			case "+":
				z.Add(x, y)
			case "-":
				z.Sub(x, y)
			case "*":
				z.Mul(x, y)
			case "/", "%":
				if y.Sign() == 0 {
					return value{}, fmt.Errorf("division by zero")
				}
				if op == "/" {
					z.Quo(x, y)
				} else {
					z.Rem(x, y)
				}
			default:
				return value{}, fmt.Errorf("invalid integer operator %q", op)
			}
			return value{
				data: z,
			}, nil
		}
	}
	x, xok := number(a)
	y, yok := number(b)
	if xok && yok {
		switch op {
		case "+":
			return value{
				data: x + y,
			}, nil
		case "-":
			return value{
				data: x - y,
			}, nil
		case "*":
			return value{
				data: x * y,
			}, nil
		case "/":
			if y == 0 {
				return value{}, fmt.Errorf("division by zero")
			}
			return value{
				data: x / y,
			}, nil
		}
	}
	return value{}, fmt.Errorf("operator %q does not accept %s and %s", op, typeName(a), typeName(b))
}
