// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"slices"
	"strings"
)

// schemaView orders declarations for metadata and canonical source output.
type schemaView struct {
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

func newSchemaView(s *Schema) schemaView {
	v := schemaView{
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

func errorTypeNames(s *Schema) map[string]bool {
	names := make(map[string]bool)
	for _, m := range s.methods {
		if m.errorType != nil {
			names[m.errorType.name] = true
		}
	}
	return names
}

func schemaHasHTTP(s *Schema) bool {
	for _, m := range s.methods {
		if len(m.http) > 0 {
			return true
		}
	}
	return false
}

func sortedGroupNames(groups map[string][]constructor) []string {
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
