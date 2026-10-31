// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"go.astrophena.name/plug"
	"go.astrophena.name/plug/plugimpl"
)

// RuntimeSchema returns independent, ordered metadata for a schema returned
// by [Parse]. Types contain newtypes first, then records and sums, each group
// ordered by name. Methods, constructors, and HTTP bindings are also ordered.
func RuntimeSchema(parsed *Schema) plug.Schema {
	view := newSchemaView(parsed)
	result := plug.Schema{
		Package:       parsed.packageName,
		Major:         parsed.major,
		Family:        plugimpl.FamilyFingerprint(parsed.packageName, parsed.major),
		Documentation: parsed.documentation,
		Fingerprint:   fingerprint(parsed),
		Source:        view.format(),
	}

	for _, permission := range view.permissions {
		result.Permissions = append(result.Permissions, plug.PermissionInfo{
			Name:          permission.name,
			Documentation: permission.documentation,
		})
	}

	for _, category := range view.categories {
		result.Categories = append(result.Categories, plug.CategoryInfo{
			Name:          category.name,
			Documentation: category.documentation,
			Types:         category.types,
			Methods:       category.methods,
		})
	}

	for _, value := range view.newtypes {
		result.Types = append(result.Types, plug.TypeInfo{
			Name:          value.name,
			Documentation: value.documentation,
			Kind:          plug.TypeNewtype,
			Underlying:    value.base,
		})
	}

	errorTypes := errorTypeNames(parsed)
	for _, name := range view.groupNames {
		constructors := view.groups[name]
		kind := plug.TypeRecord
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
		if dt := view.declaredTypes[name]; dt.documentation != "" {
			typ.Documentation = dt.documentation
		}
		for _, constructor := range constructors {
			typ.Constructors = append(typ.Constructors, plug.ConstructorInfo{
				Name:          constructor.name,
				Documentation: constructor.documentation,
				ID:            constructor.id,
				Fields:        runtimeFields(constructor.fields),
				HTTPStatus:    constructor.httpStatus,
			})
		}
		result.Types = append(result.Types, typ)
	}

	for _, method := range view.methods {
		info := plug.MethodInfo{
			Name:          method.name,
			Documentation: method.documentation,
			ID:            method.id,
			Fields:        runtimeFields(method.fields),
			Response:      runtimeTypeExpr(method.response),
			Public:        method.public,
			Permission:    method.permission,
		}
		if method.errorType != nil {
			info.Error = method.errorType.name
		}
		for _, binding := range method.http {
			info.HTTP = append(info.HTTP, plug.HTTPBindingInfo{
				Verb: binding.verb,
				Path: binding.path,
			})
		}
		result.Methods = append(result.Methods, info)
	}
	return result
}

func runtimeFields(fields []field) []plug.FieldInfo {
	if len(fields) == 0 {
		return nil
	}
	result := make([]plug.FieldInfo, 0, len(fields))
	for _, field := range fields {
		result = append(result, plug.FieldInfo{
			Name:          field.name,
			Documentation: field.documentation,
			Type:          runtimeTypeExpr(field.typ),
		})
	}
	return result
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
