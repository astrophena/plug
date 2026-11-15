// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"slices"
	"strings"
)

// Lint reports unused declarations and missing documentation in a validated schema.
func Lint(s *Schema) []error {
	if s == nil {
		return nil
	}

	var (
		issues []error
		perms  = make(map[string]bool)
	)
	for _, m := range s.methods {
		if m.permission != "" {
			perms[m.permission] = true
		}
	}
	for _, p := range s.permissions {
		if !perms[p.name] {
			issues = append(issues, diagnostic(p.position, "permission %q is declared but never required by any method", p.name))
		}
	}

	var (
		groups = groupConstructors(s)
		used   = make(map[string]bool)
		visit  func(typeRef)
	)
	visit = func(ref typeRef) {
		if ref.element != nil {
			visit(*ref.element)
			return
		}
		if isPrimitive(ref.name) || used[ref.name] {
			return
		}
		used[ref.name] = true

		for _, c := range groups[ref.name] {
			for _, f := range c.fields {
				visit(f.typ)
			}
		}
	}

	for _, m := range s.methods {
		for _, f := range m.fields {
			visit(f.typ)
		}
		visit(m.response)
		if m.errorType != nil {
			visit(*m.errorType)
		}
	}

	for _, newtype := range s.newtypes {
		if !used[newtype.name] {
			issues = append(issues, diagnostic(newtype.position, "newtype %q is declared but never referenced", newtype.name))
		}
	}

	seen := make(map[string]bool)
	for _, c := range s.constructors {
		if seen[c.result] {
			continue
		}
		seen[c.result] = true
		if !used[c.result] {
			issues = append(issues, diagnostic(c.position, "type %q is declared but never referenced by any method", c.result))
		}
	}

	lintDoc(&issues, s.position, "package", s.packageName, s.documentation)
	for _, c := range s.categories {
		lintDoc(&issues, c.position, "category", c.name, c.documentation)
	}
	for _, m := range s.methods {
		lintDoc(&issues, m.position, "method", m.name, m.documentation)
		for _, f := range m.fields {
			lintDoc(&issues, f.position, "field", m.name+"."+f.name, f.documentation)
		}
	}
	for _, p := range s.permissions {
		lintDoc(&issues, p.position, "permission", p.name, p.documentation)
	}
	for _, newtype := range s.newtypes {
		lintDoc(&issues, newtype.position, "newtype", newtype.name, newtype.documentation)
	}
	for _, dt := range s.types {
		lintDoc(&issues, dt.position, "type annotation", dt.name, dt.documentation)
	}
	for _, c := range s.constructors {
		lintDoc(&issues, c.position, "constructor", c.name, c.documentation)
		for _, f := range c.fields {
			lintDoc(&issues, f.position, "field", c.name+"."+f.name, f.documentation)
		}
	}

	slices.SortFunc(issues, func(a, b error) int {
		return strings.Compare(a.Error(), b.Error())
	})
	return issues
}

func lintDoc(issues *[]error, pos Position, kind, name, doc string) {
	if strings.TrimSpace(doc) == "" {
		*issues = append(*issues, diagnostic(pos, "%s %q is missing a documentation comment", kind, name))
	}
}
