// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"

	"go.astrophena.name/plug"
)

// JSON returns a deterministic JSON export of s. See the [schema export format].
//
// [schema export format]: https://github.com/astrophena/plug/tree/master/docs/schema-json.md
func JSON(s plug.Schema) ([]byte, error) {
	document := jsonSchema{
		FormatVersion:   1,
		ProtocolVersion: 1,
		Package:         s.Package,
		Major:           s.Major,
		Family:          hex.EncodeToString(s.Family[:]),
		Fingerprint:     hex.EncodeToString(s.Fingerprint[:]),
		Documentation:   s.Documentation,
		Permissions:     make([]jsonPermission, len(s.Permissions)),
		Types:           make([]jsonType, len(s.Types)),
		Categories:      make([]jsonCategory, len(s.Categories)),
		Methods:         make([]jsonMethod, len(s.Methods)),
	}
	for i, permission := range s.Permissions {
		document.Permissions[i] = jsonPermission{
			Name:          permission.Name,
			Documentation: permission.Documentation,
		}
	}

	types := slices.Clone(s.Types)
	slices.SortFunc(types, func(a, b plug.TypeInfo) int {
		return strings.Compare(a.Name, b.Name)
	})
	for i, typ := range types {
		var kind string
		switch typ.Kind {
		case plug.TypeNewtype:
			kind = "newtype"
		case plug.TypeRecord:
			kind = "record"
		case plug.TypeSum:
			kind = "sum"
		default:
			return nil, fmt.Errorf("unknown schema type kind %d", typ.Kind)
		}
		document.Types[i] = jsonType{
			Name:          typ.Name,
			Documentation: typ.Documentation,
			Kind:          kind,
			Underlying:    typ.Underlying,
			Error:         typ.Error,
			Constructors:  make([]jsonConstructor, len(typ.Constructors)),
		}
		for j, constructor := range typ.Constructors {
			document.Types[i].Constructors[j] = jsonConstructor{
				Name:          constructor.Name,
				Documentation: constructor.Documentation,
				ID:            constructor.ID,
				Fields:        jsonFields(constructor.Fields),
				HTTPStatus:    constructor.HTTPStatus,
			}
		}
	}

	for i, category := range s.Categories {
		document.Categories[i] = jsonCategory{
			Name:          category.Name,
			Documentation: category.Documentation,
			Types:         category.Types,
			Methods:       category.Methods,
		}
	}
	for i, method := range s.Methods {
		document.Methods[i] = jsonMethod{
			Name:          method.Name,
			Documentation: method.Documentation,
			ID:            method.ID,
			Fields:        jsonFields(method.Fields),
			Response:      jsonTypeExpression(method.Response),
			Error:         method.Error,
			Public:        method.Public,
			Permission:    method.Permission,
			HTTP:          make([]jsonHTTPBinding, len(method.HTTP)),
		}
		for j, binding := range method.HTTP {
			document.Methods[i].HTTP[j] = jsonHTTPBinding{
				Verb: binding.Verb,
				Path: binding.Path,
			}
		}
	}

	data, err := json.Marshal(document, jsontext.WithIndent("  "))
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

type jsonSchema struct {
	FormatVersion   int              `json:"format_version"`
	ProtocolVersion int              `json:"protocol_version"`
	Package         string           `json:"package"`
	Major           uint16           `json:"major"`
	Family          string           `json:"family"`
	Fingerprint     string           `json:"fingerprint"`
	Documentation   string           `json:"documentation"`
	Permissions     []jsonPermission `json:"permissions"`
	Types           []jsonType       `json:"types"`
	Categories      []jsonCategory   `json:"categories"`
	Methods         []jsonMethod     `json:"methods"`
}

type jsonPermission struct {
	Name          string `json:"name"`
	Documentation string `json:"documentation"`
}

type jsonType struct {
	Name          string            `json:"name"`
	Documentation string            `json:"documentation"`
	Kind          string            `json:"kind"`
	Underlying    string            `json:"underlying"`
	Error         bool              `json:"error"`
	Constructors  []jsonConstructor `json:"constructors"`
}

type jsonConstructor struct {
	Name          string      `json:"name"`
	Documentation string      `json:"documentation"`
	ID            uint32      `json:"id"`
	Fields        []jsonField `json:"fields"`
	HTTPStatus    int         `json:"http_status"`
}

type jsonField struct {
	Name          string       `json:"name"`
	Documentation string       `json:"documentation"`
	Type          jsonTypeExpr `json:"type"`
}

type jsonTypeExpr struct {
	Kind     string        `json:"kind"`
	Name     string        `json:"name,omitempty"`
	Element  *jsonTypeExpr `json:"element,omitempty"`
	Optional bool          `json:"optional"`
}

type jsonMethod struct {
	Name          string            `json:"name"`
	Documentation string            `json:"documentation"`
	ID            uint32            `json:"id"`
	Fields        []jsonField       `json:"fields"`
	Response      jsonTypeExpr      `json:"response"`
	Error         string            `json:"error"`
	Public        bool              `json:"public"`
	Permission    string            `json:"permission"`
	HTTP          []jsonHTTPBinding `json:"http"`
}

type jsonHTTPBinding struct {
	Verb string `json:"verb"`
	Path string `json:"path"`
}

type jsonCategory struct {
	Name          string   `json:"name"`
	Documentation string   `json:"documentation"`
	Types         []string `json:"types"`
	Methods       []string `json:"methods"`
}

func jsonFields(fields []plug.FieldInfo) []jsonField {
	result := make([]jsonField, len(fields))
	for i, field := range fields {
		result[i] = jsonField{
			Name:          field.Name,
			Documentation: field.Documentation,
			Type:          jsonTypeExpression(field.Type),
		}
	}
	return result
}

func jsonTypeExpression(expr plug.TypeExpr) jsonTypeExpr {
	result := jsonTypeExpr{
		Kind:     expr.Kind.String(),
		Name:     expr.Name,
		Optional: expr.Optional,
	}
	if expr.Element != nil {
		element := jsonTypeExpression(*expr.Element)
		result.Element = &element
	}
	return result
}
