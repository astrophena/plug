// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"slices"
	"strconv"
	"strings"

	"go.astrophena.name/plug/internal/doc"
)

// Parse parses and validates a Plug schema file.
func Parse(filename string, source []byte) (*Schema, error) {
	p := &parser{
		lexer: newLexer(filename, string(source)),
	}
	parsed, err := p.parse()
	if err != nil {
		return nil, err
	}
	if err := validate(parsed); err != nil {
		return nil, err
	}
	if err := assignIDs(parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

type parser struct {
	lexer *lexer
	peek  *tokenValue
}

func (p *parser) parse() (*Schema, error) {
	documentation, err := p.documentation()
	if err != nil {
		return nil, err
	}
	keyword, err := p.expect(tokenIdentifier, "package")
	if err != nil {
		return nil, err
	}
	if keyword.text != "package" {
		return nil, p.unexpected(keyword, "package declaration")
	}

	packageName, err := p.expect(tokenIdentifier, "package name")
	if err != nil {
		return nil, err
	}
	major := uint16(1)
	next, err := p.look()
	if err != nil {
		return nil, err
	}
	if next.kind == tokenIdentifier && next.text == "version" {
		p.take()
		value, err := p.expect(tokenNumber, "schema major version")
		if err != nil {
			return nil, err
		}
		parsed, err := strconv.ParseUint(value.text, 10, 16)
		if err != nil || parsed == 0 {
			return nil, diagnostic(value.position, "invalid schema major version %q", value.text)
		}
		major = uint16(parsed)
	}
	if err := p.optionalSemicolon(); err != nil {
		return nil, err
	}

	result := &Schema{
		packageName:   packageName.text,
		position:      packageName.position,
		major:         major,
		documentation: documentation,
	}
	currentCategory := -1

	for {
		doc, err := p.documentation()
		if err != nil {
			return nil, err
		}
		next, err := p.look()
		if err != nil {
			return nil, err
		}
		if next.kind == tokenEOF {
			if doc != "" {
				return nil, diagnostic(next.position, "documentation is not followed by a declaration")
			}
			return result, nil
		}

		name, err := p.expect(tokenIdentifier, "declaration")
		if err != nil {
			return nil, err
		}
		if name.text == "type" {
			value, err := p.parseType(doc)
			if err != nil {
				return nil, err
			}
			switch v := value.(type) {
			case newtype:
				result.newtypes = append(result.newtypes, v)
				appendCategoryType(result, currentCategory, v.name)
			case declaredType:
				result.types = append(result.types, v)
				appendCategoryType(result, currentCategory, v.name)
			}
			continue
		}
		if name.text == "permission" {
			value, err := p.parsePermission(doc)
			if err != nil {
				return nil, err
			}
			result.permissions = append(result.permissions, value)
			continue
		}
		if name.text == "category" {
			next, err := p.look()
			if err != nil {
				return nil, err
			}
			if next.kind == tokenString {
				value, err := p.parseCategory(doc)
				if err != nil {
					return nil, err
				}
				result.categories = append(result.categories, value)
				currentCategory = len(result.categories) - 1
				continue
			}
		}

		fields, delimiter, err := p.parseFields()
		if err != nil {
			return nil, err
		}
		switch delimiter.kind {
		case tokenEqual:
			value, err := p.parseConstructor(name, doc, fields)
			if err != nil {
				return nil, err
			}
			result.constructors = append(result.constructors, value)
			appendCategoryType(result, currentCategory, value.result)
		case tokenArrow:
			value, err := p.parseMethod(name, doc, fields)
			if err != nil {
				return nil, err
			}
			result.methods = append(result.methods, value)
			if currentCategory >= 0 {
				category := &result.categories[currentCategory]
				category.methods = append(category.methods, name.text)
			}
		default:
			return nil, p.unexpected(delimiter, "'=' or '->'")
		}
	}
}

func (p *parser) parseConstructor(name tokenValue, doc string, fields []field) (constructor, error) {
	resultType, err := p.expect(tokenIdentifier, "constructor result type")
	if err != nil {
		return constructor{}, err
	}
	httpStatus := 0
	next, err := p.look()
	if err != nil {
		return constructor{}, err
	}
	if next.kind == tokenIdentifier && next.text == "httpStatus" {
		p.take()
		value, err := p.expect(tokenNumber, "HTTP status")
		if err != nil {
			return constructor{}, err
		}
		httpStatus, err = strconv.Atoi(value.text)
		if err != nil {
			return constructor{}, diagnostic(value.position, "invalid HTTP status %q", value.text)
		}
	}
	if err := p.optionalSemicolon(); err != nil {
		return constructor{}, err
	}
	return constructor{
		name:          name.text,
		documentation: doc,
		fields:        fields,
		result:        resultType.text,
		position:      name.position,
		httpStatus:    httpStatus,
	}, nil
}

func (p *parser) parseMethod(name tokenValue, doc string, fields []field) (method, error) {
	response, err := p.parseTypeRef()
	if err != nil {
		return method{}, err
	}
	result := method{
		name:          name.text,
		documentation: doc,
		fields:        fields,
		response:      response,
		position:      name.position,
		public:        true,
	}

	next, err := p.look()
	if err != nil {
		return method{}, err
	}
	if next.kind == tokenIdentifier && next.text == "throws" {
		p.take()
		value, err := p.parseTypeRef()
		if err != nil {
			return method{}, err
		}
		result.errorType = &value
	}

	var accessSet bool
annotations:
	for {
		next, err := p.look()
		if err != nil {
			return method{}, err
		}
		if next.kind != tokenIdentifier {
			break
		}
		switch next.text {
		case "public", "requires":
			if accessSet {
				return method{}, diagnostic(next.position, "method access is declared more than once")
			}
			p.take()
			accessSet = true
			result.public = next.text == "public"
			if !result.public {
				value, err := p.expect(tokenIdentifier, "permission name")
				if err != nil {
					return method{}, err
				}
				result.permission = value.text
			}
		case "http":
			p.take()
			verb, err := p.expect(tokenIdentifier, "HTTP verb")
			if err != nil {
				return method{}, err
			}
			path, err := p.expect(tokenString, "quoted HTTP path")
			if err != nil {
				return method{}, err
			}
			result.http = append(result.http, httpBinding{
				verb:     verb.text,
				path:     path.text,
				position: next.position,
			})
		default:
			break annotations
		}
	}
	if err := p.optionalSemicolon(); err != nil {
		return method{}, err
	}
	return result, nil
}

func (p *parser) parseCategory(documentation string) (category, error) {
	name, err := p.expect(tokenString, "quoted category name")
	if err != nil {
		return category{}, err
	}
	if err := p.optionalSemicolon(); err != nil {
		return category{}, err
	}
	return category{
		name:          name.text,
		documentation: documentation,
		position:      name.position,
	}, nil
}

func appendCategoryType(schema *Schema, categoryIndex int, name string) {
	if categoryIndex < 0 {
		return
	}
	category := &schema.categories[categoryIndex]
	if !slices.Contains(category.types, name) {
		category.types = append(category.types, name)
	}
}

func (p *parser) parsePermission(documentation string) (permission, error) {
	name, err := p.expect(tokenIdentifier, "permission name")
	if err != nil {
		return permission{}, err
	}
	if err := p.optionalSemicolon(); err != nil {
		return permission{}, err
	}
	return permission{
		name:          name.text,
		documentation: documentation,
		position:      name.position,
	}, nil
}

func (p *parser) parseType(documentation string) (any, error) {
	name, err := p.expect(tokenIdentifier, "type name")
	if err != nil {
		return nil, err
	}
	next, err := p.look()
	if err != nil {
		return nil, err
	}
	if next.kind == tokenEqual {
		p.take()
		base, err := p.expect(tokenIdentifier, "newtype scalar")
		if err != nil {
			return nil, err
		}
		if err := p.optionalSemicolon(); err != nil {
			return nil, err
		}
		return newtype{
			name:          name.text,
			documentation: documentation,
			base:          base.text,
			position:      name.position,
		}, nil
	}

	if err := p.optionalSemicolon(); err != nil {
		return nil, err
	}
	return declaredType{
		name:          name.text,
		documentation: documentation,
		position:      name.position,
	}, nil
}

func (p *parser) parseFields() ([]field, tokenValue, error) {
	var fields []field
	for {
		documentation, err := p.documentation()
		if err != nil {
			return nil, tokenValue{}, err
		}
		next, err := p.look()
		if err != nil {
			return nil, tokenValue{}, err
		}
		if next.kind == tokenEqual || next.kind == tokenArrow {
			if documentation != "" {
				return nil, tokenValue{}, diagnostic(next.position, "documentation is not followed by a field")
			}
			p.take()
			return fields, next, nil
		}
		name, err := p.expect(tokenIdentifier, "field name, '=' or '->'")
		if err != nil {
			return nil, tokenValue{}, err
		}
		if _, err := p.expect(tokenColon, "':'"); err != nil {
			return nil, tokenValue{}, err
		}
		typ, err := p.parseTypeRef()
		if err != nil {
			return nil, tokenValue{}, err
		}
		fields = append(fields, field{
			name:          name.text,
			documentation: documentation,
			typ:           typ,
			position:      name.position,
		})
	}
}

func (p *parser) parseTypeRef() (typeRef, error) {
	name, err := p.expect(tokenIdentifier, "type")
	if err != nil {
		return typeRef{}, err
	}
	result := typeRef{
		name: name.text,
	}
	if name.text == "list" {
		if _, err := p.expect(tokenLess, "'<'"); err != nil {
			return typeRef{}, err
		}
		element, err := p.parseTypeRef()
		if err != nil {
			return typeRef{}, err
		}
		if _, err := p.expect(tokenGreater, "'>'"); err != nil {
			return typeRef{}, err
		}
		result.name = ""
		result.element = &element
	}
	next, err := p.look()
	if err != nil {
		return typeRef{}, err
	}
	if next.kind == tokenQuestion {
		p.take()
		result.optional = true
	}
	return result, nil
}

func (p *parser) documentation() (string, error) {
	var lines []string
	for {
		next, err := p.look()
		if err != nil {
			return "", err
		}
		if next.kind != tokenDoc {
			return doc.Normalize(strings.Join(lines, "\n")), nil
		}
		p.take()
		lines = append(lines, next.text)
	}
}

func (p *parser) optionalSemicolon() error {
	next, err := p.look()
	if err != nil {
		return err
	}
	if next.kind != tokenSemicolon {
		return nil
	}
	_, err = p.take()
	return err
}

func (p *parser) expect(kind tokenKind, description string) (tokenValue, error) {
	value, err := p.take()
	if err != nil {
		return tokenValue{}, err
	}
	if value.kind != kind {
		return tokenValue{}, p.unexpected(value, description)
	}
	return value, nil
}

func (p *parser) unexpected(value tokenValue, expected string) error {
	description := value.text
	if value.kind == tokenEOF {
		description = "end of file"
	}
	return diagnostic(value.position, "unexpected %q; expected %s", description, expected)
}

func (p *parser) look() (tokenValue, error) {
	if p.peek != nil {
		return *p.peek, nil
	}
	value, err := p.lexer.next()
	if err != nil {
		return tokenValue{}, err
	}
	p.peek = &value
	return value, nil
}

func (p *parser) take() (tokenValue, error) {
	value, err := p.look()
	if err != nil {
		return tokenValue{}, err
	}
	p.peek = nil
	return value, nil
}
