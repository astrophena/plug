// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plughttp

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/synctest"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/plug"
	"go.astrophena.name/plug/plugimpl"
)

func TestRoutes(t *testing.T) {
	for name, paths := range map[string][]string{
		"invalid":    {"/users/prefix{id}"},
		"equivalent": {"/users/{id}", "/users/{name}"},
		"overlap":    {"/users/{id}", "/{name}/settings"},
		"duplicate":  {"/ping", "/ping"},
	} {
		t.Run(name, func(t *testing.T) {
			s, routes := routeServer(t, paths, nil)
			if _, err := New(s, routes, Options{}); err == nil {
				t.Fatal("accepted invalid or conflicting routes")
			}
		})
	}

	s, routes := routeServer(t, []string{"/ping"}, nil)
	_, other := routeServer(t, []string{"/ping"}, nil)
	for name, tc := range map[string]struct {
		routes []Route
		opts   Options
	}{
		"empty route": {
			routes: []Route{{}},
		},
		"foreign server": {
			routes: other,
		},
		"negative body limit": {
			routes: routes,
			opts: Options{
				MaxBodyBytes: -1,
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(s, tc.routes, tc.opts); err == nil {
				t.Fatal("accepted invalid route configuration")
			}
		})
	}
}

func TestEncodeError(t *testing.T) {
	binding := Binding{
		Verb: http.MethodGet,
		Path: "/value",
	}
	sch := &plug.Schema{
		Package: "buffered",
		Major:   1,
		Family:  plugimpl.FamilyFingerprint("buffered", 1),
		Source:  "package buffered version 1\n",
		Methods: []plug.MethodInfo{
			{
				ID:       1,
				Name:     "buffered.value",
				Public:   true,
				Response: plugimpl.PrimitiveType("string"),
				HTTP:     []Binding{binding},
			},
		},
	}
	s, err := plug.NewServer(sch, plug.ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	m := plugimpl.NewMethod(sch, 1, plugimpl.VoidCodec, plugimpl.Codec[jsonValue]{}, plugimpl.ErrorCodec{})
	var (
		value jsonValue
		batch []plugimpl.Registration
	)
	if err := plugimpl.AppendHandler(&batch, m, func(context.Context, plugimpl.Void) (jsonValue, error) { return value, nil }, "Value"); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterHandlers(batch); err != nil {
		t.Fatal(err)
	}
	h, err := New(s, []Route{NewRoute(s, m, binding, nil)}, Options{})
	if err != nil {
		t.Fatal(err)
	}

	// Later responses must not contain bytes from a failed or earlier response.
	for _, tc := range []struct {
		value  jsonValue
		status int
		body   string
	}{
		{
			"first",
			http.StatusOK,
			"\"first\"\n",
		},
		{
			"invalid JSON",
			http.StatusInternalServerError,
			"{\"code\":\"internal\",\"message\":\"internal server error\"}\n",
		},
		{
			"after",
			http.StatusOK,
			"\"after\"\n",
		},
	} {
		value = tc.value
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/value", nil))
		testutil.AssertEqual(t, w.Code, tc.status)
		testutil.AssertEqual(t, w.Body.String(), tc.body)
		testutil.AssertEqual(t, w.Header().Get("Content-Type"), "application/json")
	}
}

func TestJSONSum(t *testing.T) {
	sch := &plug.Schema{
		Types: []plug.TypeInfo{
			{
				Name: "Role",
				Kind: plug.TypeSum,
				Constructors: []plug.ConstructorInfo{
					{
						Name: "guest",
					},
					{
						Name: "admin",
						Fields: []plug.FieldInfo{
							{
								Name: "level",
								Type: plugimpl.PrimitiveType("u32"),
							},
						},
					},
				},
			},
		},
	}
	cases := map[string]struct {
		body string
		ok   bool
	}{
		"fieldless": {
			`{"type":"guest"}`,
			true,
		},
		"fields": {
			`{"type":"admin","level":7}`,
			true,
		},
		"missing tag": {
			`{}`,
			false,
		},
		"numeric tag": {
			`{"type":1}`,
			false,
		},
		"unknown": {
			`{"type":"future"}`,
			false,
		},
		"missing field": {
			`{"type":"admin"}`,
			false,
		},
		"unknown field": {
			`{"type":"guest","level":7}`,
			false,
		},
		"null": {
			`null`,
			false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := validateJSONValue(sch, plugimpl.NamedType("Role"), jsontext.Value(tc.body))
			testutil.AssertEqual(t, err == nil, tc.ok)
		})
	}
}

