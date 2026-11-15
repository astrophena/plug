// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"go.astrophena.name/plug/internal/names"
)

// validate checks schema semantics and generated Go-name safety.
func validate(s *Schema) error {
	if !validIdentifier(s.packageName) {
		return diagnostic(Position{
			Line:   1,
			Column: 1,
		}, "invalid package name %q", s.packageName)
	}

	for _, c := range s.categories {
		if c.name == "" {
			return diagnostic(c.position, "category name is empty")
		}
		if c.name != strings.TrimSpace(c.name) {
			return diagnostic(c.position, "category name %q has leading or trailing whitespace", c.name)
		}
		if len(c.types) == 0 && len(c.methods) == 0 {
			return diagnostic(c.position, "category %q contains no types or methods", c.name)
		}
	}

	permissions := make(map[string]Position)
	for _, p := range s.permissions {
		if err := checkQualifiedName(p.name, p.position, "permission"); err != nil {
			return err
		}
		if err := addUnique(permissions, p.name, p.position, "permission"); err != nil {
			return err
		}
	}

	var (
		types   = make(map[string]Position)
		goNames = make(map[string]Position)
	)
	for _, name := range []string{"Schema", "Client", "Handlers", "NewClient", "RegisterHandlers"} {
		goNames[name] = Position{
			Line:   1,
			Column: 1,
		}
	}
	if schemaHasHTTP(s) {
		goNames["HTTPRoutes"] = Position{
			Line:   1,
			Column: 1,
		}
	}
	var (
		constructors = make(map[string]Position)
		methods      = make(map[string]Position)
		handlers     = make(map[string]Position)
		groups       = make(map[string][]constructor)
		httpPatterns = make(map[string]Position)
		mux          http.ServeMux
	)

	for _, value := range s.newtypes {
		if err := checkName(value.name, value.position, "type"); err != nil {
			return err
		}
		if isPrimitive(value.name) {
			return diagnostic(value.position, "newtype name %q is reserved for a primitive", value.name)
		}
		if !isPrimitive(value.base) || value.base == "raw" || value.base == "void" {
			return diagnostic(value.position, "newtype %q must use a scalar primitive, not %q", value.name, value.base)
		}
		if err := addUnique(types, value.name, value.position, "type"); err != nil {
			return err
		}
		if err := addUnique(goNames, names.Go(value.name), value.position, "generated Go declaration"); err != nil {
			return err
		}
	}

	for _, c := range s.constructors {
		if err := checkName(c.name, c.position, "constructor"); err != nil {
			return err
		}
		if err := checkName(c.result, c.position, "result type"); err != nil {
			return err
		}
		if isPrimitive(c.result) {
			return diagnostic(c.position, "constructor result %q is a primitive", c.result)
		}
		if err := addUnique(constructors, c.name, c.position, "constructor"); err != nil {
			return err
		}
		if err := addUnique(goNames, "constructorID"+names.Go(c.name), c.position, "generated Go declaration"); err != nil {
			return err
		}
		if err := validateFields(c.fields); err != nil {
			return err
		}
		groups[c.result] = append(groups[c.result], c)
	}

	for result, group := range groups {
		pos := group[0].position
		if err := addUnique(types, result, pos, "type"); err != nil {
			return err
		}
		if err := addUnique(goNames, names.Go(result), pos, "generated Go declaration"); err != nil {
			return err
		}
		if len(group) > 1 {
			if err := addUnique(goNames, "Unknown"+names.Go(result), pos, "generated Go declaration"); err != nil {
				return err
			}
			for _, c := range group {
				if err := addUnique(goNames, names.Go(c.name), c.position, "generated Go declaration"); err != nil {
					return err
				}
			}
		}
	}

	newtypeMap := make(map[string]Position, len(s.newtypes))
	for _, nt := range s.newtypes {
		newtypeMap[nt.name] = nt.position
	}

	declaredTypeNames := make(map[string]Position)
	for _, dt := range s.types {
		if err := checkName(dt.name, dt.position, "type"); err != nil {
			return err
		}
		if isPrimitive(dt.name) {
			return diagnostic(dt.position, "type name %q is reserved for a primitive", dt.name)
		}
		if prevPos, ok := newtypeMap[dt.name]; ok {
			return diagnostic(dt.position, "result type declaration %q conflicts with scalar newtype declared at %s", dt.name, prevPos)
		}
		if _, ok := groups[dt.name]; !ok {
			return diagnostic(dt.position, "declared result type %q is not constructed by any constructor", dt.name)
		}
		if err := addUnique(declaredTypeNames, dt.name, dt.position, "type declaration"); err != nil {
			return err
		}
	}

	for _, m := range s.methods {
		if err := checkQualifiedName(m.name, m.position, "method"); err != nil {
			return err
		}
		goName := names.GoMethod(m.name)
		if goName == "Close" && !isRaw(m.response) {
			return diagnostic(m.position, "method %q conflicts with generated Client.Close", m.name)
		}
		if err := addUnique(methods, m.name, m.position, "method"); err != nil {
			return err
		}
		if err := addUnique(handlers, goName, m.position, "generated Go declaration"); err != nil {
			return err
		}
		if isRaw(m.response) {
			if err := addUnique(goNames, "Open"+goName, m.position, "generated Go declaration"); err != nil {
				return err
			}
		}
		if len(m.fields) > 0 {
			if err := addUnique(goNames, goName+"Request", m.position, "generated Go declaration"); err != nil {
				return err
			}
		}
		if err := addUnique(goNames, "methodID"+goName, m.position, "generated Go declaration"); err != nil {
			return err
		}
		if err := addUnique(goNames, "method"+goName, m.position, "generated Go declaration"); err != nil {
			return err
		}
		if err := validateFields(m.fields); err != nil {
			return err
		}
		if m.public == (m.permission != "") {
			return diagnostic(m.position, "method %q must be public or require exactly one permission", m.name)
		}
		if m.permission != "" {
			if _, ok := permissions[m.permission]; !ok {
				return diagnostic(m.position, "method %q requires undeclared permission %q", m.name, m.permission)
			}
		}
		for _, binding := range m.http {
			if isRaw(m.response) {
				return diagnostic(binding.position, "raw method %q cannot have an HTTP binding", m.name)
			}
			if err := validateHTTPBinding(binding, m.fields); err != nil {
				return err
			}
			pattern := binding.verb + " " + binding.path
			if err := addUnique(httpPatterns, pattern, binding.position, "HTTP binding"); err != nil {
				return err
			}
			if err := checkHTTPPattern(&mux, pattern); err != nil {
				return diagnostic(binding.position, "invalid HTTP binding: %v", err)
			}
		}
	}

	var (
		typeCategories   = make(map[string]string)
		methodCategories = make(map[string]string)
	)
	for _, c := range s.categories {
		for _, typ := range c.types {
			if _, ok := types[typ]; !ok {
				return diagnostic(c.position, "category %q contains unknown type %q", c.name, typ)
			}
			if previous, ok := typeCategories[typ]; ok {
				return diagnostic(c.position, "type %q appears in categories %q and %q", typ, previous, c.name)
			}
			typeCategories[typ] = c.name
		}
		for _, m := range c.methods {
			if _, ok := methods[m]; !ok {
				return diagnostic(c.position, "category %q contains unknown method %q", c.name, m)
			}
			if previous, ok := methodCategories[m]; ok {
				return diagnostic(c.position, "method %q appears in categories %q and %q", m, previous, c.name)
			}
			methodCategories[m] = c.name
		}
	}

	known := func(name string) bool {
		return isPrimitive(name) || hasKey(types, name)
	}
	for _, c := range s.constructors {
		for _, f := range c.fields {
			if err := validateTypeRef(f.typ, f.position, false, known); err != nil {
				return err
			}
		}
	}

	errorTypes := make(map[string]Position)
	for _, m := range s.methods {
		for _, f := range m.fields {
			if err := validateTypeRef(f.typ, f.position, false, known); err != nil {
				return err
			}
		}
		fields := make(map[string]field, len(m.fields))
		for _, f := range m.fields {
			fields[f.name] = f
		}
		for _, binding := range m.http {
			params, _ := pathParameters(binding.path)
			for _, param := range params {
				if !httpTextType(fields[param].typ, s.newtypes, false) {
					return diagnostic(binding.position, "HTTP path parameter %q must be a scalar field", param)
				}
				if strings.Contains(binding.path, "{"+param+"...}") {
					name := fields[param].typ.name
					for _, value := range s.newtypes {
						if value.name == name {
							name = value.base
							break
						}
					}
					if name != "string" {
						return diagnostic(binding.position, "HTTP wildcard parameter %q must be a string field", param)
					}
				}
			}
			if binding.verb == "GET" || binding.verb == "DELETE" {
				for _, f := range m.fields {
					if !httpTextType(f.typ, s.newtypes, true) {
						return diagnostic(binding.position, "%s binding field %q must be a scalar or list of scalars", binding.verb, f.name)
					}
				}
			}
		}
		if err := validateTypeRef(m.response, m.position, true, known); err != nil {
			return err
		}
		if m.errorType != nil {
			if m.errorType.optional || m.errorType.element != nil || m.errorType.name == "" {
				return diagnostic(m.position, "method error must be a named constructor type")
			}
			if _, ok := groups[m.errorType.name]; !ok {
				return diagnostic(m.position, "method error %q is not a constructor type", m.errorType.name)
			}
			errorTypes[m.errorType.name] = m.position
		}
	}

	for _, c := range s.constructors {
		if _, ok := errorTypes[c.result]; ok {
			for _, f := range c.fields {
				if names.Go(f.name) == "Error" {
					return diagnostic(f.position, "error constructor field %q conflicts with the generated Error method", f.name)
				}
			}
		} else if c.httpStatus != 0 {
			return diagnostic(c.position, "HTTP status is only valid on declared error constructors")
		}
		if c.httpStatus != 0 && (c.httpStatus < 400 || c.httpStatus > 599) {
			return diagnostic(c.position, "invalid error HTTP status %d", c.httpStatus)
		}
		for _, f := range c.fields {
			if name := referencedName(f.typ, errorTypes); name != "" {
				return diagnostic(f.position, "error type %q cannot be used as a field type", name)
			}
		}
	}
	for _, m := range s.methods {
		for _, f := range m.fields {
			if name := referencedName(f.typ, errorTypes); name != "" {
				return diagnostic(f.position, "error type %q cannot be used as a request field", name)
			}
		}
		if name := referencedName(m.response, errorTypes); name != "" {
			return diagnostic(m.position, "error type %q cannot be used as a response", name)
		}
	}

	return rejectRecursiveTypes(groups)
}

