// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func builtins() map[string]*function {
	funcs := make(map[string]*function)
	add := func(name string, params []string, doc string, invoke func(context.Context, []value) (value, error)) {
		funcs[name] = &function{
			name:   name,
			params: params,
			doc:    doc,
			invoke: invoke,
		}
	}

	for name, doc := range map[string]string{
		"map":      "Apply fn to each item.",
		"filter":   "Keep items whose predicate is true.",
		"sortBy":   "Stable-sort items by their computed keys.",
		"find":     "First match, or nil; stop at the first match.",
		"any":      "Whether any item matches; stop on true.",
		"all":      "Whether all items match; stop on false.",
		"uniqueBy": "Keep the first item for each equal key.",
		"groupBy":  "Group equal keys into {key, items} records in first-seen order.",
	} {
		add(name, []string{"items", "fn"}, doc, func(ctx context.Context, args []value) (value, error) {
			xs, ok := args[0].data.([]value)
			if !ok {
				return value{}, fmt.Errorf("%s: expected a list", name)
			}
			fn, ok := args[1].data.(*function)
			if !ok || len(fn.params) != 1 {
				return value{}, fmt.Errorf("%s: expected a function of one argument", name)
			}

			switch name {
			case "sortBy":
				return sortList(ctx, xs, fn)
			case "uniqueBy", "groupBy":
				return groupList(ctx, name, xs, fn)
			default:
				return walkList(ctx, name, xs, fn)
			}
		})
	}

	for name, match := range map[string]struct {
		test func(string, string) bool
		doc  string
	}{
		"contains": {
			strings.Contains,
			"Case-sensitive substring match.",
		},
		"startsWith": {
			strings.HasPrefix,
			"Case-sensitive prefix match.",
		},
		"endsWith": {
			strings.HasSuffix,
			"Case-sensitive suffix match.",
		},
	} {
		add(name, []string{"text", "part"}, match.doc, func(ctx context.Context, args []value) (value, error) {
			text, textOK := args[0].data.(string)
			part, partOK := args[1].data.(string)
			if !textOK || !partOK {
				return value{}, fmt.Errorf("%s: expected two strings", name)
			}
			return value{data: match.test(text, part)}, nil
		})
	}

	add("sum", []string{"items"}, "Sum numbers; empty lists yield 0.", func(ctx context.Context, args []value) (value, error) {
		xs, ok := args[0].data.([]value)
		if !ok {
			return value{}, fmt.Errorf("sum: expected a list")
		}
		total := integer(0)
		for _, x := range xs {
			if err := ctx.Err(); err != nil {
				return value{}, err
			}
			// The accumulator owns its integer storage until the result is returned.
			if z, ok := total.data.(*big.Int); ok {
				if n, ok := x.data.(*big.Int); ok {
					z.Add(z, n)
					continue
				}
			}
			if _, ok := number(x); !ok {
				return value{}, fmt.Errorf("sum: expected numbers")
			}
			var err error
			total, err = binary("+", total, x)
			if err != nil {
				return value{}, err
			}
		}
		return total, nil
	})

	add("age", []string{"timestamp"}, "Whole seconds since an RFC 3339 timestamp; future is negative.", func(ctx context.Context, args []value) (value, error) {
		text, ok := args[0].data.(string)
		if !ok {
			return value{}, fmt.Errorf("age: expected an RFC 3339 timestamp string")
		}
		t, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return value{}, fmt.Errorf("age: %w", err)
		}
		now := time.Now()
		seconds := now.Unix() - t.Unix()
		if seconds > 0 && now.Nanosecond() < t.Nanosecond() {
			seconds--
		} else if seconds < 0 && now.Nanosecond() > t.Nanosecond() {
			seconds++
		}
		return integer(seconds), nil
	})

	add("assert", []string{"condition", "message"}, "Return nil on true; otherwise fail with message.", func(ctx context.Context, args []value) (value, error) {
		condition, ok := args[0].data.(bool)
		if !ok {
			return value{}, fmt.Errorf("assert: expected a bool condition")
		}
		message, ok := args[1].data.(string)
		if !ok {
			return value{}, fmt.Errorf("assert: expected a string message")
		}
		if !condition {
			return value{}, fmt.Errorf("assert: %s", message)
		}
		return value{}, nil
	})

	add("take", []string{"items", "n"}, "First n items.", func(ctx context.Context, args []value) (value, error) {
		xs, ok := args[0].data.([]value)
		if !ok {
			return value{}, fmt.Errorf("take: expected a list")
		}
		n, ok := args[1].data.(*big.Int)
		if !ok || n.Sign() < 0 || !n.IsInt64() {
			return value{}, fmt.Errorf("take: expected a nonnegative integer")
		}
		return value{data: xs[:min(int64(len(xs)), n.Int64())]}, nil
	})

	add("count", []string{"items"}, "Count items, fields, bytes, or Unicode characters.", func(ctx context.Context, args []value) (value, error) {
		switch x := args[0].data.(type) {
		case []value:
			return integer(int64(len(x))), nil
		case []byte:
			return integer(int64(len(x))), nil
		case string:
			return integer(int64(utf8.RuneCountInString(x))), nil
		case *record:
			return integer(int64(len(x.fields))), nil
		}
		return value{}, fmt.Errorf("count: expected a list, record, string, or bytes")
	})

	add("keys", []string{"record"}, "List record field names.", func(ctx context.Context, args []value) (value, error) {
		r, ok := args[0].data.(*record)
		if !ok {
			return value{}, fmt.Errorf("keys: expected a record")
		}
		out := make([]value, 0, len(r.fields))
		for _, f := range r.fields {
			out = append(out, value{data: f.name})
		}
		return value{data: out}, nil
	})

	add("parseJSON", []string{"text"}, "Parse JSON without losing integer precision.", func(ctx context.Context, args []value) (value, error) {
		text, ok := args[0].data.(string)
		if !ok {
			return value{}, fmt.Errorf("parseJSON: expected a string")
		}
		d := json.NewDecoder(strings.NewReader(text))
		d.UseNumber()
		v, err := readJSON(d, 0)
		if err != nil {
			return value{}, err
		}
		if _, err := d.Token(); err != io.EOF {
			return value{}, fmt.Errorf("parseJSON: trailing data")
		}
		return v, nil
	})

	add("toJSON", []string{"value"}, "Encode a value as JSON.", func(ctx context.Context, args []value) (value, error) {
		text, err := marshalJSON(args[0])
		return value{data: text}, err
	})

	add("bytes", []string{"text"}, "Encode a string as UTF-8 bytes.", func(ctx context.Context, args []value) (value, error) {
		text, ok := args[0].data.(string)
		if !ok {
			return value{}, fmt.Errorf("bytes: expected a string")
		}
		return value{data: []byte(text)}, nil
	})

	add("text", []string{"bytes"}, "Decode UTF-8 bytes into a string.", func(ctx context.Context, args []value) (value, error) {
		b, ok := args[0].data.([]byte)
		if !ok || !utf8.Valid(b) {
			return value{}, fmt.Errorf("text: expected UTF-8 bytes")
		}
		return value{data: string(b)}, nil
	})

	return funcs
}

