// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package browser

import (
	"context"
	"io"
	"io/fs"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/base/web"
	"go.astrophena.name/plug"
	"go.astrophena.name/plug/internal/schema"
)

func TestBrowser(t *testing.T) {
	var (
		b    = newBrowser(t, testSchema(t), "/reference")
		css  = `href="/` + b.server.StaticHashName("static/css/plug-schema.css") + `"`
		icon = `rel="icon" type="image/svg+xml" href="/` + b.server.StaticHashName("static/icons/plug.svg") + `"`
		js   = `src="/` + b.server.StaticHashName("static/js/plug-schema.js") + `"`
	)
	cases := map[string]struct {
		method      string
		path        string
		status      int
		contains    []string
		notContains []string
	}{
		"index": {
			path:   "/",
			status: http.StatusOK,
			contains: []string{
				`<h1>Greeter API</h1>`,
				`href="./the-other-side-of-the-socket"`,
				`<section id="http"><h2>HTTP</h2>`,
				"Users",
				"Service",
				"Data model",
				"user.read",
				`type="search" id="browser-filter" data-browser-filter`,
				`<span class="badge">4 HTTP routes</span>`,
				`<li><span class="verb">GET</span><code>/v1/users/{id}</code></li>`,
				icon,
				css,
				js,
				`href="types/User"`,
				`href="methods/greeter.getUser"`,
				`href="./index.md"`,
			},
			notContains: []string{
				"Other methods",
			},
		},
		"protocol page": {
			path:   "/the-other-side-of-the-socket",
			status: http.StatusOK,
			contains: []string{
				`href="https://github.com/astrophena/plug/tree/master/docs/protocol.md"`,
				`<a href="./">API</a>`,
			},
			notContains: []string{
				`data-browser-filter`,
			},
		},
		"method": {
			path:   "/methods/greeter.getUser",
			status: http.StatusOK,
			contains: []string{
				"greeter.getUser returns the requested user.",
				`<th>Description</th>`,
				`<span class="field-presence required">Required</span>`,
				`curl --request GET \`,
				`value="/v1/users/1"`,
				`curl --request POST \`,
				`value="/v1/get-user"`,
				`&#34;id&#34;: 1`,
				`data-http-example data-method="POST"`,
				"2 constructors with HTTP status",
				`&#34;name&#34;: &#34;example&#34;`,
				`&#34;code&#34;: &#34;forbidden&#34;`,
				`Returns <code><a class="type" href="../types/LookupError">LookupError</a></code>.`,
				"404",
				`href="../types/UserID"`,
				`href="../types/LookupError"`,
				`href="../index.md"`,
			},
		},
		"public method": {
			path:   "/methods/greeter.ping",
			status: http.StatusOK,
			contains: []string{
				"Access <code>public</code>",
				"HTTP 204 No Content",
			},
		},
		"method with query": {
			path:   "/methods/greeter.listUsers",
			status: http.StatusOK,
			contains: []string{
				"greeter.listUsers lists at most the requested number of users.",
				"limit specifies the maximum number of users to return.",
				`<th>Description</th>`,
				`value="/v1/users?limit=1"`,
				`https://api.example.test/v1/users?limit=1`,
			},
		},
		"type": {
			path:   "/types/User",
			status: http.StatusOK,
			contains: []string{
				"user represents a user known to the greeter.",
				`<span class="field-presence required">Required</span>`,
				`<span class="field-presence optional">Optional</span>`,
				`<span class="primitive">string</span>?`,
				`href="../types/UserID"`,
				`href="../methods/greeter.getUser"`,
			},
			notContains: []string{
				`opt string`,
			},
		},
		"sum type": {
			path:   "/types/Role",
			status: http.StatusOK,
			contains: []string{
				`&#34;type&#34;: &#34;admin&#34;`,
				`&#34;level&#34;: 1`,
			},
			notContains: []string{
				"@type",
			},
		},
		"missing method": {
			path:   "/methods/greeter.missing",
			status: http.StatusNotFound,
		},
		"missing type": {
			path:   "/types/Missing",
			status: http.StatusNotFound,
		},
		"method not allowed": {
			method: http.MethodPost,
			path:   "/",
			status: http.StatusMethodNotAllowed,
		},
		"head": {
			method: http.MethodHead,
			path:   "/types/User",
			status: http.StatusOK,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if tc.method == "" {
				tc.method = http.MethodGet
			}
			r := httptest.NewRequest(tc.method, "/reference"+tc.path, nil)
			r.Host = "api.example.test"
			r.Header.Set("X-Forwarded-Proto", "https")

			w := b.serve(t, r)
			testutil.AssertEqual(t, w.Code, tc.status)
			body := w.Body.String()
			for _, value := range tc.contains {
				if !strings.Contains(body, value) {
					t.Errorf("browser does not contain %q", value)
				}
			}
			for _, value := range tc.notContains {
				if strings.Contains(body, value) {
					t.Errorf("browser unexpectedly contains %q", value)
				}
			}
			if tc.method == http.MethodHead && body != "" {
				t.Errorf("HEAD response contains a body")
			}
		})
	}

	t.Run("assets", func(t *testing.T) {
		for _, name := range []string{"static/css/plug-schema.css", "static/js/plug-schema.js", "static/icons/plug.svg"} {
			path := "/" + b.server.StaticHashName(name)
			w := b.serve(t, httptest.NewRequest(http.MethodGet, path, nil))
			if w.Code != http.StatusOK || w.Body.Len() == 0 {
				t.Errorf("GET %s = %d, %d bytes", path, w.Code, w.Body.Len())
			}
		}
	})
}

