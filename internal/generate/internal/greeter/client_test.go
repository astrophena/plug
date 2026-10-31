// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package greeter

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientLiveCalls(t *testing.T) {
	var gotHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Custom-Header")
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusNoContent)
		case "/v1/users":
			if r.URL.Query().Get("limit") != "10" {
				http.Error(w, "limit=10 required", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.MarshalWrite(w, []User{
				{
					ID:   42,
					Name: "Alice",
					Role: &Admin{
						Level: 9,
					},
				},
			})
		case "/v1/users/42":
			w.Header().Set("Content-Type", "application/json")
			json.MarshalWrite(w, User{
				ID:   42,
				Name: "Alice",
				Role: &Admin{
					Level: 9,
				},
			})
		case "/v1/users/1":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.MarshalWrite(w, map[string]any{
				"code":    "notFound",
				"message": "notFound: id=1",
				"details": map[string]any{"id": 1},
			})
		case "/v1/users/999":
			http.Error(w, "internal crash", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := NewClient(ts.URL, WithHeader("X-Custom-Header", "my-value"), WithHTTPClient(http.DefaultClient))
	ctx := t.Context()

	if err := c.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
	if gotHeader != "my-value" {
		t.Fatalf("got X-Custom-Header %q, want %q", gotHeader, "my-value")
	}

	user, err := c.GetUser(ctx, GetUserRequest{
		ID: 42,
	})
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if user.ID != 42 || user.Name != "Alice" {
		t.Fatalf("got user %+v, want ID 42 and Name Alice", user)
	}
	admin, ok := user.Role.(*Admin)
	if !ok || admin.Level != 9 {
		t.Fatalf("got role %+v, want *Admin with Level 9", user.Role)
	}

	users, err := c.ListUsers(ctx, ListUsersRequest{
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListUsers failed: %v", err)
	}
	if len(users) != 1 || users[0].Name != "Alice" {
		t.Fatalf("got users %+v, want 1 user Alice", users)
	}

	_, err = c.GetUser(ctx, GetUserRequest{
		ID: 1,
	})
	if err == nil {
		t.Fatal("got nil, want error")
	}
	var notFound *NotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("got error %T: %v, want *NotFound", err, err)
	}
	if notFound.ID != 1 {
		t.Fatalf("got notFound.ID = %d, want 1", notFound.ID)
	}

	_, err = c.GetUser(ctx, GetUserRequest{
		ID: 999,
	})
	if err == nil {
		t.Fatal("got nil, want error for 999")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("got error %T: %v, want *HTTPError", err, err)
	}
	if httpErr.StatusCode != 500 {
		t.Fatalf("got status %d, want 500", httpErr.StatusCode)
	}
}
