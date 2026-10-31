// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package browser

import (
	"fmt"
	"net/http"
	"strings"

	"go.astrophena.name/plug"
)

func (b *browser) serveMarkdown(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", b.schema.Package+"-api.md"))
	if r.Method == http.MethodHead {
		return
	}
	w.Write([]byte(b.markdown(requestOrigin(r))))
}

func (b *browser) markdown(origin string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s API\n\n", b.serviceName())
	if b.schema.Documentation != "" {
		fmt.Fprintf(&sb, "%s\n\n", b.docMarkdown(b.schema.Documentation))
	}
	fmt.Fprintf(&sb, "Package: `%s`  \nPlug version: `%d`\n\n", b.schema.Package, b.schema.Major)
	sb.WriteString("This document describes the Plug service contract. HTTP routes are available only where listed. The deployment handles authentication and may restrict access to public methods.\n\n")

	if len(b.schema.Categories) > 0 {
		sb.WriteString("## Categories\n\n")
		for _, category := range b.schema.Categories {
			fmt.Fprintf(&sb, "### %s\n\n", category.Name)
			if category.Documentation != "" {
				fmt.Fprintf(&sb, "%s\n\n", b.docMarkdown(category.Documentation))
			}
			for _, name := range category.Methods {
				fmt.Fprintf(&sb, "- Method `%s`\n", name)
			}
			for _, name := range category.Types {
				fmt.Fprintf(&sb, "- Type `%s`\n", name)
			}
			sb.WriteByte('\n')
		}
	}

	sb.WriteString("## Methods\n\n")
	for _, method := range b.methods {
		b.markdownMethod(&sb, method, origin)
	}

	sb.WriteString("## Types\n\n")
	for _, typ := range b.types {
		b.markdownType(&sb, typ)
	}

	if len(b.schema.Permissions) > 0 {
		sb.WriteString("## Permissions\n\n")
		for _, permission := range b.schema.Permissions {
			fmt.Fprintf(&sb, "- `%s`", permission.Name)
			b.markdownListDoc(&sb, permission.Documentation)
		}
		sb.WriteByte('\n')
	}

	if b.httpRoutes > 0 {
		sb.WriteString("## HTTP conventions\n\n")
		sb.WriteString("GET and DELETE use path and query values. Other verbs also accept JSON. Path and query values take precedence. Repeat query parameters for lists, encode bytes as base64, and omit optional values. Records use field names as JSON keys; sums use `type` for the constructor, including fieldless variants. Empty lists use `[]` and empty bytes use `\"\"`. Absent optional fields are omitted; input also accepts `null`. Integers are numbers, so JavaScript may lose precision for large 64-bit values. An absent optional result returns HTTP 200 with `null`. A `void` result returns 204 No Content. Errors contain `code` and `message`; declared errors also contain `details`.\n\n")
	}
	sb.WriteString("## Plug protocol\n\n")
	sb.WriteString("Plug runs over an ordered byte stream. A client sends `PLUG`, the wire version, and schema identity. Each request frame carries a request ID, method ID, and encoded fields. The server decodes, authorizes, and invokes the handler, then returns a response, declared error, or core error. Native and HTTP calls use the same handlers.\n\n")
	sb.WriteString("The inspection handshake returns the wire version, schema hashes, and canonical source. Compatible schemas may add declarations, sum constructors, and optional tail fields. See the [full protocol](https://github.com/astrophena/plug/tree/master/docs/protocol.md).\n\n")

	sb.WriteString("## Wire identity\n\n")
	fmt.Fprintf(&sb, "- Family: `%s`\n- Fingerprint: `%s`\n\n", fingerprint(b.schema.Family), fingerprint(b.schema.Fingerprint))
	sb.WriteString("Package and version define compatibility. The fingerprint identifies this schema. New declarations and optional tail fields are compatible; other changes need a new version.\n\n")
	sb.WriteString("## Canonical schema\n\n")
	markdownCode(&sb, b.schema.Source)
	return sb.String()
}