func TestBrowserWithoutHTTP(t *testing.T) {
	s := testSchema(t)
	for i := range s.Methods {
		s.Methods[i].HTTP = nil
	}
	b := newBrowser(t, s, "")
	for _, path := range []string{"/", "/types/User"} {
		w := b.serve(t, httptest.NewRequest(http.MethodGet, path, nil))
		testutil.AssertEqual(t, w.Code, http.StatusOK)
		body := w.Body.String()
		for _, unexpected := range []string{`href="./#http"`, `id="http"`, `<h2>JSON</h2>`, "HTTP routes</span>"} {
			if strings.Contains(body, unexpected) {
				t.Errorf("GET %s unexpectedly contains %q", path, unexpected)
			}
		}
	}
}

func TestBrowserUncategorized(t *testing.T) {
	s := testSchema(t)
	s.Categories = []plug.CategoryInfo{
		{
			Name:    "User lookup",
			Methods: []string{"greeter.getUser"},
		},
		{
			Name:  "Identifiers",
			Types: []string{"UserID"},
		},
	}
	b := newBrowser(t, s, "")
	w := b.serve(t, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, want := range []string{"User lookup", "Identifiers", "Other methods", "Other types", "greeter.listUsers", "LookupError"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("browser does not contain %q", want)
		}
	}
}

