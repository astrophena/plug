// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"fmt"
	"hash/crc32"
	"net/http"
	"slices"
	"strings"

	"go.astrophena.name/plug/internal/names"
)

// validate checks schema semantics and generated Go-name safety.
func validate(schema *Schema) error {
	if !validIdentifier(schema.packageName) {
		return diagnostic(Position{Line: 1, Column: 1}, "invalid package name %q", schema.packageName)
	}

	for _, category := range schema.categories {
		if category.name == "" {
			return diagnostic(category.position, "category name is empty")
		}
		if category.name != strings.TrimSpace(category.name) {
			return diagnostic(category.position, "category name %q has leading or trailing whitespace", category.name)
		}
		if len(category.types) == 0 && len(category.methods) == 0 {
			return diagnostic(category.position, "category %q contains no types or methods", category.name)
		}
	}

	permissions := make(map[string]Position)
	for _, permission := range schema.permissions {
		if err := checkQualifiedName(permission.name, permission.position, "permission"); err != nil {
			return err
		}
		if err := addUnique(permissions, permission.name, permission.position, "permission"); err != nil {
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
	if schemaHasHTTP(schema) {
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

	for _, value := range schema.newtypes {
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

	for _, constructor := range schema.constructors {
		if err := checkName(constructor.name, constructor.position, "constructor"); err != nil {
			return err
		}
		if err := checkName(constructor.result, constructor.position, "result type"); err != nil {
			return err
		}
		if isPrimitive(constructor.result) {
			return diagnostic(constructor.position, "constructor result %q is a primitive", constructor.result)
		}
		if err := addUnique(constructors, constructor.name, constructor.position, "constructor"); err != nil {
			return err
		}
		if err := addUnique(goNames, "constructorID"+names.Go(constructor.name), constructor.position, "generated Go declaration"); err != nil {
			return err
		}
		if err := validateFields(constructor.fields); err != nil {
			return err
		}
		groups[constructor.result] = append(groups[constructor.result], constructor)
	}

	for result, group := range groups {
		position := group[0].position
		if err := addUnique(types, result, position, "type"); err != nil {
			return err
		}
		if err := addUnique(goNames, names.Go(result), position, "generated Go declaration"); err != nil {
			return err
		}
		if len(group) > 1 {
			if err := addUnique(goNames, "Unknown"+names.Go(result), position, "generated Go declaration"); err != nil {
				return err
			}
			for _, constructor := range group {
				if err := addUnique(goNames, names.Go(constructor.name), constructor.position, "generated Go declaration"); err != nil {
					return err
				}
			}
		}
	}

	newtypeMap := make(map[string]Position, len(schema.newtypes))
	for _, nt := range schema.newtypes {
		newtypeMap[nt.name] = nt.position
	}

	declaredTypeNames := make(map[string]Position)
	for _, dt := range schema.types {
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

	for _, method := range schema.methods {
		if err := checkQualifiedName(method.name, method.position, "method"); err != nil {
			return err
		}
		goName := names.GoMethod(method.name)
		if goName == "Close" && !isRaw(method.response) {
			return diagnostic(method.position, "method %q conflicts with generated Client.Close", method.name)
		}
		if err := addUnique(methods, method.name, method.position, "method"); err != nil {
			return err
		}
		if err := addUnique(handlers, goName, method.position, "generated Go declaration"); err != nil {
			return err
		}
		if isRaw(method.response) {
			if err := addUnique(goNames, "Open"+goName, method.position, "generated Go declaration"); err != nil {
				return err
			}
		}
		if len(method.fields) > 0 {
			if err := addUnique(goNames, goName+"Request", method.position, "generated Go declaration"); err != nil {
				return err
			}
		}
		if err := addUnique(goNames, "methodID"+goName, method.position, "generated Go declaration"); err != nil {
			return err
		}
		if err := addUnique(goNames, "method"+goName, method.position, "generated Go declaration"); err != nil {
			return err
		}
		if err := validateFields(method.fields); err != nil {
			return err
		}
		if method.public == (method.permission != "") {
			return diagnostic(method.position, "method %q must be public or require exactly one permission", method.name)
		}
		if method.permission != "" {
			if _, ok := permissions[method.permission]; !ok {
				return diagnostic(method.position, "method %q requires undeclared permission %q", method.name, method.permission)
			}
		}
		for _, binding := range method.http {
			if isRaw(method.response) {
				return diagnostic(binding.position, "raw method %q cannot have an HTTP binding", method.name)
			}
			if err := validateHTTPBinding(binding, method.fields); err != nil {
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

	categorizedTypes := make(map[string]string)
	categorizedMethods := make(map[string]string)
	for _, category := range schema.categories {
		for _, typ := range category.types {
			if _, ok := types[typ]; !ok {
				return diagnostic(category.position, "category %q contains unknown type %q", category.name, typ)
			}
			if previous, ok := categorizedTypes[typ]; ok {
				return diagnostic(category.position, "type %q appears in categories %q and %q", typ, previous, category.name)
			}
			categorizedTypes[typ] = category.name
		}
		for _, method := range category.methods {
			if _, ok := methods[method]; !ok {
				return diagnostic(category.position, "category %q contains unknown method %q", category.name, method)
			}
			if previous, ok := categorizedMethods[method]; ok {
				return diagnostic(category.position, "method %q appears in categories %q and %q", method, previous, category.name)
			}
			categorizedMethods[method] = category.name
		}
	}

	known := func(name string) bool {
		return isPrimitive(name) || hasKey(types, name)
	}
	for _, constructor := range schema.constructors {
		for _, field := range constructor.fields {
			if err := validateTypeRef(field.typ, field.position, false, known); err != nil {
				return err
			}
		}
	}

	errorTypes := make(map[string]Position)
	for _, method := range schema.methods {
		for _, field := range method.fields {
			if err := validateTypeRef(field.typ, field.position, false, known); err != nil {
				return err
			}
		}
		fields := make(map[string]field, len(method.fields))
		for _, field := range method.fields {
			fields[field.name] = field
		}
		for _, binding := range method.http {
			parameters, _ := pathParameters(binding.path)
			for _, parameter := range parameters {
				if !httpTextType(fields[parameter].typ, schema.newtypes, false) {
					return diagnostic(binding.position, "HTTP path parameter %q must be a scalar field", parameter)
				}
				if strings.Contains(binding.path, "{"+parameter+"...}") {
					name := fields[parameter].typ.name
					for _, value := range schema.newtypes {
						if value.name == name {
							name = value.base
							break
						}
					}
					if name != "string" {
						return diagnostic(binding.position, "HTTP wildcard parameter %q must be a string field", parameter)
					}
				}
			}
			if binding.verb == "GET" || binding.verb == "DELETE" {
				for _, field := range method.fields {
					if !httpTextType(field.typ, schema.newtypes, true) {
						return diagnostic(binding.position, "%s binding field %q must be a scalar or list of scalars", binding.verb, field.name)
					}
				}
			}
		}
		if err := validateTypeRef(method.response, method.position, true, known); err != nil {
			return err
		}
		if method.errorType != nil {
			if method.errorType.optional || method.errorType.element != nil || method.errorType.name == "" {
				return diagnostic(method.position, "method error must be a named constructor type")
			}
			if _, ok := groups[method.errorType.name]; !ok {
				return diagnostic(method.position, "method error %q is not a constructor type", method.errorType.name)
			}
			errorTypes[method.errorType.name] = method.position
		}
	}

	for _, constructor := range schema.constructors {
		if _, ok := errorTypes[constructor.result]; ok {
			for _, field := range constructor.fields {
				if names.Go(field.name) == "Error" {
					return diagnostic(field.position, "error constructor field %q conflicts with the generated Error method", field.name)
				}
			}
		} else if constructor.httpStatus != 0 {
			return diagnostic(constructor.position, "HTTP status is only valid on declared error constructors")
		}
		if constructor.httpStatus != 0 && (constructor.httpStatus < 400 || constructor.httpStatus > 599) {
			return diagnostic(constructor.position, "invalid error HTTP status %d", constructor.httpStatus)
		}
		for _, field := range constructor.fields {
			if name := referencedName(field.typ, errorTypes); name != "" {
				return diagnostic(field.position, "error type %q cannot be used as a field type", name)
			}
		}
	}
	for _, method := range schema.methods {
		for _, field := range method.fields {
			if name := referencedName(field.typ, errorTypes); name != "" {
				return diagnostic(field.position, "error type %q cannot be used as a request field", name)
			}
		}
		if name := referencedName(method.response, errorTypes); name != "" {
			return diagnostic(method.position, "error type %q cannot be used as a response", name)
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

func assignIDs(schema *Schema) error {
	ids := make(map[uint32]string, len(schema.constructors)+len(schema.methods))
	assign := func(canonical string, position Position) (uint32, error) {
		id := crc32.ChecksumIEEE([]byte(canonical))
		if previous, ok := ids[id]; ok {
			return 0, diagnostic(position, "CRC32 collision between %q and %q (0x%08x)", previous, canonical, id)
		}
		ids[id] = canonical
		return id, nil
	}

	for i := range schema.constructors {
		c := &schema.constructors[i]
		id, err := assign(constructorText(schema.packageName, *c, false), c.position)
		if err != nil {
			return err
		}
		c.id = id
	}
	for i := range schema.methods {
		m := &schema.methods[i]
		id, err := assign(methodText(*m, false), m.position)
		if err != nil {
			return err
		}
		m.id = id
	}
	return nil
}

func validateFields(fields []field) error {
	var (
		fieldNames = make(map[string]Position)
		goNames    = make(map[string]Position)
		optional   bool
	)
	for _, field := range fields {
		if err := checkName(field.name, field.position, "field"); err != nil {
			return err
		}
		if err := addUnique(fieldNames, field.name, field.position, "field"); err != nil {
			return err
		}
		if err := addUnique(goNames, names.Go(field.name), field.position, "generated Go field"); err != nil {
			return err
		}
		if field.typ.optional {
			optional = true
		} else if optional {
			return diagnostic(field.position, "required field %q follows an optional extension field", field.name)
		}
	}
	return nil
}

func validateTypeRef(ref typeRef, position Position, response bool, known func(string) bool) error {
	if ref.element != nil {
		if ref.element.optional {
			return diagnostic(position, "list elements cannot be optional; wrap the list instead")
		}
		return validateTypeRef(*ref.element, position, false, known)
	}
	if !known(ref.name) {
		return diagnostic(position, "unknown type %q", ref.name)
	}
	if (ref.name == "void" || ref.name == "raw") && (!response || ref.optional) {
		return diagnostic(position, "%s is only valid as a non-optional method response", ref.name)
	}
	return nil
}

func rejectRecursiveTypes(groups map[string][]constructor) error {
	edges := make(map[string][]string)
	for result, constructors := range groups {
		for _, constructor := range constructors {
			for _, field := range constructor.fields {
				if name := namedType(field.typ); name != "" && hasKey(groups, name) {
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
	state := make(map[string]uint8)
	var (
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

func addUnique(values map[string]Position, name string, position Position, kind string) error {
	if previous, ok := values[name]; ok {
		return diagnostic(position, "duplicate %s %q; first declared at %s", kind, name, previous)
	}
	values[name] = position
	return nil
}

func checkName(name string, position Position, kind string) error {
	if !validIdentifier(name) {
		return diagnostic(position, "invalid %s name %q", kind, name)
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
		return diagnostic(position, "%s name %q is reserved", kind, name)
	}
	return nil
}

func checkQualifiedName(name string, position Position, kind string) error {
	group, method, ok := strings.Cut(name, ".")
	if !ok || group == "" || method == "" || strings.Contains(method, ".") {
		return diagnostic(position, "%s name %q must have the form group.name", kind, name)
	}
	if err := checkName(group, position, kind+" group"); err != nil {
		return err
	}
	return checkName(method, position, kind)
}

func validateHTTPBinding(binding httpBinding, fields []field) error {
	if !slices.Contains([]string{"DELETE", "GET", "PATCH", "POST", "PUT"}, binding.verb) {
		return diagnostic(binding.position, "unsupported HTTP verb %q", binding.verb)
	}
	parameters, err := pathParameters(binding.path)
	if err != nil {
		return diagnostic(binding.position, "invalid HTTP path %q: %v", binding.path, err)
	}
	fieldNames := make(map[string]bool, len(fields))
	for _, field := range fields {
		fieldNames[field.name] = true
	}
	for _, parameter := range parameters {
		if !fieldNames[parameter] {
			return diagnostic(binding.position, "HTTP path parameter %q is not a request field", parameter)
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
		return nil, fmt.Errorf("path must start with '/'")
	}
	if strings.ContainsAny(path, "?#") {
		return nil, fmt.Errorf("query strings and fragments are not allowed")
	}
	var (
		result []string
		seen   = make(map[string]bool)
	)
	for offset := 0; offset < len(path); {
		start := strings.IndexByte(path[offset:], '{')
		if start < 0 {
			if strings.ContainsRune(path[offset:], '}') {
				return nil, fmt.Errorf("unmatched '}'")
			}
			break
		}
		start += offset
		if strings.ContainsRune(path[offset:start], '}') {
			return nil, fmt.Errorf("unmatched '}'")
		}
		end := strings.IndexByte(path[start+1:], '}')
		if end < 0 {
			return nil, fmt.Errorf("unmatched '{'")
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