func walkList(ctx context.Context, name string, xs []value, fn *function) (value, error) {
	var out []value
	if name == "map" || name == "filter" {
		out = make([]value, 0, len(xs))
	}
	for _, x := range xs {
		if err := ctx.Err(); err != nil {
			return value{}, err
		}
		v, err := fn.call(ctx, []value{x})
		if err != nil {
			return value{}, err
		}
		if name == "map" {
			out = append(out, v)
			continue
		}

		b, ok := v.data.(bool)
		if !ok {
			return value{}, fmt.Errorf("%s: predicate must return bool", name)
		}
		if name == "find" && b {
			return x, nil
		}
		if name == "any" && b || name == "all" && !b {
			return v, nil
		}
		if name == "filter" && b {
			out = append(out, x)
		}
	}

	switch name {
	case "find":
		return value{}, nil
	case "any", "all":
		return value{data: name == "all"}, nil
	default:
		return value{data: out}, nil
	}
}

func sortList(ctx context.Context, xs []value, fn *function) (value, error) {
	var (
		keys  = make([]value, len(xs))
		order = make([]int, len(xs))
	)
	for i, x := range xs {
		if err := ctx.Err(); err != nil {
			return value{}, err
		}
		v, err := fn.call(ctx, []value{x})
		if err != nil {
			return value{}, err
		}
		keys[i] = v
		order[i] = i
	}

	var sortErr error
	slices.SortStableFunc(order, func(i, j int) int {
		cmp, err := compare(keys[i], keys[j])
		if err != nil {
			sortErr = err
		}
		return cmp
	})
	if sortErr != nil {
		return value{}, sortErr
	}

	out := make([]value, 0, len(xs))
	for _, i := range order {
		out = append(out, xs[i])
	}
	return value{data: out}, nil
}