func (b *browser) markdownMethod(sb *strings.Builder, method method, origin string) {
	info := method.Info
	fmt.Fprintf(sb, "<a id=\"method-%s\"></a>\n\n", info.Name)
	fmt.Fprintf(sb, "### `%s`\n\n", info.Name)
	if info.Documentation != "" {
		fmt.Fprintf(sb, "%s\n\n", b.docMarkdown(info.Documentation))
	}
	fmt.Fprintf(sb, "Method ID: `%s`. ", hexID(info.ID))
	if info.Public {
		sb.WriteString("Access: `public` (no Plug permission required).\n\n")
	} else {
		fmt.Fprintf(sb, "Permission: `%s`.\n\n", info.Permission)
	}
	markdownCode(sb, methodDeclaration(info))
	if len(info.Fields) > 0 {
		sb.WriteString("Parameters:\n\n")
		b.markdownFields(sb, info.Fields)
	}
	fmt.Fprintf(sb, "Result: `%s`.\n\n", typeSyntax(info.Response))
	if info.Error != "" {
		fmt.Fprintf(sb, "Declared error: `%s`.\n\n", info.Error)
		for _, constructor := range method.Errors {
			status := constructor.HTTPStatus
			if status == 0 {
				status = http.StatusBadRequest
			}
			fmt.Fprintf(sb, "- `%s`: HTTP %d", constructor.Name, status)
			b.markdownListDoc(sb, constructor.Documentation)
		}
		sb.WriteByte('\n')
	}
	for _, example := range b.httpExamples(info, origin) {
		fmt.Fprintf(sb, "#### HTTP `%s %s`\n\n", example.Binding.Verb, example.Binding.Path)
		if len(example.Fields) > 0 {
			sb.WriteString("Request mapping:\n\n")
			for _, field := range example.Fields {
				fmt.Fprintf(sb, "- `%s` (`%s`): %s, %s.\n", field.Info.Name, typeSyntax(field.Info.Type), field.Location, field.Representation)
			}
			sb.WriteByte('\n')
		} else {
			sb.WriteString("No request parameters.\n\n")
		}
		sb.WriteString("Example request:\n\n")
		markdownCode(sb, example.Curl)
		if example.OptionalFieldsOmitted {
			sb.WriteString("Optional fields are omitted.\n\n")
		}
		if example.Void {
			sb.WriteString("Success returns HTTP 204 No Content.\n\n")
		} else {
			sb.WriteString("Success normally returns HTTP 200 with JSON. Example response:\n\n")
			markdownCode(sb, example.JSON)
		}
	}
	if len(info.HTTP) > 0 && info.Error != "" {
		sb.WriteString("Declared errors use an envelope with `code`, `message`, and `details`. Example:\n\n")
		markdownCode(sb, b.exampleErrorJSON(info.Error))
	}
}

func (b *browser) markdownType(sb *strings.Builder, typ typeDoc) {
	info := typ.Info
	fmt.Fprintf(sb, "<a id=\"type-%s\"></a>\n\n", info.Name)
	fmt.Fprintf(sb, "### `%s` (%s)\n\n", info.Name, typeKind(info))
	if info.Documentation != "" {
		fmt.Fprintf(sb, "%s\n\n", b.docMarkdown(info.Documentation))
	}
	for _, declaration := range typeDeclarations(info) {
		if declaration.Documentation != "" && declaration.Documentation != info.Documentation {
			fmt.Fprintf(sb, "%s\n\n", b.docMarkdown(declaration.Documentation))
		}
		if declaration.ID != 0 {
			fmt.Fprintf(sb, "Constructor ID: `%s`. ", hexID(declaration.ID))
			if declaration.HTTPStatus != 0 {
				fmt.Fprintf(sb, "HTTP status: %d. ", declaration.HTTPStatus)
			}
			sb.WriteString("\n\n")
		}
		markdownCode(sb, declaration.Source)
		if len(declaration.Fields) > 0 {
			b.markdownFields(sb, declaration.Fields)
		}
	}
	if b.httpRoutes > 0 {
		sb.WriteString("Example JSON value:\n\n")
		markdownCode(sb, b.exampleTypeJSON(info))
	}
	if len(typ.Requests) > 0 || len(typ.Responses) > 0 || len(typ.Failures) > 0 || len(typ.Types) > 0 {
		sb.WriteString("Used by:\n\n")
		for _, method := range typ.Requests {
			fmt.Fprintf(sb, "- Request parameter of `%s`\n", method.Name)
		}
		for _, method := range typ.Responses {
			fmt.Fprintf(sb, "- Returned by `%s`\n", method.Name)
		}
		for _, method := range typ.Failures {
			fmt.Fprintf(sb, "- Thrown by `%s`\n", method.Name)
		}
		for _, owner := range typ.Types {
			fmt.Fprintf(sb, "- Referenced by type `%s`\n", owner.Name)
		}
		sb.WriteByte('\n')
	}
}

func (b *browser) markdownFields(sb *strings.Builder, fields []plug.FieldInfo) {
	for _, field := range fields {
		fmt.Fprintf(sb, "- `%s`: `%s`", field.Name, typeSyntax(field.Type))
		if field.Type.Optional {
			sb.WriteString(" (optional)")
		} else {
			sb.WriteString(" (required)")
		}
		b.markdownListDoc(sb, field.Documentation)
	}
	sb.WriteByte('\n')
}

func markdownCode(sb *strings.Builder, source string) {
	for line := range strings.SplitSeq(strings.TrimSuffix(source, "\n"), "\n") {
		sb.WriteString("    ")
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	sb.WriteByte('\n')
}
