// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"go.astrophena.name/plug"
	"go.astrophena.name/plug/plugimpl"
)

// RuntimeSchema returns an independent metadata snapshot of s.
// Types list newtypes first, then records and sums, sorted by name within each group.
// Methods and constructors are sorted by name; HTTP bindings by verb and path.
func RuntimeSchema(s *Schema) plug.Schema {
	var (
		v   = newView(s)
		out = plug.Schema{
			Package:       s.packageName,
			Major:         s.major,
			Family:        plugimpl.FamilyFingerprint(s.packageName, s.major),
			Documentation: s.documentation,
			Fingerprint:   fingerprint(s),
			Source:        v.format(),
		}
	)

	for _, p := range v.permissions {
		out.Permissions = append(out.Permissions, plug.PermissionInfo{
			Name:          p.name,
			Documentation: p.documentation,
		})
	}

	for _, c := range v.categories {
		out.Categories = append(out.Categories, plug.CategoryInfo{
			Name:          c.name,
			Documentation: c.documentation,
			Types:         c.types,
			Methods:       c.methods,
		})
	}

	for _, value := range v.newtypes {
		out.Types = append(out.Types, plug.TypeInfo{
			Name:          value.name,
			Documentation: value.documentation,
			Kind:          plug.TypeNewtype,
			Underlying:    value.base,
		})
	}

	errorTypes := errorTypeNames(s)
	for _, name := range v.groupNames {
		var (
			constructors = v.groups[name]
			kind         = plug.TypeRecord
		)
		if len(constructors) > 1 {
			kind = plug.TypeSum
		}
		typ := plug.TypeInfo{
			Name:  name,
			Kind:  kind,
			Error: errorTypes[name],
		}
		if len(constructors) == 1 {
			typ.Documentation = constructors[0].documentation
		}
		if dt := v.declaredTypes[name]; dt.documentation != "" {
			typ.Documentation = dt.documentation
		}
		for _, c := range constructors {
			typ.Constructors = append(typ.Constructors, plug.ConstructorInfo{
				Name:          c.name,
				Documentation: c.documentation,
				ID:            c.id,
				Fields:        runtimeFields(c.fields),
				HTTPStatus:    c.httpStatus,
			})
		}
		out.Types = append(out.Types, typ)
	}

	for _, m := range v.methods {
		info := plug.MethodInfo{
			Name:          m.name,
			Documentation: m.documentation,
			ID:            m.id,
			Fields:        runtimeFields(m.fields),
			Response:      runtimeTypeExpr(m.response),
			Public:        m.public,
			Permission:    m.permission,
		}
		if m.errorType != nil {
			info.Error = m.errorType.name
		}
		for _, binding := range m.http {
			info.HTTP = append(info.HTTP, plug.HTTPBindingInfo{
				Verb: binding.verb,
				Path: binding.path,
			})
		}
		out.Methods = append(out.Methods, info)
	}
	return out
}

func runtimeFields(fields []field) []plug.FieldInfo {
	if len(fields) == 0 {
		return nil
	}
	out := make([]plug.FieldInfo, 0, len(fields))
	for _, f := range fields {
		out = append(out, plug.FieldInfo{
			Name:          f.name,
			Documentation: f.documentation,
			Type:          runtimeTypeExpr(f.typ),
		})
	}
	return out
}

func runtimeTypeExpr(ref typeRef) plug.TypeExpr {
	if ref.element != nil {
		elem := runtimeTypeExpr(*ref.element)
		return plug.TypeExpr{
			Kind:     plug.TypeExprList,
			Element:  &elem,
			Optional: ref.optional,
		}
	}
	kind := plug.TypeExprNamed
	if isPrimitive(ref.name) {
		kind = plug.TypeExprPrimitive
	}
	return plug.TypeExpr{
		Kind:     kind,
		Name:     ref.name,
		Optional: ref.optional,
	}
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