func TestErrorDetails(t *testing.T) {
	for name, tc := range map[string]struct {
		details any
		want    string
	}{
		"protocol": {
			nil,
			`{"code":"oops","message":"oops"}` + "\n",
		},
		"declared": {
			struct{}{},
			`{"code":"oops","message":"oops","details":{}}` + "\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeError(w, http.StatusBadRequest, "oops", errors.New("oops"), tc.details)
			testutil.AssertEqual(t, w.Body.String(), tc.want)
		})
	}
}

func TestPathValues(t *testing.T) {
	sch := &plug.Schema{}
	m := plug.MethodInfo{
		Fields: []plug.FieldInfo{
			{
				Name: "repository",
				Type: plugimpl.PrimitiveType("string"),
			},
			{
				Name: "path",
				Type: plugimpl.OptionalType(plugimpl.PrimitiveType("string")),
			},
		},
	}
	binding := Binding{
		Verb: "GET",
		Path: "/repos/{repository}/contents/{path...}",
	}
	for name, path := range map[string]string{"root": "", "nested": "roles/git/README.md", "literal percent": "docs/a%2Fb"} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/?path=ignored", nil)
			r.SetPathValue("repository", "aur/nested/repo")
			r.SetPathValue("path", path)
			fields, err := readFields(r, sch, m, binding, 1024)
			if err != nil {
				t.Fatal(err)
			}

			var repo, got string
			if err := json.Unmarshal(fields["repository"], &repo); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(fields["path"], &got); err != nil {
				t.Fatal(err)
			}
			testutil.AssertEqual(t, repo, "aur/nested/repo")
			testutil.AssertEqual(t, got, path)
		})
	}
}

func TestShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			release  = make(chan struct{})
			unblock  = sync.OnceFunc(func() { close(release) })
			calls    int
			canceled bool
		)
		defer unblock()
		s, routes := routeServer(t, []string{"/ping"}, func(ctx context.Context) error {
			calls++
			<-ctx.Done()
			canceled = true
			<-release
			return nil
		})
		h, err := New(s, routes, Options{})
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan int, 1)

		go func() {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequestWithContext(s.Context(), "GET", "/ping", nil))
			done <- w.Code
		}()
		synctest.Wait()
		testutil.AssertEqual(t, calls, 1)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := s.Shutdown(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("Shutdown() = %v; handler is still active", err)
		}
		synctest.Wait()
		testutil.AssertEqual(t, canceled, true)

		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/ping", nil))
		testutil.AssertEqual(t, w.Code, http.StatusInternalServerError)
		testutil.AssertEqual(t, calls, 1)

		unblock()
		if err := s.Shutdown(t.Context()); err != nil {
			t.Fatal(err)
		}
		testutil.AssertEqual(t, <-done, http.StatusNoContent)
	})
}

type jsonValue string

func (v jsonValue) MarshalJSON() ([]byte, error) {
	if v == "invalid JSON" {
		return []byte(`"partial`), errors.New("private marshal error")
	}
	return json.Marshal(string(v))
}

func routeServer(t *testing.T, paths []string, h func(context.Context) error) (*plug.Server, []Route) {
	t.Helper()
	var bindings []Binding
	for _, path := range paths {
		bindings = append(bindings, Binding{
			Verb: "GET",
			Path: path,
		})
	}
	sch := &plug.Schema{
		Package: "test",
		Major:   1,
		Family:  plugimpl.FamilyFingerprint("test", 1),
		Source:  "package test version 1\ntest.call -> void public\n",
		Methods: []plug.MethodInfo{
			{
				ID:       1,
				Name:     "test.call",
				Public:   true,
				Response: plugimpl.PrimitiveType("void"),
				HTTP:     bindings,
			},
		},
	}
	s, err := plug.NewServer(sch, plug.ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if h == nil {
		h = func(context.Context) error { return nil }
	}
	var (
		m    = plugimpl.NewMethod(sch, 1, plugimpl.VoidCodec, plugimpl.VoidCodec, plugimpl.ErrorCodec{})
		regs []plugimpl.Registration
	)
	if err := plugimpl.AppendHandlerNoReqVoid(&regs, m, h, "Call"); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterHandlers(regs); err != nil {
		t.Fatal(err)
	}
	var routes []Route
	for _, binding := range bindings {
		routes = append(routes, NewRoute(s, m, binding, nil))
	}
	return s, routes
}