func httpTextType(ref typeRef, newtypes []newtype, allowList bool) bool {
	if ref.element != nil {
		return allowList && httpTextType(*ref.element, newtypes, false)
	}
	if ref.name == "void" || ref.name == "raw" {
		return false
	}
	if isPrimitive(ref.name) {
		return true
	}
	return slices.ContainsFunc(newtypes, func(value newtype) bool {
		return value.name == ref.name
	})
}

func validateFields(fields []field) error {
	var (
		fieldNames = make(map[string]Position)
		goNames    = make(map[string]Position)
		optional   bool
	)
	for _, f := range fields {
		if err := checkName(f.name, f.position, "field"); err != nil {
			return err
		}
		if err := addUnique(fieldNames, f.name, f.position, "field"); err != nil {
			return err
		}
		if err := addUnique(goNames, names.Go(f.name), f.position, "generated Go field"); err != nil {
			return err
		}
		if f.typ.optional {
			optional = true
		} else if optional {
			return diagnostic(f.position, "required field %q follows an optional extension field", f.name)
		}
	}
	return nil
}

func validateTypeRef(ref typeRef, pos Position, response bool, known func(string) bool) error {
	if ref.element != nil {
		if ref.element.optional {
			return diagnostic(pos, "list elements cannot be optional; wrap the list instead")
		}
		return validateTypeRef(*ref.element, pos, false, known)
	}
	if !known(ref.name) {
		return diagnostic(pos, "unknown type %q", ref.name)
	}
	if (ref.name == "void" || ref.name == "raw") && (!response || ref.optional) {
		return diagnostic(pos, "%s is only valid as a non-optional method response", ref.name)
	}
	return nil
}

