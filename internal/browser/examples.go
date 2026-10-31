// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package browser

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"go.astrophena.name/plug"
)

type httpExample struct {
	Binding               plug.HTTPBindingInfo
	URL                   string
	Body                  string
	Curl                  string
	Fields                []httpField
	JSON                  string
	Void                  bool
	OptionalFieldsOmitted bool
}

type httpField struct {
	Info           plug.FieldInfo
	Location       string
	Representation string
}

type jsonField struct {
	Name  string
	Value any
}

type jsonObject []jsonField

func (o jsonObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, field := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		name, err := json.Marshal(field.Name)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(field.Value)
		if err != nil {
			return nil, err
		}
		buf.Write(name)
		buf.WriteByte(':')
		buf.Write(encoded)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func (b *browser) httpExamples(method plug.MethodInfo, origin string) []httpExample {
	result := make([]httpExample, 0, len(method.HTTP))
	for _, binding := range method.HTTP {
		exampleURL, pathFields := b.exampleURL(binding, method.Fields)
		body := ""
		if binding.Verb != http.MethodGet && binding.Verb != http.MethodDelete {
			fields := make([]plug.FieldInfo, 0, len(method.Fields))
			for _, field := range method.Fields {
				if !pathFields[field.Name] {
					fields = append(fields, field)
				}
			}
			body = b.exampleFieldsJSON(fields)
		}
		result = append(result, httpExample{
			Binding: binding,
			URL:     exampleURL,
			Body:    body,
			Curl:    curlExample(binding.Verb, origin+exampleURL, body),
			Fields:  b.httpFields(binding, method.Fields, pathFields),
			JSON:    b.exampleResponseJSON(method.Response),
			Void:    method.Response.Kind == plug.TypeExprPrimitive && method.Response.Name == "void",
			OptionalFieldsOmitted: slices.ContainsFunc(method.Fields, func(field plug.FieldInfo) bool {
				return field.Type.Optional && !pathFields[field.Name]
			}),
		})
	}
	return result
}

func (b *browser) httpFields(binding plug.HTTPBindingInfo, fields []plug.FieldInfo, pathFields map[string]bool) []httpField {
	result := make([]httpField, 0, len(fields))
	for _, field := range fields {
		item := httpField{
			Info: field,
		}
		switch {
		case pathFields[field.Name]:
			item.Location = "Path"
			item.Representation = "URL-encoded scalar"
			if strings.Contains(binding.Path, "{"+field.Name+"...}") {
				item.Representation = "URL-encoded path segments"
			}
		case binding.Verb == http.MethodGet || binding.Verb == http.MethodDelete:
			item.Location = "Query"
			if field.Type.Kind == plug.TypeExprList {
				item.Representation = "Repeated query parameter"
			} else {
				item.Representation = "Single query parameter"
			}
		default:
			item.Location = "JSON body"
			item.Representation = "JSON value"
		}
		result = append(result, item)
	}
	return result
}

func (b *browser) exampleURL(binding plug.HTTPBindingInfo, fields []plug.FieldInfo) (string, map[string]bool) {
	exampleURL := binding.Path
	pathFields := make(map[string]bool)
	for _, field := range fields {
		placeholder := "{" + field.Name + "}"
		wildcard := strings.Contains(exampleURL, "{"+field.Name+"...}")
		if wildcard {
			placeholder = "{" + field.Name + "...}"
		}
		if !strings.Contains(exampleURL, placeholder) {
			continue
		}
		pathFields[field.Name] = true
		value := url.PathEscape(b.exampleText(field.Type))
		if wildcard {
			value = "directory/" + value
		}
		exampleURL = strings.ReplaceAll(exampleURL, placeholder, value)
	}
	if binding.Verb != http.MethodGet && binding.Verb != http.MethodDelete {
		return exampleURL, pathFields
	}
	query := make(url.Values)
	for _, field := range fields {
		if pathFields[field.Name] || field.Type.Optional {
			continue
		}
		query.Add(field.Name, b.exampleText(field.Type))
	}
	if encoded := query.Encode(); encoded != "" {
		exampleURL += "?" + encoded
	}
	return exampleURL, pathFields
}

func (b *browser) exampleText(expr plug.TypeExpr) string {
	if expr.Kind == plug.TypeExprList && expr.Element != nil {
		return b.exampleText(*expr.Element)
	}
	if typ, ok := b.typeByName[expr.Name]; ok && typ.Info.Kind == plug.TypeNewtype {
		expr.Name = typ.Info.Underlying
	}
	switch expr.Name {
	case "bool":
		return "true"
	case "bytes":
		return "AA=="
	case "f64":
		return "1.5"
	case "i32", "i64", "u32", "u64":
		return "1"
	default:
		return "example"
	}
}

func (b *browser) exampleFieldsJSON(fields []plug.FieldInfo) string {
	if len(fields) == 0 {
		return ""
	}
	value := make(jsonObject, 0, len(fields))
	for _, field := range fields {
		if field.Type.Optional {
			continue
		}
		value = append(value, jsonField{
			Name:  field.Name,
			Value: b.exampleValue(field.Type),
		})
	}
	if len(value) == 0 {
		return ""
	}
	return marshalExampleJSON(value)
}

func (b *browser) exampleTypeJSON(typ plug.TypeInfo) string {
	return marshalExampleJSON(b.exampleValue(plug.TypeExpr{
		Kind: plug.TypeExprNamed,
		Name: typ.Name,
	}))
}

func (b *browser) exampleResponseJSON(response plug.TypeExpr) string {
	if response.Kind == plug.TypeExprPrimitive && (response.Name == "void" || response.Name == "raw") {
		return ""
	}
	return marshalExampleJSON(b.exampleValue(response))
}

func (b *browser) exampleErrorJSON(typeName string) string {
	typ, ok := b.typeByName[typeName]
	if !ok || len(typ.Info.Constructors) == 0 {
		return ""
	}
	return marshalExampleJSON(jsonObject{
		{
			Name:  "code",
			Value: typ.Info.Constructors[0].Name,
		},
		{
			Name:  "message",
			Value: "application error",
		},
		{
			Name: "details",
			Value: b.exampleValue(plug.TypeExpr{
				Kind: plug.TypeExprNamed,
				Name: typeName,
			}),
		},
	})
}

func (b *browser) exampleValue(expr plug.TypeExpr) any {
	if expr.Kind == plug.TypeExprList {
		var element any = "example"
		if expr.Element != nil {
			element = b.exampleValue(*expr.Element)
		}
		return []any{element}
	}
	switch expr.Name {
	case "bool":
		return true
	case "bytes":
		return "AA=="
	case "f64":
		return 1.5
	case "i32", "i64", "u32", "u64":
		return 1
	case "string":
		return "example"
	case "void":
		return nil
	}

	typ, ok := b.typeByName[expr.Name]
	if !ok {
		return "example"
	}
	if typ.Info.Kind == plug.TypeNewtype {
		return b.exampleValue(plug.TypeExpr{
			Kind: plug.TypeExprPrimitive,
			Name: typ.Info.Underlying,
		})
	}
	if len(typ.Info.Constructors) == 0 {
		return jsonObject{}
	}
	constructor := typ.Info.Constructors[0]
	value := make(jsonObject, 0, len(constructor.Fields)+1)
	if typ.Info.Kind == plug.TypeSum {
		value = append(value, jsonField{
			Name:  "type",
			Value: constructor.Name,
		})
	}
	for _, field := range constructor.Fields {
		if field.Type.Optional {
			continue
		}
		value = append(value, jsonField{
			Name:  field.Name,
			Value: b.exampleValue(field.Type),
		})
	}
	return value
}

func marshalExampleJSON(value any) string {
	result, err := json.Marshal(value, jsontext.WithIndent("  "))
	if err != nil {
		return ""
	}
	return string(result)
}

func curlExample(method, requestURL, body string) string {
	var sb strings.Builder
	sb.WriteString("curl --request ")
	sb.WriteString(method)
	if body != "" {
		sb.WriteString(" \\\n  --header 'Content-Type: application/json' \\\n  --data '")
		sb.WriteString(body)
		sb.WriteByte('\'')
	}
	sb.WriteString(" \\\n  ")
	sb.WriteString(shellQuote(requestURL))
	return sb.String()
}

func requestOrigin(r *http.Request) string {
	scheme := r.URL.Scheme
	if scheme == "" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	host := r.Host
	if host == "" {
		host = r.URL.Host
	}
	return (&url.URL{
		Scheme: scheme,
		Host:   host,
	}).String()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
