// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

func (v *view) format() string {
	var buf bytes.Buffer
	writeDoc(&buf, "", v.schema.documentation)
	fmt.Fprintf(&buf, "package %s version %d\n", v.schema.packageName, v.schema.major)

	for _, perm := range v.permissions {
		buf.WriteByte('\n')
		writeDoc(&buf, "", perm.documentation)
		fmt.Fprintf(&buf, "permission %s\n", perm.name)
	}

	var (
		typeCategories   = make(map[string]bool, len(v.newtypes)+len(v.groupNames))
		methodCategories = make(map[string]bool, len(v.methods))
	)
	for _, c := range v.categories {
		for _, name := range c.types {
			typeCategories[name] = true
		}
		for _, name := range c.methods {
			methodCategories[name] = true
		}
	}
	writeNewtype := func(name string) {
		value := v.newtypeByName[name]
		buf.WriteByte('\n')
		writeDoc(&buf, "", value.documentation)
		fmt.Fprintf(&buf, "type %s = %s\n", value.name, value.base)
	}
	writeGroup := func(name string) {
		if dt, ok := v.declaredTypes[name]; ok {
			buf.WriteByte('\n')
			writeDoc(&buf, "", dt.documentation)
			fmt.Fprintf(&buf, "type %s\n", dt.name)
		}
		for _, c := range v.groups[name] {
			buf.WriteByte('\n')
			writeDoc(&buf, "", c.documentation)
			tail := "= " + c.result
			if c.httpStatus != 0 {
				tail += fmt.Sprintf(" httpStatus %d", c.httpStatus)
			}
			writeDeclaration(&buf, c.name, c.fields, tail)
		}
	}
	writeMethod := func(m method) {
		buf.WriteByte('\n')
		writeDoc(&buf, "", m.documentation)
		writeMethodDeclaration(&buf, m)
	}
	for _, value := range v.newtypes {
		name := value.name
		if !typeCategories[name] {
			writeNewtype(name)
		}
	}
	for _, name := range v.groupNames {
		if !typeCategories[name] {
			writeGroup(name)
		}
	}
	for _, m := range v.methods {
		if !methodCategories[m.name] {
			writeMethod(m)
		}
	}
	for _, c := range v.categories {
		buf.WriteByte('\n')
		writeDoc(&buf, "", c.documentation)
		fmt.Fprintf(&buf, "category %s\n", strconv.Quote(c.name))
		for _, name := range c.types {
			if _, ok := v.newtypeByName[name]; ok {
				writeNewtype(name)
			} else {
				writeGroup(name)
			}
		}
		for _, name := range c.methods {
			writeMethod(v.methodByName[name])
		}
	}
	return buf.String()
}

func writeMethodDeclaration(buf *bytes.Buffer, m method) {
	var clauses []string
	if m.errorType != nil {
		clauses = append(clauses, "throws "+m.errorType.String())
	}
	if m.public {
		clauses = append(clauses, "public")
	} else {
		clauses = append(clauses, "requires "+m.permission)
	}
	for _, binding := range m.http {
		clauses = append(clauses, "http "+binding.verb+" "+strconv.Quote(binding.path))
	}
	tail := "-> " + m.response.String()
	if len(clauses) == 1 {
		tail += " " + clauses[0]
	}
	writeDeclaration(buf, m.name, m.fields, tail)
	if len(clauses) > 1 {
		for _, clause := range clauses {
			fmt.Fprintf(buf, "  %s\n", clause)
		}
	}
}

func sortedHTTP(bindings []httpBinding) []httpBinding {
	result := slices.Clone(bindings)
	slices.SortFunc(result, func(a, b httpBinding) int {
		if compared := strings.Compare(a.verb, b.verb); compared != 0 {
			return compared
		}
		return strings.Compare(a.path, b.path)
	})
	return result
}

func writeDeclaration(buf *bytes.Buffer, name string, fields []field, tail string) {
	hasFieldDocs := false
	for _, f := range fields {
		if f.documentation != "" {
			hasFieldDocs = true
			break
		}
	}
	if !hasFieldDocs {
		fmt.Fprintf(buf, "%s", name)
		for _, f := range fields {
			fmt.Fprintf(buf, " %s: %s", f.name, f.typ.String())
		}
		fmt.Fprintf(buf, " %s\n", tail)
		return
	}

	fmt.Fprintf(buf, "%s\n", name)
	for _, f := range fields {
		writeDoc(buf, "  ", f.documentation)
		fmt.Fprintf(buf, "  %s: %s\n", f.name, f.typ.String())
	}
	fmt.Fprintf(buf, "%s\n", tail)
}

func writeDoc(buf *bytes.Buffer, indent, doc string) {
	if doc == "" {
		return
	}
	for line := range strings.SplitSeq(doc, "\n") {
		if line == "" {
			fmt.Fprintf(buf, "%s//\n", indent)
			continue
		}
		if strings.HasPrefix(line, "\t") {
			fmt.Fprintf(buf, "%s//%s\n", indent, line)
			continue
		}
		fmt.Fprintf(buf, "%s// %s\n", indent, line)
	}
}

// view orders declarations for metadata and formatting.
type view struct {
	schema        *Schema
	permissions   []permission
	newtypes      []newtype
	newtypeByName map[string]newtype
	declaredTypes map[string]declaredType
	groups        map[string][]constructor
	groupNames    []string
	methods       []method
	methodByName  map[string]method
	categories    []category
}

func newView(s *Schema) view {
	v := view{
		schema:        s,
		permissions:   slices.Clone(s.permissions),
		newtypes:      slices.Clone(s.newtypes),
		newtypeByName: make(map[string]newtype, len(s.newtypes)),
		declaredTypes: make(map[string]declaredType, len(s.types)),
		groups:        groupConstructors(s),
		methods:       slices.Clone(s.methods),
		methodByName:  make(map[string]method, len(s.methods)),
		categories:    slices.Clone(s.categories),
	}
	slices.SortFunc(v.permissions, func(a, b permission) int { return strings.Compare(a.name, b.name) })
	slices.SortFunc(v.newtypes, func(a, b newtype) int { return strings.Compare(a.name, b.name) })
	slices.SortFunc(v.methods, func(a, b method) int { return strings.Compare(a.name, b.name) })
	v.groupNames = sortedGroupNames(v.groups)

	for _, value := range v.newtypes {
		v.newtypeByName[value.name] = value
	}
	for _, dt := range s.types {
		v.declaredTypes[dt.name] = dt
	}

	for i := range v.methods {
		m := &v.methods[i]
		m.http = sortedHTTP(m.http)
		v.methodByName[m.name] = *m
	}

	for i := range v.categories {
		c := &v.categories[i]
		c.types = slices.Clone(c.types)
		c.methods = slices.Clone(c.methods)
		slices.Sort(c.types)
		slices.Sort(c.methods)
	}
	return v
}

func groupConstructors(s *Schema) map[string][]constructor {
	groups := make(map[string][]constructor)
	for _, c := range s.constructors {
		groups[c.result] = append(groups[c.result], c)
	}

	for name := range groups {
		slices.SortFunc(groups[name], func(a, b constructor) int { return strings.Compare(a.name, b.name) })
	}
	return groups
}

func sortedGroupNames(groups map[string][]constructor) []string {
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
