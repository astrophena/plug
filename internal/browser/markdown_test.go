// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package browser

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
)

func TestBrowserMarkdown(t *testing.T) {
	var (
		b = newBrowser(t, testSchema(t), "/reference")
		r = httptest.NewRequest(http.MethodGet, "/reference/index.md", nil)
	)
	r.Host = "api.example.test"
	r.Header.Set("X-Forwarded-Proto", "https")

	w := b.serve(t, r)
	testutil.AssertEqual(t, w.Code, http.StatusOK)
	testutil.AssertEqual(t, w.Header().Get("Content-Type"), "text/markdown; charset=utf-8")
	testutil.AssertEqual(t, w.Header().Get("Content-Disposition"), `attachment; filename="greeter-api.md"`)
	for _, want := range []string{
		"# Greeter API",
		"## Methods",
		"## Types",
		"## Permissions",
		"## Canonical schema",
		"### `greeter.getUser`",
		"greeter.getUser returns the requested user.",
		"Method ID: `",
		"`id`: `UserID` (required)\n\n  id identifies the user to look up.",
		"Declared error: `LookupError`.",
		"`notFound`: HTTP 404",
		"#### HTTP `GET /v1/users/{id}`",
		"#### HTTP `POST /v1/get-user`",
		"https://api.example.test/v1/users/1",
		"Request mapping:",
		"Example response:",
		"### `Role` (sum type)",
		"admin represents an administrative user.",
		"Constructor ID: `",
		"Example JSON value:",
		"## Wire identity",
		"package greeter version 1",
		"category \"Users\"",
	} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("Markdown does not contain %q", want)
		}
	}

	w = b.serve(t, httptest.NewRequest(http.MethodHead, "/reference/index.md", nil))
	testutil.AssertEqual(t, w.Code, http.StatusOK)
	testutil.AssertEqual(t, w.Body.Len(), 0)
}

func TestMarkdownDocumentation(t *testing.T) {
	b := newBrowser(t, documentedSchema(t), "/reference")
	w := b.serve(t, httptest.NewRequest(http.MethodGet, "/reference/index.md", nil))
	testutil.AssertEqual(t, w.Code, http.StatusOK)
	body := w.Body.String()
	for _, label := range []string{"Package", "Category", "Method", "Type", "Constructor", "Permission", "Field"} {
		want := label + " Text with [User](#type-User) and [greeter.getUser](#method-greeter.getUser)."
		if !strings.Contains(body, want) {
			t.Errorf("Markdown does not contain %q", want)
		}
	}
	for _, want := range []string{
		`<a id="type-User"></a>`,
		`<a id="method-greeter.getUser"></a>`,
		"\n  ##### Usage\n",
		"\n    - Read [User](#type-User).\n    - Keep \\[Missing].\n",
		"\n      [greeter.getUser] id: 1\n          nested\n",
		`Raw \<script>alert("hello")\</script>.`,
		`[the guide](https://example.com/guide?q=one&other=two)`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Markdown does not contain %q", want)
		}
	}
}