func rejectRecursiveTypes(groups map[string][]constructor) error {
	edges := make(map[string][]string)
	for result, constructors := range groups {
		for _, c := range constructors {
			for _, f := range c.fields {
				if name := namedType(f.typ); name != "" && hasKey(groups, name) {
					edges[result] = append(edges[result], name)
				}
			}
		}
	}

	const (
		unseen uint8 = iota
		visiting
		done
	)
	var (
		state = make(map[string]uint8)
		stack []string
		visit func(string) error
	)
	visit = func(name string) error {
		state[name] = visiting
		stack = append(stack, name)
		for _, dependency := range edges[name] {
			switch state[dependency] {
			case unseen:
				if err := visit(dependency); err != nil {
					return err
				}
			case visiting:
				start := slices.Index(stack, dependency)
				cycle := append(slices.Clone(stack[start:]), dependency)
				return diagnostic(groups[name][0].position, "recursive type cycle is not supported: %v", cycle)
			}
		}
		stack = stack[:len(stack)-1]
		state[name] = done
		return nil
	}
	for name := range groups {
		if state[name] == unseen {
			if err := visit(name); err != nil {
				return err
			}
		}
	}
	return nil
}

func namedType(ref typeRef) string {
	if ref.element != nil {
		return namedType(*ref.element)
	}
	return ref.name
}

