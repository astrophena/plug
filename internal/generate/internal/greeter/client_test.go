// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package greeter

//go:generate go tool plug generate -http-client -o client.go ../../../../example/greeter/schema.plug

import (
	"errors"
	"io"
	"net/http"
	"testing"

	"go.astrophena.name/base/testutil"
)

func TestClient(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		testutil.AssertEqual(t, r.Header.Get("X-Test"), "test")
		w.Header().Set("Content-Type", "application/json")

		const user = `{"id":42,"name":"Ada","role":{"type":"admin","level":9}}`
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusNoContent)
		case "/v1/users":
			testutil.AssertEqual(t, r.URL.Query().Get("limit"), "10")
			io.WriteString(w, "["+user+"]")
		case "/v1/users/42":
			io.WriteString(w, user)
		case "/v1/users/1":
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"code":"notFound","message":"missing","details":{"id":1}}`)
		default:
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	})
	c := NewClient("https://example.test", WithHeader("X-Test", "test"), WithHTTPClient(testutil.MockHTTPClient(h)))
	ctx := t.Context()

	if err := c.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	user, err := c.GetUser(ctx, GetUserRequest{
		ID: 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, user.ID, UserID(42))
	testutil.AssertEqual(t, user.Role, Role(&Admin{
		Level: 9,
	}))

	users, err := c.ListUsers(ctx, ListUsersRequest{
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, users, []User{user})

	_, err = c.GetUser(ctx, GetUserRequest{
		ID: 1,
	})
	missing, ok := errors.AsType[*NotFound](err)
	if !ok {
		t.Fatalf("GetUser() = %v; want *NotFound", err)
	}
	testutil.AssertEqual(t, missing.ID, UserID(1))

	_, err = c.GetUser(ctx, GetUserRequest{
		ID: 999,
	})
	httpErr, ok := errors.AsType[*HTTPError](err)
	if !ok {
		t.Fatalf("GetUser() = %v; want *HTTPError", err)
	}
	testutil.AssertEqual(t, httpErr.StatusCode, http.StatusInternalServerError)
}
