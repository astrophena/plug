// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package browser_test

import (
	"context"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.astrophena.name/base/web"
	"go.astrophena.name/plug"
	"go.astrophena.name/plug/example/greeter"
	"go.astrophena.name/plug/internal/browser"
	"go.astrophena.name/plug/plugimpl"
)

func TestBrowser(t *testing.T) {
	browser := newBrowser(t, &greeter.Schema, "")
	stylesheet := `href="/` + browser.server.StaticHashName("static/css/plug-schema.css") + `"`
	favicon := `rel="icon" type="image/svg+xml" href="/` + browser.server.StaticHashName("static/icons/plug.svg") + `"`
	script := `src="/` + browser.server.StaticHashName("static/js/plug-schema.js") + `"`
	cases := map[string]struct {
		method      string
		path        string
		status      int
		contains    []string
		notContains []string
	}{
		"index": {
			method: http.MethodGet,
			path:   "/",
			status: http.StatusOK,
			contains: []string{
				`<h1>Greeter API</h1>`,
				"Schema notation",
				`<code>throws E</code>`,
				"Protocol overview",
				`href="./the-other-side-of-the-socket"`,
				`<section id="http"><h2>HTTP</h2>`,
				"Methods can require these permissions.",
				"HTTP errors",
				"Wire identity",
				"Fingerprint",
				"Users",
				"Users groups operations that read and enumerate greeter users.",
				"Service",
				"Service groups service-level diagnostics.",
				"Data model",
				"Data model groups values exchanged by the greeter API.",
				"user.read",
				`type="search" id="browser-filter" data-browser-filter`,
				`placeholder="Method, type, or HTTP path"`,
				`data-no-results aria-live="polite" hidden>No results found.`,
				`<span class="badge">Package greeter</span>`,
				`<span class="badge">Plug version 1</span>`,
				`<span class="badge">4 HTTP routes</span>`,
				`<li><span class="verb">GET</span><code>/v1/users/{id}</code></li>`,
				favicon,
				stylesheet,
				script,
				`href="types/User"`,
				`href="methods/greeter.getUser"`,
				`href="./index.md"`,
				`Download Markdown`,
			},
			notContains: []string{
				"onkeyup",
				"onsubmit",
				"Other methods",
			},
		},
		"protocol page": {
			method: http.MethodGet,
			path:   "/the-other-side-of-the-socket",
			status: http.StatusOK,
			contains: []string{
				"Plug protocol",
				"Plug runs over an ordered byte stream.",
				"native → handshake + frames",
				"authenticate + authorize",
				"Call flow",
				"The client sends <code>PLUG</code>, the wire version, and schema identity.",
				"A frame carries the request ID, method ID, and encoded fields.",
				"Inspect a server",
				`connection.sendall(b&#34;PLUG\x01\x01&#34;)`,
				`print(f&#34;fingerprint: {fingerprint}&#34;)`,
				"Compatible schemas may add declarations, sum constructors, and optional tail fields.",
				`href="https://github.com/astrophena/plug/tree/master/docs/protocol.md"`,
				`<a href="./">API</a>`,
				stylesheet,
				script,
			},
			notContains: []string{
				`data-browser-filter`,
			},
		},
		"method": {
			method: http.MethodGet,
			path:   "/methods/greeter.getUser",
			status: http.StatusOK,
			contains: []string{
				"greeter.getUser returns the requested user.",
				"Method ID",
				"Permission",
				"Declaration",
				`<th>Description</th>`,
				`<span class="field-presence required">Required</span>`,
				`curl --request GET \`,
				`value="/v1/users/1"`,
				`curl --request POST \`,
				`value="/v1/get-user"`,
				`&#34;id&#34;: 1`,
				`data-http-example data-method="POST"`,
				"Uses the current session.",
				"Declared errors",
				"2 constructors with HTTP status",
				"Request mapping",
				"JSON body",
				"URL-encoded scalar",
				"Example JSON response",
				`&#34;name&#34;: &#34;example&#34;`,
				"Error envelope",
				`&#34;code&#34;: &#34;forbidden&#34;`,
				`Returns <code><a class="type" href="../types/LookupError">LookupError</a></code>.`,
				"404",
				`href="../types/UserID"`,
				`href="../types/LookupError"`,
				`href="../index.md"`,
				stylesheet,
				script,
			},
		},
		"public method": {
			method: http.MethodGet,
			path:   "/methods/greeter.ping",
			status: http.StatusOK,
			contains: []string{
				"Access <code>public</code>",
				"requires no Plug permission",
				"deployment may still restrict access",
				"HTTP 204 No Content",
			},
		},
		"method with query": {
			method: http.MethodGet,
			path:   "/methods/greeter.listUsers",
			status: http.StatusOK,
			contains: []string{
				"greeter.listUsers lists at most the requested number of users.",
				"limit specifies the maximum number of users to return.",
				`<th>Description</th>`,
				`value="/v1/users?limit=1"`,
				`https://api.example.test/v1/users?limit=1`,
				"Single query parameter",
			},
		},
		"type": {
			method: http.MethodGet,
			path:   "/types/User",
			status: http.StatusOK,
			contains: []string{
				"user represents a user known to the greeter.",
				"Declaration",
				"JSON",
				"Records use schema field names as JSON keys.",
				"Used by",
				`<span class="field-presence required">Required</span>`,
				`<span class="field-presence optional">Optional</span>`,
				`<span class="primitive">string</span>?`,
				stylesheet,
				script,
				`href="../types/UserID"`,
				`href="../methods/greeter.getUser"`,
			},
			notContains: []string{
				`opt string`,
			},
		},
		"sum type": {
			method: http.MethodGet,
			path:   "/types/Role",
			status: http.StatusOK,
			contains: []string{
				"JSON",
				`The <code>type</code> field contains the constructor name.`,
				`&#34;type&#34;: &#34;admin&#34;`,
				`&#34;level&#34;: 1`,
			},
			notContains: []string{
				"@type",
			},
		},
		"missing method": {
			method: http.MethodGet,
			path:   "/methods/greeter.missing",
			status: http.StatusNotFound,
		},
		"missing type": {
			method: http.MethodGet,
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
			request := httptest.NewRequest(tc.method, tc.path, nil)
			request.Host = "api.example.test"
			request.Header.Set("X-Forwarded-Proto", "https")

			response := browser.serve(t, request)
			if response.Code != tc.status {
				t.Fatalf("response status = %d, want %d", response.Code, tc.status)
			}
			body := response.Body.String()
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
			path := "/" + browser.server.StaticHashName(name)
			response := browser.serve(t, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusOK || response.Body.Len() == 0 {
				t.Errorf("GET %s = %d, %d bytes", path, response.Code, response.Body.Len())
			}
		}
	})
}

