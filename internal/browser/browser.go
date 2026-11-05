// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package browser renders Plug schema documentation and embeds its static assets.
package browser

//go:generate go tool templ fmt .
//go:generate go tool templ generate -include-version=false

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/internal/doc"
	"go.astrophena.name/plug/plugimpl"

	"github.com/a-h/templ"
)

// New returns a schema browser with HTML pages and an index.md download.
func New(schema *plug.Schema) (http.Handler, error) {
	if err := plugimpl.ValidateSchema(schema); err != nil {
		return nil, err
	}
	if err := validateCategories(schema); err != nil {
		return nil, err
	}

	b := &browser{
		schema:       schema,
		docs:         doc.NewParser(schema),
		methods:      make([]method, 0, len(schema.Methods)),
		methodByName: make(map[string]method, len(schema.Methods)),
		types:        make([]typeDoc, 0, len(schema.Types)),
		typeByName:   make(map[string]typeDoc, len(schema.Types)),
	}
	b.initMethods()
	b.initTypes()
	b.initCategories()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", b.serveIndex)
	mux.HandleFunc("GET /index.md", b.serveMarkdown)
	mux.HandleFunc("GET /methods/{method}", b.serveMethod)
	mux.HandleFunc("GET /types/{type}", b.serveType)
	mux.HandleFunc("GET /the-other-side-of-the-socket", b.serveProtocol)
	return mux, nil
}

func (b *browser) initMethods() {
	methods := slices.Clone(b.schema.Methods)
	slices.SortFunc(methods, func(a, b plug.MethodInfo) int {
		return strings.Compare(a.Name, b.Name)
	})

	for _, info := range methods {
		item := method{
			MethodInfo: info,
		}
		b.httpRoutes += len(info.HTTP)

		if info.Error != "" {
			for _, typ := range b.schema.Types {
				if typ.Name == info.Error {
					item.Errors = typ.Constructors
					break
				}
			}
		}

		b.methods = append(b.methods, item)
		b.methodByName[info.Name] = item
	}
}

func (b *browser) initTypes() {
	types := slices.Clone(b.schema.Types)
	slices.SortFunc(types, func(a, b plug.TypeInfo) int {
		return strings.Compare(a.Name, b.Name)
	})

	for _, typ := range types {
		item := typeDoc{
			TypeInfo: typ,
		}
		for _, m := range b.methods {
			info := m.MethodInfo
			if usesType(info.Fields, typ.Name) {
				item.Requests = append(item.Requests, info)
			}
			if typeReferences(info.Response, typ.Name) {
				item.Responses = append(item.Responses, info)
			}
			if info.Error == typ.Name {
				item.Failures = append(item.Failures, info)
			}
		}

		for _, owner := range types {
			if owner.Name == typ.Name {
				continue
			}
			for _, constructor := range owner.Constructors {
				if usesType(constructor.Fields, typ.Name) {
					item.Types = append(item.Types, owner)
					break
				}
			}
		}

		b.types = append(b.types, item)
		b.typeByName[typ.Name] = item
	}
}

func (b *browser) initCategories() {
	var (
		categorizedMethods = make(map[string]bool)
		categorizedTypes   = make(map[string]bool)
	)

	for _, info := range b.schema.Categories {
		item := category{
			Name:          info.Name,
			Documentation: info.Documentation,
		}
		for _, name := range info.Methods {
			item.Methods = append(item.Methods, b.methodByName[name])
			categorizedMethods[name] = true
		}
		for _, name := range info.Types {
			item.Types = append(item.Types, b.typeByName[name])
			categorizedTypes[name] = true
		}

		slices.SortFunc(item.Methods, func(a, b method) int {
			return strings.Compare(a.Name, b.Name)
		})
		slices.SortFunc(item.Types, func(a, b typeDoc) int {
			return strings.Compare(a.Name, b.Name)
		})
		if len(item.Methods) > 0 {
			b.methodCategories = append(b.methodCategories, item)
		}
		if len(item.Types) > 0 {
			b.typeCategories = append(b.typeCategories, item)
		}
	}

	if len(b.methodCategories) > 0 {
		for _, method := range b.methods {
			if !categorizedMethods[method.Name] {
				b.otherMethods = append(b.otherMethods, method)
			}
		}
	}

	if len(b.typeCategories) > 0 {
		for _, typ := range b.types {
			if !categorizedTypes[typ.Name] {
				b.otherTypes = append(b.otherTypes, typ)
			}
		}
	}
}

