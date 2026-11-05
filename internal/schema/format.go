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

// formatSchema renders a deterministic documented schema.
func formatSchema(schema *Schema) string {
	view := newSchemaView(schema)
	return view.format()
}

func (v *schemaView) format() string {
	var buf bytes.Buffer
	writeDoc(&buf, "", v.schema.documentation)
	fmt.Fprintf(&buf, "package %s version %d\n", v.schema.packageName, v.schema.major)

	for _, perm := range v.permissions {
		buf.WriteByte('\n')
		writeDoc(&buf, "", perm.documentation)
		fmt.Fprintf(&buf, "permission %s\n", perm.name)
	}

	var (
		categorizedTypes   = make(map[string]bool, len(v.newtypes)+len(v.groupNames))
		categorizedMethods = make(map[string]bool, len(v.methods))
	)
	for _, category := range v.categories {
		for _, name := range category.types {
			categorizedTypes[name] = true
		}
		for _, name := range category.methods {
			categorizedMethods[name] = true
		}
	}
	writeNewtype := func(name string) {
		value := v.newtypeByName[name]
		buf.WriteByte('\n')
		writeDoc(&buf, "", value.documentation)
		fmt.Fprintf(&buf, "type %s = %s\n", value.name, value.base)
	}
	writeConstructorGroup := func(name string) {
		if dt, ok := v.declaredTypes[name]; ok {
			buf.WriteByte('\n')
			writeDoc(&buf, "", dt.documentation)
			fmt.Fprintf(&buf, "type %s\n", dt.name)
		}
		for _, constructor := range v.groups[name] {
			buf.WriteByte('\n')
			writeDoc(&buf, "", constructor.documentation)
			tail := "= " + constructor.result
			if constructor.httpStatus != 0 {
				tail += fmt.Sprintf(" httpStatus %d", constructor.httpStatus)
			}
			writeDeclaration(&buf, constructor.name, constructor.fields, tail)
		}
	}
	writeMethod := func(method method) {
		buf.WriteByte('\n')
		writeDoc(&buf, "", method.documentation)
		writeMethodDeclaration(&buf, method)
	}
	for _, value := range v.newtypes {
		name := value.name
		if !categorizedTypes[name] {
			writeNewtype(name)
		}
	}
	for _, name := range v.groupNames {
		if !categorizedTypes[name] {
			writeConstructorGroup(name)
		}
	}
	for _, method := range v.methods {
		if !categorizedMethods[method.name] {
			writeMethod(method)
		}
	}
	for _, category := range v.categories {
		buf.WriteByte('\n')
		writeDoc(&buf, "", category.documentation)
		fmt.Fprintf(&buf, "category %s\n", strconv.Quote(category.name))
		for _, name := range category.types {
			if _, ok := v.newtypeByName[name]; ok {
				writeNewtype(name)
			} else {
				writeConstructorGroup(name)
			}
		}
		for _, name := range category.methods {
			writeMethod(v.methodByName[name])
		}
	}
	return buf.String()
}

func writeMethodDeclaration(buf *bytes.Buffer, method method) {
	var clauses []string
	if method.errorType != nil {
		clauses = append(clauses, "throws "+method.errorType.String())
	}
	if method.public {
		clauses = append(clauses, "public")
	} else {
		clauses = append(clauses, "requires "+method.permission)
	}
	for _, binding := range method.http {
		clauses = append(clauses, "http "+binding.verb+" "+strconv.Quote(binding.path))
	}
	tail := "-> " + method.response.String()
	if len(clauses) == 1 {
		tail += " " + clauses[0]
	}
	writeDeclaration(buf, method.name, method.fields, tail)
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
	for _, field := range fields {
		if field.documentation != "" {
			hasFieldDocs = true
			break
		}
	}
	if !hasFieldDocs {
		fmt.Fprintf(buf, "%s", name)
		for _, field := range fields {
			fmt.Fprintf(buf, " %s: %s", field.name, field.typ.String())
		}
		fmt.Fprintf(buf, " %s\n", tail)
		return
	}

	fmt.Fprintf(buf, "%s\n", name)
	for _, field := range fields {
		writeDoc(buf, "  ", field.documentation)
		fmt.Fprintf(buf, "  %s: %s\n", field.name, field.typ.String())
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