func TestBrowserMarkdown(t *testing.T) {
	browser := newBrowser(t, &greeter.Schema, "/reference")
	request := httptest.NewRequest(http.MethodGet, "/reference/index.md", nil)
	request.Host = "api.example.test"
	request.Header.Set("X-Forwarded-Proto", "https")

	response := browser.serve(t, request)
	if response.Code != http.StatusOK {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := response.Header().Get("Content-Disposition"); got != `attachment; filename="greeter-api.md"` {
		t.Errorf("Content-Disposition = %q", got)
	}
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
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("Markdown does not contain %q", want)
		}
	}

	head := browser.serve(t, httptest.NewRequest(http.MethodHead, "/reference/index.md", nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Errorf("HEAD response = %d %q", head.Code, head.Body.String())
	}
}

func TestBrowserWithoutHTTPBindingsStaysPlugOnly(t *testing.T) {
	schema := plugimpl.CopySchema(&greeter.Schema)
	for index := range schema.Methods {
		schema.Methods[index].HTTP = nil
	}
	browser := newBrowser(t, schema, "")
	for _, path := range []string{"/", "/types/User"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)

		response := browser.serve(t, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d", path, response.Code, http.StatusOK)
		}
		body := response.Body.String()
		for _, unexpected := range []string{`href="./#http"`, `id="http"`, `<h2>JSON</h2>`, "HTTP routes</span>"} {
			if strings.Contains(body, unexpected) {
				t.Errorf("GET %s unexpectedly contains %q", path, unexpected)
			}
		}
	}
}