func validateCategories(schema *plug.Schema) error {
	typeNames := make(map[string]bool, len(schema.Types))
	for _, typ := range schema.Types {
		typeNames[typ.Name] = true
	}
	methodNames := make(map[string]bool, len(schema.Methods))
	for _, method := range schema.Methods {
		methodNames[method.Name] = true
	}
	categorizedTypes := make(map[string]string)
	categorizedMethods := make(map[string]string)
	for _, category := range schema.Categories {
		if category.Name == "" || category.Name != strings.TrimSpace(category.Name) {
			return fmt.Errorf("plughttp: schema contains an empty or padded category %q", category.Name)
		}
		if len(category.Types) == 0 && len(category.Methods) == 0 {
			return fmt.Errorf("plughttp: category %q contains no types or methods", category.Name)
		}
		for _, typ := range category.Types {
			if !typeNames[typ] {
				return fmt.Errorf("plughttp: category %q contains unknown type %q", category.Name, typ)
			}
			if previous, ok := categorizedTypes[typ]; ok {
				return fmt.Errorf("plughttp: type %q appears in categories %q and %q", typ, previous, category.Name)
			}
			categorizedTypes[typ] = category.Name
		}
		for _, method := range category.Methods {
			if !methodNames[method] {
				return fmt.Errorf("plughttp: category %q contains unknown method %q", category.Name, method)
			}
			if previous, ok := categorizedMethods[method]; ok {
				return fmt.Errorf("plughttp: method %q appears in categories %q and %q", method, previous, category.Name)
			}
			categorizedMethods[method] = category.Name
		}
	}
	return nil
}

type browser struct {
	schema     *plug.Schema
	docs       *doc.Parser
	httpRoutes int

	methods          []method
	methodByName     map[string]method
	methodCategories []category
	otherMethods     []method

	types          []typeDoc
	typeByName     map[string]typeDoc
	typeCategories []category
	otherTypes     []typeDoc
}

type method struct {
	plug.MethodInfo
	Errors []plug.ConstructorInfo
}

type category struct {
	Name          string
	Documentation string
	Methods       []method
	Types         []typeDoc
}

type typeDoc struct {
	plug.TypeInfo
	Requests  []plug.MethodInfo
	Responses []plug.MethodInfo
	Failures  []plug.MethodInfo
	Types     []plug.TypeInfo
}

type page struct {
	browser *browser
	origin  string
	root    string
	title   string
}

func (b *browser) serviceName() string {
	return strings.ToUpper(b.schema.Package[:1]) + b.schema.Package[1:]
}

func (p page) homeURL() string {
	if p.root == "" {
		return "./"
	}
	return p.root
}

func (p page) methodsURL() string { return p.homeURL() + "#methods" }

func (p page) typesURL() string { return p.homeURL() + "#types" }

func (p page) httpURL() string { return p.homeURL() + "#http" }

func (p page) schemaURL() string { return p.homeURL() + "#schema" }

func (p page) protocolURL() string { return p.homeURL() + "the-other-side-of-the-socket" }

func (p page) markdownURL() string { return p.homeURL() + "index.md" }

func (p page) methodURL(name string) string {
	return p.root + "methods/" + url.PathEscape(name)
}

func (p page) typeURL(name string) string {
	return p.root + "types/" + url.PathEscape(name)
}

