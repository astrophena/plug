// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plughttp_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/example/greeter"
	"go.astrophena.name/plug/plughttp"
)

func TestNew(t *testing.T) {
	cases := map[string]struct {
		server func(*testing.T) *plug.Server
		routes func(*plug.Server) []plughttp.Route
		opts   plughttp.Options
		want   string
	}{
		"nil server": {
			server: func(*testing.T) *plug.Server { return nil },
			want:   "server is nil",
		},
		"negative body limit": {
			opts: plughttp.Options{
				MaxBodyBytes: -1,
			},
			want: "MaxBodyBytes cannot be negative",
		},
		"missing handlers": {
			server: func(t *testing.T) *plug.Server {
				s, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{})
				if err != nil {
					t.Fatal(err)
				}
				return s
			},
			want: "missing handlers",
		},
		"empty route": {
			routes: func(*plug.Server) []plughttp.Route { return []plughttp.Route{{}} },
			want:   "has no handler",
		},
		"different server": {
			routes: func(*plug.Server) []plughttp.Route {
				return greeter.HTTPRoutes(newGreeterServer(t))
			},
			want: "belongs to a different server",
		},
		"duplicate route": {
			routes: func(s *plug.Server) []plughttp.Route {
				r := greeter.HTTPRoutes(s)[0]
				return []plughttp.Route{r, r}
			},
			want: "invalid route",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			newServer := tc.server
			if newServer == nil {
				newServer = newGreeterServer
			}
			s := newServer(t)
			var routes []plughttp.Route
			if tc.routes != nil {
				routes = tc.routes(s)
			}

			_, err := plughttp.New(s, routes, tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("New() error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestHTTPRoutes(t *testing.T) {
	server := newGreeterServer(t)
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
			handler, err := plughttp.New(server, greeter.HTTPRoutes(server), plughttp.Options{
				Authenticate: tc.authenticate,
			})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodGet, tc.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.status || !strings.Contains(response.Body.String(), tc.contains) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestHTTPJSONRoundTrip(t *testing.T) {
	server := newGreeterServer(t)
	handler, err := plughttp.New(server, greeter.HTTPRoutes(server), plughttp.Options{
		Authenticate: principal("allowed"),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/users/42", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var user greeter.User
	if err := user.UnmarshalJSON(response.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	role, ok := user.Role.(*greeter.Admin)
	if user.ID != 42 || !ok || role.Level != 7 {
		t.Fatalf("user = %#v", user)
	}
}

func TestHTTPRequests(t *testing.T) {
	server := newGreeterServer(t)
	handler, err := plughttp.New(server, greeter.HTTPRoutes(server), plughttp.Options{
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
			handler.ServeHTTP(w, r)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("response = %d %s; want %d containing %q", w.Code, w.Body.String(), tc.status, tc.want)
			}
		})
	}
}

func TestHTTPEmptyList(t *testing.T) {
	server := newGreeterServer(t)
	handler, err := plughttp.New(server, greeter.HTTPRoutes(server), plughttp.Options{
		Authenticate: principal("allowed"),
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/users?limit=0", nil))
	if response.Code != http.StatusOK || response.Body.String() != "[]\n" {
		t.Fatalf("response = %d %s; want 200 []", response.Code, response.Body.String())
	}
}

func TestHTTPBodyLimit(t *testing.T) {
	s := newGreeterServer(t)
	routes := greeter.HTTPRoutes(s)
	cases := map[string]struct {
		limit  int64
		status int
	}{
		"default": {
			status: http.StatusOK,
		},
		"at limit": {
			limit:  9,
			status: http.StatusOK,
		},
		"over limit": {
			limit:  8,
			status: http.StatusBadRequest,
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

			r := httptest.NewRequest(http.MethodPost, "/v1/get-user", strings.NewReader(`{"id":42}`))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("response = %d %s, want %d", w.Code, w.Body.String(), tc.status)
			}
		})
	}
}

func TestHTTPBodyLimitOverride(t *testing.T) {
	s, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{
		Limits: plug.Limits{
			MaxFrameSize: 64,
		},
		Security: plug.Security{
			Authorize: func(context.Context, plug.Principal, string, plug.MethodInfo, any) error { return nil },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := greeter.RegisterHandlers(s, greeter.Handlers{
		GetUser: func(context.Context, greeter.GetUserRequest) (greeter.User, error) {
			return greeter.User{
				Role: &greeter.Guest{},
			}, nil
		},
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) { return nil, nil },
		Ping:      func(context.Context) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	routes := greeter.HTTPRoutes(s)
	body := strings.Repeat(" ", 64) + `{"id":42}`
	for name, limit := range map[string]int64{"default": 0, "larger than frame limit": int64(len(body))} {
		t.Run(name, func(t *testing.T) {
			h, err := plughttp.New(s, routes, plughttp.Options{
				Authenticate: principal("allowed"),
				MaxBodyBytes: limit,
			})
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/get-user", strings.NewReader(body)))
			want := http.StatusOK
			if limit == 0 {
				want = http.StatusBadRequest
			}
			if w.Code != want {
				t.Fatalf("response = %d %s; want %d", w.Code, w.Body.String(), want)
			}
		})
	}
}

func TestHTTPShutdown(t *testing.T) {
	s, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{
		Security: plug.Security{
			Authorize: func(context.Context, plug.Principal, string, plug.MethodInfo, any) error { return nil },
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	var (
		started  = make(chan struct{})
		canceled = make(chan struct{})
		release  = make(chan struct{})
		calls    atomic.Int32
	)
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	if err := greeter.RegisterHandlers(s, greeter.Handlers{
		GetUser:   func(context.Context, greeter.GetUserRequest) (greeter.User, error) { return greeter.User{}, nil },
		ListUsers: func(context.Context, greeter.ListUsersRequest) ([]greeter.User, error) { return nil, nil },
		Ping: func(ctx context.Context) error {
			calls.Add(1)
			close(started)
			<-ctx.Done()
			close(canceled)
			<-release
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	h, err := plughttp.New(s, greeter.HTTPRoutes(s), plughttp.Options{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		done <- w
	}()
	<-started

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Shutdown() = %v; want context.Canceled while the handler is active", err)
	}
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not cancel the HTTP handler")
	}
	if err := s.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("second Shutdown() = %v; want context.Canceled while the handler is active", err)
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != http.StatusInternalServerError || calls.Load() != 1 {
		t.Fatalf("request after shutdown = %d, %d handler calls; want 500, 1", w.Code, calls.Load())
	}

	close(release)
	if err := s.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if w := <-done; w.Code != http.StatusNoContent {
		t.Fatalf("active request = %d %s; want 204", w.Code, w.Body.String())
	}
}

func newGreeterServer(t *testing.T) *plug.Server {
	t.Helper()
	server, err := plug.NewServer(&greeter.Schema, plug.ServerOptions{
		Security: plug.Security{
			Authorize: func(_ context.Context, principal plug.Principal, permission string, _ plug.MethodInfo, _ any) error {
				if principal.Subject != "allowed" || permission != greeter.PermissionUserRead {
					return errors.New("access denied")
				}
				return nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := greeter.RegisterHandlers(server, greeter.Handlers{
		GetUser: func(_ context.Context, request greeter.GetUserRequest) (greeter.User, error) {
			if request.ID == 1 {
				return greeter.User{}, &greeter.NotFound{
					ID: request.ID,
				}
			}
			return greeter.User{
				ID:     request.ID,
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
	return server
}

func principal(subject string) func(*http.Request) (plug.Principal, error) {
	return func(*http.Request) (plug.Principal, error) {
		return plug.Principal{
			Subject: subject,
		}, nil
	}
}
