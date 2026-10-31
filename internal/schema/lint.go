// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"slices"
	"strings"
)

// Lint performs static analysis on a validated [Schema] and returns diagnostic messages.
func Lint(schema *Schema) []error {
	if schema == nil {
		return nil
	}

	var issues []error

	// Check for unused permissions.
	usedPermissions := make(map[string]bool)
	for _, method := range schema.methods {
		if method.permission != "" {
			usedPermissions[method.permission] = true
		}
	}
	for _, permission := range schema.permissions {
		if !usedPermissions[permission.name] {
			issues = append(issues, diagnostic(permission.position, "permission %q is declared but never required by any method", permission.name))
		}
	}

	// Analyze reachability for types.
	knownConstructors := groupConstructors(schema)
	reachableTypes := make(map[string]bool)
	var visitTypeRef func(ref typeRef)
	visitTypeRef = func(ref typeRef) {
		if ref.element != nil {
			visitTypeRef(*ref.element)
			return
		}
		if isPrimitive(ref.name) || reachableTypes[ref.name] {
			return
		}
		reachableTypes[ref.name] = true

		for _, constructor := range knownConstructors[ref.name] {
			for _, field := range constructor.fields {
				visitTypeRef(field.typ)
			}
		}
	}

	for _, method := range schema.methods {
		for _, field := range method.fields {
			visitTypeRef(field.typ)
		}
		visitTypeRef(method.response)
		if method.errorType != nil {
			visitTypeRef(*method.errorType)
		}
	}

	// Check for unused newtypes.
	for _, newtype := range schema.newtypes {
		if !reachableTypes[newtype.name] {
			issues = append(issues, diagnostic(newtype.position, "newtype %q is declared but never referenced", newtype.name))
		}
	}

	// Check for unused constructors or record types.
	visitedConstructorResults := make(map[string]bool)
	for _, constructor := range schema.constructors {
		if visitedConstructorResults[constructor.result] {
			continue
		}
		visitedConstructorResults[constructor.result] = true
		if !reachableTypes[constructor.result] {
			issues = append(issues, diagnostic(constructor.position, "type %q is declared but never referenced by any method", constructor.result))
		}
	}

	// Check documentation coverage.
	lintDocumentation(&issues, schema.position, "package", schema.packageName, schema.documentation)
	for _, category := range schema.categories {
		lintDocumentation(&issues, category.position, "category", category.name, category.documentation)
	}
	for _, method := range schema.methods {
		lintDocumentation(&issues, method.position, "method", method.name, method.documentation)
		for _, field := range method.fields {
			lintDocumentation(&issues, field.position, "field", method.name+"."+field.name, field.documentation)
		}
	}
	for _, permission := range schema.permissions {
		lintDocumentation(&issues, permission.position, "permission", permission.name, permission.documentation)
	}
	for _, newtype := range schema.newtypes {
		lintDocumentation(&issues, newtype.position, "newtype", newtype.name, newtype.documentation)
	}
	for _, dt := range schema.types {
		lintDocumentation(&issues, dt.position, "type annotation", dt.name, dt.documentation)
	}
	for _, constructor := range schema.constructors {
		lintDocumentation(&issues, constructor.position, "constructor", constructor.name, constructor.documentation)
		for _, field := range constructor.fields {
			lintDocumentation(&issues, field.position, "field", constructor.name+"."+field.name, field.documentation)
		}
	}

	slices.SortFunc(issues, func(a, b error) int {
		return strings.Compare(a.Error(), b.Error())
	})
	return issues
}

func lintDocumentation(issues *[]error, position Position, kind, name, doc string) {
	if strings.TrimSpace(doc) == "" {
		*issues = append(*issues, diagnostic(position, "%s %q is missing a documentation comment", kind, name))
	}
}