func (p page) tokens(source string) []token {
	var result []token
	for offset := 0; offset < len(source); {
		if strings.HasPrefix(source[offset:], "//") {
			result = append(result, token{
				Text:  source[offset:],
				Class: "comment",
			})
			break
		}
		if source[offset] == '"' {
			end := offset + 1
			for end < len(source) {
				if source[end] == '\\' && end+1 < len(source) {
					end += 2
					continue
				}
				end++
				if source[end-1] == '"' {
					break
				}
			}
			result = append(result, token{
				Text:  source[offset:end],
				Class: "string",
			})
			offset = end
			continue
		}
		if isIdentStart(source[offset]) {
			end := offset + 1
			for end < len(source) && isIdentContinue(source[end]) {
				end++
			}
			word := source[offset:end]
			token := token{
				Text: word,
			}
			switch {
			case p.browser.hasMethod(word):
				token.Class = "method"
				token.Href = p.methodURL(word)
			case p.browser.hasType(word):
				token.Class = "type"
				token.Href = p.typeURL(word)
			case keywords[word]:
				token.Class = "keyword"
			case primitives[word]:
				token.Class = "primitive"
			}
			result = append(result, token)
			offset = end
			continue
		}
		if source[offset] >= '0' && source[offset] <= '9' {
			end := offset + 1
			for end < len(source) && source[end] >= '0' && source[end] <= '9' {
				end++
			}
			result = append(result, token{
				Text:  source[offset:end],
				Class: "number",
			})
			offset = end
			continue
		}
		end := offset + 1
		for end < len(source) && !strings.HasPrefix(source[end:], "//") && source[end] != '"' && !isIdentStart(source[end]) && !(source[end] >= '0' && source[end] <= '9') {
			end++
		}
		result = append(result, token{
			Text: source[offset:end],
		})
		offset = end
	}
	return result
}

type token struct {
	Text  string
	Class string
	Href  string
}

func (b *browser) serveIndex(w http.ResponseWriter, r *http.Request) {
	b.render(w, r, indexPage(page{
		browser: b,
		title:   b.serviceName() + " API",
	}))
}

func (b *browser) serveMethod(w http.ResponseWriter, r *http.Request) {
	m, ok := b.methodByName[r.PathValue("method")]
	if !ok {
		http.NotFound(w, r)
		return
	}

	b.render(w, r, methodPage(page{
		browser: b,
		origin:  requestOrigin(r),
		root:    "../",
		title:   m.Name,
	}, m))
}

func (b *browser) serveType(w http.ResponseWriter, r *http.Request) {
	typ, ok := b.typeByName[r.PathValue("type")]
	if !ok {
		http.NotFound(w, r)
		return
	}

	b.render(w, r, typePage(page{
		browser: b,
		root:    "../",
		title:   typ.Name,
	}, typ))
}

func (b *browser) serveProtocol(w http.ResponseWriter, r *http.Request) {
	b.render(w, r, protocolPage(page{
		browser: b,
		root:    "./",
		title:   "Plug protocol",
	}))
}