func TestBrowserGroupsUncategorizedMethods(t *testing.T) {
	schema := plugimpl.CopySchema(&greeter.Schema)
	schema.Categories = []plug.CategoryInfo{
		{
			Name:          "User lookup",
			Documentation: "Methods for retrieving individual users.",
			Methods:       []string{"greeter.getUser"},
		},
	}
	browser := newBrowser(t, schema, "")
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	response := browser.serve(t, request)
	if response.Code != http.StatusOK {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusOK)
	}
	for _, value := range []string{"User lookup", "Methods for retrieving individual users.", "Other methods", "greeter.listUsers", "greeter.ping"} {
		if !strings.Contains(response.Body.String(), value) {
			t.Errorf("browser does not contain %q", value)
		}
	}
}

func TestBrowserGroupsUncategorizedTypes(t *testing.T) {
	schema := plugimpl.CopySchema(&greeter.Schema)
	schema.Categories = []plug.CategoryInfo{
		{
			Name:          "Identifiers",
			Documentation: "Scalar identifiers used by the service.",
			Types:         []string{"UserID"},
		},
	}
	browser := newBrowser(t, schema, "")
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	response := browser.serve(t, request)
	if response.Code != http.StatusOK {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusOK)
	}
	for _, value := range []string{"Identifiers", "Scalar identifiers used by the service.", "Other types", "LookupError", "User"} {
		if !strings.Contains(response.Body.String(), value) {
			t.Errorf("browser does not contain %q", value)
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
			schema := plugimpl.CopySchema(&greeter.Schema)
			schema.Categories = tc.categories
			_, err := browser.New(schema)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestBrowserOptionalParameterUsesSchemaSyntax(t *testing.T) {
	schema := plugimpl.CopySchema(&greeter.Schema)
	for methodIndex := range schema.Methods {
		if schema.Methods[methodIndex].Name == "greeter.listUsers" {
			schema.Methods[methodIndex].Fields[0].Type.Optional = true
		}
	}
	browser := newBrowser(t, schema, "")
	request := httptest.NewRequest(http.MethodGet, "/methods/greeter.listUsers", nil)

	response := browser.serve(t, request)
	if response.Code != http.StatusOK {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	if !strings.Contains(body, `<span class="primitive">u32</span>?`) {
		t.Errorf("browser does not use optional schema syntax")
	}
	if strings.Contains(body, "opt u32") {
		t.Errorf("browser contains runtime optional syntax")
	}
}

func TestBrowserTypeWithoutFieldDescriptionsOmitsDescriptionColumn(t *testing.T) {
	schema := plugimpl.CopySchema(&greeter.Schema)
	for typeIndex := range schema.Types {
		if schema.Types[typeIndex].Name != "User" {
			continue
		}
		for constructorIndex := range schema.Types[typeIndex].Constructors {
			for fieldIndex := range schema.Types[typeIndex].Constructors[constructorIndex].Fields {
				schema.Types[typeIndex].Constructors[constructorIndex].Fields[fieldIndex].Documentation = ""
			}
		}
	}
	browser := newBrowser(t, schema, "")
	request := httptest.NewRequest(http.MethodGet, "/types/User", nil)

	response := browser.serve(t, request)
	if response.Code != http.StatusOK {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusOK)
	}
	if strings.Contains(response.Body.String(), `<th>Description</th>`) {
		t.Error("browser type table contains a description column")
	}
}

func TestBrowserMountPrefix(t *testing.T) {
	browser := newBrowser(t, &greeter.Schema, "/reference")
	request := httptest.NewRequest(http.MethodGet, "/reference/methods/greeter.getUser", nil)

	response := browser.serve(t, request)
	if response.Code != http.StatusOK {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), `href="../types/User"`) {
		t.Fatalf("mounted browser does not contain a prefix-relative type link")
	}
}

type browserClient struct {
	server *web.Server
	client *http.Client
}

func newBrowser(t *testing.T, schema *plug.Schema, prefix string) *browserClient {
	t.Helper()
	h, err := browser.New(schema)
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
		StaticFS: browser.StaticFS(),
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