func groupList(ctx context.Context, name string, xs []value, fn *function) (value, error) {
	var (
		group  = name == "groupBy"
		out    = xs
		keys   = make([]value, 0, len(xs))
		groups [][]value
		index  map[any]int
		copied bool
	)

	if group {
		groups = make([][]value, 0, len(xs))
	}

	for i, x := range xs {
		if err := ctx.Err(); err != nil {
			return value{}, err
		}
		v, err := fn.call(ctx, []value{x})
		if err != nil {
			return value{}, err
		}
		if !equal(v, v) {
			return value{}, fmt.Errorf("%s: key must support equality", name)
		}
		if len(keys) > 0 {
			if err := ctx.Err(); err != nil {
				return value{}, err
			}
		}

		// Keep small groups cheap; index scalar keys once the scan gets large.
		if index == nil && len(keys) == 32 {
			index = make(map[any]int, len(xs))
			for i, key := range keys {
				if k, ok := groupKey(key); ok {
					index[k] = i
				}
			}
		}

		var (
			key    any
			scalar bool
			at     = -1
		)
		if index != nil {
			key, scalar = groupKey(v)
		}
		if scalar {
			if i, found := index[key]; found {
				at = i
			}
		} else {
			for i, key := range keys {
				if err := ctx.Err(); err != nil {
					return value{}, err
				}
				if equal(key, v) {
					at = i
					break
				}
			}
		}

		if at < 0 {
			at = len(keys)
			keys = append(keys, v)
			if scalar {
				index[key] = at
			}
			if group {
				groups = append(groups, nil)
			} else if copied {
				out = append(out, x)
			}
		} else if !group && !copied {
			// An all-unique result can share the immutable input.
			out = make([]value, i, len(xs))
			copy(out, xs[:i])
			copied = true
		}
		if group {
			groups[at] = append(groups[at], x)
		}
	}

	if !group {
		return value{data: out}, nil
	}

	out = make([]value, 0, len(keys))
	for i, key := range keys {
		out = append(out, value{
			data: &record{
				fields: []field{
					{
						name:  "key",
						value: key,
					},
					{
						name:  "items",
						value: value{data: groups[i]},
					},
				},
			},
		})
	}
	return value{data: out}, nil
}

type numKey string
type bytesKey string

// groupKey shares numeric equality across scalar keys. Composites keep the scan.
func groupKey(v value) (any, bool) {
	switch x := v.data.(type) {
	case nil, bool, string:
		return v.data, true
	case []byte:
		return bytesKey(x), true
	case *big.Int:
		if x.IsInt64() {
			return x.Int64(), true
		}
		return numKey(x.String()), true
	case float64:
		if x >= math.MinInt64 && x < math.MaxInt64 && x == math.Trunc(x) {
			return int64(x), true
		}
		if math.IsInf(x, 0) {
			return numKey(strconv.FormatFloat(x, 'g', -1, 64)), true
		}
		return numKey(new(big.Rat).SetFloat64(x).RatString()), true
	}
	return nil, false
}

func readJSON(d *json.Decoder, depth int) (value, error) {
	if depth > maxDepth {
		return value{}, fmt.Errorf("JSON is too deeply nested")
	}
	t, err := d.Token()
	if err != nil {
		return value{}, err
	}
	switch t := t.(type) {
	case json.Number:
		if z, ok := new(big.Int).SetString(string(t), 10); ok {
			return value{data: z}, nil
		}

		f, err := strconv.ParseFloat(string(t), 64)
		return value{data: f}, err
	case json.Delim:
		if t == '[' {
			xs := make([]value, 0)
			for d.More() {
				v, err := readJSON(d, depth+1)
				if err != nil {
					return value{}, err
				}
				xs = append(xs, v)
			}
			_, err := d.Token()
			return value{data: xs}, err
		}

		if t == '{' {
			r := &record{}
			seen := make(map[string]bool)
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return value{}, err
				}
				name := key.(string)
				if seen[name] {
					return value{}, fmt.Errorf("duplicate JSON field %q", name)
				}
				seen[name] = true
				v, err := readJSON(d, depth+1)
				if err != nil {
					return value{}, err
				}
				r.fields = append(r.fields, field{
					name:  name,
					value: v,
				})
			}

			_, err := d.Token()
			return value{data: r}, err
		}
		return value{}, fmt.Errorf("unexpected JSON delimiter %q", t)
	default:
		return value{data: t}, nil
	}
}

func marshalJSON(v value) (string, error) {
	var b strings.Builder
	err := writeJSON(&b, v, 0)
	return b.String(), err
}

func writeJSON(b *strings.Builder, v value, depth int) error {
	if depth > maxDepth {
		return fmt.Errorf("value is too deeply nested")
	}
	switch x := v.data.(type) {
	case *big.Int:
		b.WriteString(x.String())
	case []value:
		b.WriteByte('[')
		for i, x := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeJSON(b, x, depth+1); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case *record:
		b.WriteByte('{')
		comma := false
		if x.ctor != "" {
			b.WriteString(`"type":`)
			if err := jsonv2.MarshalWrite(b, x.ctor, json.DefaultOptionsV1()); err != nil {
				return err
			}
			comma = true
		}
		for _, f := range x.fields {
			if comma {
				b.WriteByte(',')
			}
			if err := jsonv2.MarshalWrite(b, f.name, json.DefaultOptionsV1()); err != nil {
				return err
			}
			b.WriteByte(':')
			if err := writeJSON(b, f.value, depth+1); err != nil {
				return err
			}
			comma = true
		}
		b.WriteByte('}')
	case *function:
		return fmt.Errorf("cannot encode a function as JSON")
	case []byte:
		return jsonv2.MarshalWrite(b, base64.StdEncoding.EncodeToString(x), json.DefaultOptionsV1())
	case string:
		return jsonv2.MarshalWrite(b, x, json.DefaultOptionsV1())
	default:
		data, err := json.Marshal(x)
		if err != nil {
			return err
		}
		b.Write(data)
	}
	return nil
}