func referencedName(ref typeRef, names map[string]Position) string {
	name := namedType(ref)
	if hasKey(names, name) {
		return name
	}
	return ""
}

func addUnique(values map[string]Position, name string, pos Position, kind string) error {
	if previous, ok := values[name]; ok {
		return diagnostic(pos, "duplicate %s %q; first declared at %s", kind, name, previous)
	}
	values[name] = pos
	return nil
}

func checkName(name string, pos Position, kind string) error {
	if !validIdentifier(name) {
		return diagnostic(pos, "invalid %s name %q", kind, name)
	}
	if slices.Contains([]string{
		"http",
		"httpStatus",
		"list",
		"package",
		"permission",
		"public",
		"requires",
		"throws",
		"type",
		"version",
	}, name) {
		return diagnostic(pos, "%s name %q is reserved", kind, name)
	}
	return nil
}

func checkQualifiedName(name string, pos Position, kind string) error {
	group, m, ok := strings.Cut(name, ".")
	if !ok || group == "" || m == "" || strings.Contains(m, ".") {
		return diagnostic(pos, "%s name %q must have the form group.name", kind, name)
	}
	if err := checkName(group, pos, kind+" group"); err != nil {
		return err
	}
	return checkName(m, pos, kind)
}

func validateHTTPBinding(binding httpBinding, fields []field) error {
	if !slices.Contains([]string{"DELETE", "GET", "PATCH", "POST", "PUT"}, binding.verb) {
		return diagnostic(binding.position, "unsupported HTTP verb %q", binding.verb)
	}
	params, err := pathParameters(binding.path)
	if err != nil {
		return diagnostic(binding.position, "invalid HTTP path %q: %v", binding.path, err)
	}
	fieldNames := make(map[string]bool, len(fields))
	for _, f := range fields {
		fieldNames[f.name] = true
	}
	for _, param := range params {
		if !fieldNames[param] {
			return diagnostic(binding.position, "HTTP path parameter %q is not a request field", param)
		}
	}
	return nil
}

func checkHTTPPattern(mux *http.ServeMux, pattern string) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("%v", v)
		}
	}()
	mux.HandleFunc(pattern, func(http.ResponseWriter, *http.Request) {})
	return nil
}

func pathParameters(path string) ([]string, error) {
	if !strings.HasPrefix(path, "/") {
		return nil, errors.New("path must start with '/'")
	}
	if strings.ContainsAny(path, "?#") {
		return nil, errors.New("query strings and fragments are not allowed")
	}
	var (
		result []string
		seen   = make(map[string]bool)
	)
	for offset := 0; offset < len(path); {
		start := strings.IndexByte(path[offset:], '{')
		if start < 0 {
			if strings.ContainsRune(path[offset:], '}') {
				return nil, errors.New("unmatched '}'")
			}
			break
		}
		start += offset
		if strings.ContainsRune(path[offset:start], '}') {
			return nil, errors.New("unmatched '}'")
		}
		end := strings.IndexByte(path[start+1:], '}')
		if end < 0 {
			return nil, errors.New("unmatched '{'")
		}
		end += start + 1
		name := strings.TrimSuffix(path[start+1:end], "...")
		if !validIdentifier(name) {
			return nil, fmt.Errorf("invalid parameter %q", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate parameter %q", name)
		}
		seen[name] = true
		result = append(result, name)
		offset = end + 1
	}
	return result, nil
}

func hasKey[K comparable, V any](values map[K]V, key K) bool {
	_, ok := values[key]
	return ok
}

func schemaHasHTTP(s *Schema) bool {
	for _, m := range s.methods {
		if len(m.http) > 0 {
			return true
		}
	}
	return false
}
