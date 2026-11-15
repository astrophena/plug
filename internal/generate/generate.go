// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package generate emits Go, TypeScript, and JSON from Plug metadata.
// Inputs must come from [go.astrophena.name/plug/internal/schema.RuntimeSchema].
// Generation is deterministic and does not modify its input.
package generate

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/internal/doc"
)

type model struct {
	schema            plug.Schema
	types             map[string]plug.TypeInfo
	groupNames        []string
	constructors      []constructor
	errorConstructors []constructor
	methods           []plug.MethodInfo
	newtypes          []plug.TypeInfo
	httpMethods       []httpMethod
	hasHTTP           bool
}

type constructor struct {
	plug.ConstructorInfo
	result string
}

func newModel(s plug.Schema) model {
	m := model{
		schema:  s,
		types:   make(map[string]plug.TypeInfo, len(s.Types)),
		methods: s.Methods,
	}
	for _, typ := range s.Types {
		m.types[typ.Name] = typ
		if typ.Kind == plug.TypeNewtype {
			m.newtypes = append(m.newtypes, typ)
			continue
		}
		m.groupNames = append(m.groupNames, typ.Name)
		for _, c := range typ.Constructors {
			m.constructors = append(m.constructors, constructor{
				c,
				typ.Name,
			})
		}
	}
	slices.SortFunc(m.constructors, func(a, b constructor) int {
		return strings.Compare(a.Name, b.Name)
	})

	for _, c := range m.constructors {
		if m.types[c.result].Error {
			m.errorConstructors = append(m.errorConstructors, c)
		}
	}
	for _, method := range s.Methods {
		m.hasHTTP = m.hasHTTP || len(method.HTTP) > 0
	}
	return m
}

func (m *model) useHTTPMethods() {
	m.methods = nil
	for _, method := range m.schema.Methods {
		if len(method.HTTP) == 0 {
			continue
		}
		m.methods = append(m.methods, method)
		m.httpMethods = append(m.httpMethods, analyzeHTTPMethod(method))
	}
}

type httpMethod struct {
	method      plug.MethodInfo
	binding     plug.HTTPBindingInfo
	pathFields  []plug.FieldInfo
	queryFields []plug.FieldInfo
	bodyFields  []plug.FieldInfo
}

func analyzeHTTPMethod(m plug.MethodInfo) httpMethod {
	var (
		binding   = m.HTTP[0]
		params    = extractPathParams(binding.Path)
		pathNames = make(map[string]bool, len(params))
		fields    = make(map[string]plug.FieldInfo, len(m.Fields))
		result    = httpMethod{
			method:  m,
			binding: binding,
		}
	)
	for _, field := range m.Fields {
		fields[field.Name] = field
	}

	for _, name := range params {
		pathNames[name] = true
		result.pathFields = append(result.pathFields, fields[name])
	}

	for _, field := range m.Fields {
		if pathNames[field.Name] {
			continue
		}
		if binding.Verb == "GET" || binding.Verb == "DELETE" {
			result.queryFields = append(result.queryFields, field)
		} else {
			result.bodyFields = append(result.bodyFields, field)
		}
	}
	return result
}

func extractPathParams(path string) []string {
	var params []string
	for {
		_, rest, found := strings.Cut(path, "{")
		if !found {
			return params
		}
		name, rest, _ := strings.Cut(rest, "}")
		params = append(params, strings.TrimSuffix(name, "..."))
		path = rest
	}
}

func isRaw(ref plug.TypeExpr) bool {
	return ref.Kind == plug.TypeExprPrimitive && ref.Name == "raw" && !ref.Optional
}

type emitter struct {
	buf    bytes.Buffer
	indent string
	docs   *doc.Parser
}

func (e *emitter) line(format string, values ...any) {
	fmt.Fprintf(&e.buf, format+"\n", values...)
}

func (e *emitter) doc(name, sourceName, fallback, documentation string) {
	e.comments("", formatDocLines(name, sourceName, fallback, e.docs.Comment(documentation)))
}

func (e *emitter) docIndented(name, sourceName, fallback, documentation string) {
	e.comments(e.indent, formatDocLines(name, sourceName, fallback, e.docs.Comment(documentation)))
}

func (e *emitter) comments(indent string, lines []string) {
	// JavaScript also treats Unicode line separators as comment terminators.
	split := strings.NewReplacer("\r\n", "\n", "\r", "\n", "\u2028", "\n", "\u2029", "\n")
	for _, line := range lines {
		for line := range strings.SplitSeq(split.Replace(line), "\n") {
			if line == "" {
				e.line("%s//", indent)
			} else if strings.HasPrefix(line, "\t") {
				e.line("%s//%s", indent, line)
			} else {
				e.line("%s// %s", indent, line)
			}
		}
	}
}

func formatDocLines(name, sourceName, fallback, doc string) []string {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return []string{name + " " + fallback}
	}

	lines := strings.Split(doc, "\n")
	if strings.HasPrefix(lines[0], sourceName+" ") {
		lines[0] = name + lines[0][len(sourceName):]
		return lines
	}
	if strings.HasPrefix(lines[0], name+" ") {
		return lines
	}
	return append([]string{name + " " + fallback, ""}, lines...)
}