func TestBrowserRejectsInvalidCategories(t *testing.T) {
	cases := map[string]struct {
		categories []plug.CategoryInfo
		want       string
	}{
		"empty": {
			categories: []plug.CategoryInfo{
				{
					Name: "Users",
				},
			},
			want: "contains no types or methods",
		},
		"unknown method": {
			categories: []plug.CategoryInfo{
				{
					Name:    "Users",
					Methods: []string{"greeter.missing"},
				},
			},
			want: "unknown method",
		},
		"unknown type": {
			categories: []plug.CategoryInfo{
				{
					Name:  "Data",
					Types: []string{"Missing"},
				},
			},
			want: "unknown type",
		},
		"method in two categories": {
			categories: []plug.CategoryInfo{
				{
					Name:    "Users",
					Methods: []string{"greeter.getUser"},
				},
				{
					Name:    "Lookup",
					Methods: []string{"greeter.getUser"},
				},
			},
			want: "appears in categories",
		},
		"type in two categories": {
			categories: []plug.CategoryInfo{
				{
					Name:  "Data",
					Types: []string{"User"},
				},
				{
					Name:  "Responses",
					Types: []string{"User"},
				},
			},
			want: "appears in categories",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := testSchema(t)
			s.Categories = tc.categories
			_, err := New(s)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestDocumentation(t *testing.T) {
	const mount = "/reference"
	b := newBrowser(t, documentedSchema(t), mount)
	for path, labels := range map[string][]string{
		"/":                        {"Package", "Category", "Method", "Type", "Permission"},
		"/methods/greeter.getUser": {"Method", "Field", "Constructor"},
		"/types/User":              {"Type", "Constructor", "Field"},
	} {
		w := b.serve(t, httptest.NewRequest(http.MethodGet, mount+path, nil))
		testutil.AssertEqual(t, w.Code, http.StatusOK)
		body := w.Body.String()
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
}

func TestStaticFS(t *testing.T) {
	app := fstest.MapFS{
		"static/css/plug-schema.css": &fstest.MapFile{
			Data: []byte("application override"),
		},
		"static/css/application.css": &fstest.MapFile{
			Data: []byte("application asset"),
		},
	}
	fsys := StaticFS(app)
	cases := map[string]struct {
		path string
		want string
	}{
		"application asset": {
			path: "static/css/application.css",
			want: "application asset",
		},
		"application takes precedence": {
			path: "static/css/plug-schema.css",
			want: "application override",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := fs.ReadFile(fsys, tc.path)
			if err != nil {
				t.Fatal(err)
			}
			testutil.AssertEqual(t, string(data), tc.want)
		})
	}
}

type browserClient struct {
	server *web.Server
	client *http.Client
}

func testSchema(t *testing.T) *plug.Schema {
	t.Helper()
	src, err := os.ReadFile("../../example/greeter/schema.plug")
	if err != nil {
		t.Fatal(err)
	}
	s, err := schema.Parse("greeter.plug", src)
	if err != nil {
		t.Fatal(err)
	}
	out := schema.RuntimeSchema(s)
	return &out
}

func newBrowser(t *testing.T, schema *plug.Schema, prefix string) *browserClient {
	t.Helper()
	h, err := New(schema)
	if err != nil {
		t.Fatal(err)
	}

	// t.TempDir includes the test name, which can exceed the Unix socket path
	// limit under Termux's long temporary directory prefix.
	dir, err := os.MkdirTemp("", "plug-browser-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})

	var (
		mux   = http.NewServeMux()
		ready = make(chan struct{})
		done  = make(chan error, 1)
	)
	mux.Handle(prefix+"/", http.StripPrefix(prefix, h))
	s := &web.Server{
		Addr:     filepath.Join(dir, "web.sock"),
		Mux:      mux,
		StaticFS: StaticFS(),
		Ready:    func() { close(ready) },
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go func() { done <- s.ListenAndServe(ctx) }()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("start browser server: %v", err)
	case <-ctx.Done():
		cancel()
		<-done
		t.Fatal(ctx.Err())
	}
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return new(net.Dialer).DialContext(ctx, "unix", s.Addr)
		},
	}
	t.Cleanup(transport.CloseIdleConnections)
	return &browserClient{
		server: s,
		client: &http.Client{
			Transport: transport,
		},
	}
}

func (b *browserClient) serve(t *testing.T, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	r = r.Clone(t.Context())
	r.RequestURI = ""
	r.URL.Scheme = "http"
	r.URL.Host = "api.example.test"
	response, err := b.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	w := httptest.NewRecorder()
	w.Code = response.StatusCode
	maps.Copy(w.Header(), response.Header)
	if _, err := io.Copy(w.Body, response.Body); err != nil {
		t.Fatal(err)
	}
	return w
}

func documentedSchema(t *testing.T) *plug.Schema {
	t.Helper()
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
	s := testSchema(t)
	s.Documentation = "Package " + text
	for i := range s.Permissions {
		s.Permissions[i].Documentation = "Permission " + text
	}
	for i := range s.Categories {
		s.Categories[i].Documentation = "Category " + text
	}
	for i := range s.Methods {
		m := &s.Methods[i]
		m.Documentation = "Method " + text
		for j := range m.Fields {
			m.Fields[j].Documentation = "Field " + text
		}
	}
	for i := range s.Types {
		typ := &s.Types[i]
		typ.Documentation = "Type " + text
		for j := range typ.Constructors {
			c := &typ.Constructors[j]
			c.Documentation = "Constructor " + text
			for k := range c.Fields {
				c.Fields[k].Documentation = "Field " + text
			}
		}
	}
	return s
}
