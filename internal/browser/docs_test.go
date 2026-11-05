// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package browser_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.astrophena.name/plug/example/greeter"
	"go.astrophena.name/plug/plugimpl"
)

func TestBrowserDocumentation(t *testing.T) {
	const text = `Text with [User] and [greeter.getUser].

# Usage

  - Read [User].
  - Keep [Missing].

Example:

	[greeter.getUser] id: 1
	    nested

Raw <script>alert("hello")</script>. See [the guide].

[the guide]: https://example.com/guide?q=one&other=two
`
	schema := plugimpl.CopySchema(&greeter.Schema)
	schema.Documentation = "Package " + text
	for i := range schema.Permissions {
		schema.Permissions[i].Documentation = "Permission " + text
	}
	for i := range schema.Categories {
		schema.Categories[i].Documentation = "Category " + text
	}
	for i := range schema.Methods {
		method := &schema.Methods[i]
		method.Documentation = "Method " + text
		for j := range method.Fields {
			method.Fields[j].Documentation = "Field " + text
		}
	}
	for i := range schema.Types {
		typ := &schema.Types[i]
		typ.Documentation = "Type " + text
		for j := range typ.Constructors {
			constructor := &typ.Constructors[j]
			constructor.Documentation = "Constructor " + text
			for k := range constructor.Fields {
				constructor.Fields[k].Documentation = "Field " + text
			}
		}
	}
	mount := "/reference"
	browser := newBrowser(t, schema, mount)
	for path, labels := range map[string][]string{
		"/":                        {"Package", "Category", "Method", "Type", "Permission"},
		"/methods/greeter.getUser": {"Method", "Field", "Constructor"},
		"/types/User":              {"Type", "Constructor", "Field"},
	} {
		response := browser.serve(t, httptest.NewRequest(http.MethodGet, mount+path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, response.Code)
		}
		body := response.Body.String()
		root := ""
		if path != "/" {
			root = "../"
		}
		for _, label := range labels {
			want := "<p>" + label + ` Text with <a href="` + root + `types/User">User</a> and <a href="` + root + `methods/greeter.getUser">greeter.getUser</a>.`
			if !strings.Contains(body, want) {
				t.Errorf("GET %s does not contain %q", path, want)
			}
		}
		for _, want := range []string{
			"<ul>\n<li>Read ",
			"<li>Keep [Missing].",
			"<pre>[greeter.getUser] id: 1\n    nested\n</pre>",
			`Raw &lt;script&gt;alert(&quot;hello&quot;)&lt;/script&gt;.`,
			`href="https://example.com/guide?q=one&amp;other=two"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s does not contain %q", path, want)
			}
		}
		if got, want := strings.Count(body, "<h4>Usage</h4>"), strings.Count(body, `class="documentation"`); got != want {
			t.Errorf("GET %s rendered %d headings for %d comments", path, got, want)
		}
		if strings.Contains(body, `<h4 id=`) || strings.Contains(body, `<script>alert(`) {
			t.Errorf("GET %s contains a generated heading ID or unescaped HTML", path)
		}
	}

	response := browser.serve(t, httptest.NewRequest(http.MethodGet, mount+"/index.md", nil))
	body := response.Body.String()
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