func (b *browser) render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	var buf bytes.Buffer
	if err := c.Render(r.Context(), &buf); err != nil {
		http.Error(w, fmt.Sprintf("plughttp: render schema browser: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	w.Write(buf.Bytes())
}

func (b *browser) hasMethod(name string) bool {
	_, ok := b.methodByName[name]
	return ok
}

func (b *browser) hasType(name string) bool {
	_, ok := b.typeByName[name]
	return ok
}

func fingerprint(value [32]byte) string { return fmt.Sprintf("%x", value) }

type declaration struct {
	Source        string
	Documentation string
	ID            uint32
	HTTPStatus    int
	Fields        []plug.FieldInfo
}

func typeDeclarations(typ plug.TypeInfo) []declaration {
	if typ.Kind == plug.TypeNewtype {
		return []declaration{
			{
				Source: "type " + typ.Name + " = " + typ.Underlying,
			},
		}
	}
	result := make([]declaration, 0, len(typ.Constructors))
	for _, constructor := range typ.Constructors {
		var source strings.Builder
		source.WriteString(constructor.Name)
		for _, field := range constructor.Fields {
			source.WriteString("\n  ")
			source.WriteString(field.Name)
			source.WriteString(": ")
			source.WriteString(typeSyntax(field.Type))
		}
		source.WriteString("\n= ")
		source.WriteString(typ.Name)
		if constructor.HTTPStatus != 0 {
			source.WriteString(" httpStatus ")
			source.WriteString(strconv.Itoa(constructor.HTTPStatus))
		}
		result = append(result, declaration{
			Source:        source.String(),
			Documentation: constructor.Documentation,
			ID:            constructor.ID,
			HTTPStatus:    constructor.HTTPStatus,
			Fields:        constructor.Fields,
		})
	}
	return result
}

func methodDeclaration(method plug.MethodInfo) string {
	var sb strings.Builder
	sb.WriteString(method.Name)
	for _, field := range method.Fields {
		sb.WriteString("\n  ")
		sb.WriteString(field.Name)
		sb.WriteString(": ")
		sb.WriteString(typeSyntax(field.Type))
	}
	sb.WriteString("\n-> ")
	sb.WriteString(typeSyntax(method.Response))
	if method.Error != "" {
		sb.WriteString("\n  throws ")
		sb.WriteString(method.Error)
	}
	if method.Public {
		sb.WriteString("\n  public")
	} else {
		sb.WriteString("\n  requires ")
		sb.WriteString(method.Permission)
	}
	for _, binding := range method.HTTP {
		sb.WriteString("\n  http ")
		sb.WriteString(binding.Verb)
		sb.WriteByte(' ')
		sb.WriteString(strconv.Quote(binding.Path))
	}
	return sb.String()
}

func typeKind(typ plug.TypeInfo) string {
	if typ.Error {
		return "error"
	}
	switch typ.Kind {
	case plug.TypeNewtype:
		return "newtype"
	case plug.TypeRecord:
		return "record"
	case plug.TypeSum:
		return "sum type"
	default:
		return "type"
	}
}

func typeSyntax(expr plug.TypeExpr) string {
	var sb strings.Builder
	if expr.Kind == plug.TypeExprList {
		sb.WriteString("list<")
		if expr.Element != nil {
			sb.WriteString(typeSyntax(*expr.Element))
		}
		sb.WriteByte('>')
	} else {
		sb.WriteString(expr.Name)
	}
	if expr.Optional {
		sb.WriteByte('?')
	}
	return sb.String()
}

func hasDocs(fields []plug.FieldInfo) bool {
	return slices.ContainsFunc(fields, func(field plug.FieldInfo) bool {
		return field.Documentation != ""
	})
}

func usesType(fields []plug.FieldInfo, name string) bool {
	return slices.ContainsFunc(fields, func(field plug.FieldInfo) bool {
		return typeReferences(field.Type, name)
	})
}

func isIdentStart(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isIdentContinue(value byte) bool {
	return isIdentStart(value) || value >= '0' && value <= '9' || value == '.'
}

func hexID(value uint32) string { return fmt.Sprintf("0x%08x", value) }

var keywords = map[string]bool{
	"category":   true,
	"DELETE":     true,
	"GET":        true,
	"PATCH":      true,
	"POST":       true,
	"PUT":        true,
	"http":       true,
	"httpStatus": true,
	"package":    true,
	"permission": true,
	"public":     true,
	"requires":   true,
	"throws":     true,
	"type":       true,
	"version":    true,
}

var primitives = map[string]bool{
	"bool":   true,
	"bytes":  true,
	"f64":    true,
	"i32":    true,
	"i64":    true,
	"list":   true,
	"string": true,
	"u32":    true,
	"u64":    true,
	"void":   true,
}

// typeReferences reports whether typ contains a reference to name.
func typeReferences(typ plug.TypeExpr, name string) bool {
	if typ.Kind == plug.TypeExprList && typ.Element != nil {
		return typeReferences(*typ.Element, name)
	}
	return typ.Kind == plug.TypeExprNamed && typ.Name == name
}
