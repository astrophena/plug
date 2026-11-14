// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plughttp_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/plug"
	"go.astrophena.name/plug/example/greeter"
	"go.astrophena.name/plug/plughttp"
)

func TestAuthentication(t *testing.T) {
	srv := service(t, plug.ServerOptions{})
	cases := map[string]struct {
		path         string
		authenticate func(*http.Request) (plug.Principal, error)
		status       int
		contains     string
	}{
		"application error": {
			path:         "/v1/users/1",
			authenticate: principal("allowed"),
			status:       http.StatusNotFound,
			contains:     `"code":"notFound"`,
		},
		"authorized": {
			path:         "/v1/users/42",
			authenticate: principal("allowed"),
			status:       http.StatusOK,
			contains:     `"type":"admin"`,
		},
		"denied": {
			path:         "/v1/users/42",
			authenticate: principal("denied"),
			status:       http.StatusForbidden,
			contains:     `"code":"permission_denied"`,
		},
		"unauthenticated": {
			path:     "/v1/users/42",
			status:   http.StatusUnauthorized,
			contains: `"code":"unauthenticated"`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h, err := plughttp.New(srv, greeter.HTTPRoutes(srv), plughttp.Options{
				Authenticate: tc.authenticate,
			})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			testutil.AssertEqual(t, w.Code, tc.status)
			if !strings.Contains(w.Body.String(), tc.contains) {
				t.Fatalf("response = %s; want %q", w.Body.String(), tc.contains)
			}
			if tc.status != http.StatusOK {
				return
			}

			var user greeter.User
			if err := user.UnmarshalJSON(w.Body.Bytes()); err != nil {
				t.Fatal(err)
			}
			role, ok := user.Role.(*greeter.Admin)
			if !ok {
				t.Fatalf("role = %T; want *greeter.Admin", user.Role)
			}
			testutil.AssertEqual(t, user.ID, greeter.UserID(42))
			testutil.AssertEqual(t, role.Level, uint32(7))
		})
	}
}

func TestRequests(t *testing.T) {
	srv := service(t, plug.ServerOptions{})
	h, err := plughttp.New(srv, greeter.HTTPRoutes(srv), plughttp.Options{
		Authenticate: principal("allowed"),
	})
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		method string
		path   string
		body   string
		status int
		want   string
	}{
		"empty list": {
			method: http.MethodGet,
			path:   "/v1/users?limit=0",
			status: http.StatusOK,
			want:   "[]\n",
		},
		"JSON body": {
			method: http.MethodPost,
			path:   "/v1/get-user",
			body:   `{"id":42}`,
			status: http.StatusOK,
			want:   `"id":42`,
		},
		"query overrides body": {
			method: http.MethodPost,
			path:   "/v1/get-user?id=42",
			body:   `{"id":2}`,
			status: http.StatusOK,
			want:   `"id":42`,
		},
		"path overrides query": {
			method: http.MethodGet,
			path:   "/v1/users/42?id=2",
			status: http.StatusOK,
			want:   `"id":42`,
		},
		"null body with query override": {
			method: http.MethodPost,
			path:   "/v1/get-user?id=42",
			body:   "null",
			status: http.StatusBadRequest,
		},
		"duplicate body field": {
			method: http.MethodPost,
			path:   "/v1/get-user?id=42",
			body:   `{"id":1,"id":42}`,
			status: http.StatusBadRequest,
		},
		"trailing JSON value": {
			method: http.MethodPost,
			path:   "/v1/get-user?id=42",
			body:   `{"id":42}{}`,
			status: http.StatusBadRequest,
		},
		"missing required field": {
			method: http.MethodPost,
			path:   "/v1/get-user",
			body:   `{}`,
			status: http.StatusBadRequest,
			want:   "field id is required",
		},
		"null required field": {
			method: http.MethodPost,
			path:   "/v1/get-user",
			body:   `{"id":null}`,
			status: http.StatusBadRequest,
			want:   "field id: value is required",
		},
		"unknown body field": {
			method: http.MethodPost,
			path:   "/v1/get-user",
			body:   `{"id":42,"unknown_field":"val"}`,
			status: http.StatusBadRequest,
			want:   "unknown field",
		},
		"unknown query parameter": {
			method: http.MethodGet,
			path:   "/v1/users?limit=10&unknown=val",
			status: http.StatusBadRequest,
			want:   "unknown query parameter",
		},
		"duplicate query parameter": {
			method: http.MethodGet,
			path:   "/v1/users?limit=10&limit=20",
			status: http.StatusBadRequest,
			want:   "duplicate parameter",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			testutil.AssertEqual(t, w.Code, tc.status)
			if !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("response = %s; want %q", w.Body.String(), tc.want)
			}
		})
	}
}

func TestBodyLimit(t *testing.T) {
	s := service(t, plug.ServerOptions{
		Limits: plug.Limits{
			MaxFrameSize: 64,
		},
	})
	routes := greeter.HTTPRoutes(s)

	const body = `{"id":42}`
	padded := strings.Repeat(" ", 64) + body
	cases := map[string]struct {
		body   string
		limit  int64
		status int
	}{
		"at limit": {
			body:   body,
			limit:  int64(len(body)),
			status: http.StatusOK,
		},
		"over limit": {
			body:   body,
			limit:  int64(len(body) - 1),
			status: http.StatusBadRequest,
		},
		"default frame limit": {
			body:   padded,
			status: http.StatusBadRequest,
		},
		"larger than frame limit": {
			body:   padded,
			limit:  int64(len(padded)),
			status: http.StatusOK,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h, err := plughttp.New(s, routes, plughttp.Options{
				Authenticate: principal("allowed"),
				MaxBodyBytes: tc.limit,
			})
			if err != nil {
				t.Fatal(err)
			}

			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/get-user", strings.NewReader(tc.body)))
			testutil.AssertEqual(t, w.Code, tc.status)
		})
	}
}

func TestConcurrentRequests(t *testing.T) {
	srv := service(t, plug.ServerOptions{})
	h, err := plughttp.New(srv, greeter.HTTPRoutes(srv), plughttp.Options{
		Authenticate: principal("allowed"),
	})
	if err != nil {
		t.Fatal(err)
	}

	for id := range 16 {
		t.Run(strconv.Itoa(id+10), func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/users/"+strconv.Itoa(id+10), nil))
			var user greeter.User
			if err := user.UnmarshalJSON(w.Body.Bytes()); err != nil {
				t.Fatal(err)
			}
			testutil.AssertEqual(t, w.Code, http.StatusOK)
			testutil.AssertEqual(t, user.ID, greeter.UserID(id+10))
		})
	}
}

func service(t *testing.T, opts plug.ServerOptions) *plug.Server {
	t.Helper()
	if opts.Security.Authorize == nil {
		opts.Security.Authorize = func(_ context.Context, p plug.Principal, permission string, _ plug.MethodInfo, _ any) error {
			if p.Subject != "allowed" || permission != greeter.PermissionUserRead {
				return errors.New("access denied")
			}
			return nil
		}
	}
	s, err := plug.NewServer(&greeter.Schema, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err := greeter.RegisterHandlers(s, greeter.Handlers{
		GetUser: func(_ context.Context, req greeter.GetUserRequest) (greeter.User, error) {
			if req.ID == 1 {
				return greeter.User{}, &greeter.NotFound{
					ID: req.ID,
				}
			}
			return greeter.User{
				ID:     req.ID,
				Name:   "Ada",
				Labels: []string{"test"},
				Role: &greeter.Admin{
					Level: 7,
				},
			}, nil
		},
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) { return nil, nil },
		Ping:      func(context.Context) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func principal(subject string) func(*http.Request) (plug.Principal, error) {
	return func(*http.Request) (plug.Principal, error) {
		return plug.Principal{
			Subject: subject,
		}, nil
	}
}
