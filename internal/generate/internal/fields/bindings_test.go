// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package fields

//go:generate go tool plug generate -o bindings.go schema.plug

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
	"go.astrophena.name/plug"
	"go.astrophena.name/plug/plughttp"
	"go.astrophena.name/plug/plugimpl"
)

func TestHTTP(t *testing.T) {
	var (
		saved = methodEcho
		codec = saved.Errors()
	)
	t.Cleanup(func() { methodEcho = saved })
	codec.Encode = nil
	codec.Decode = nil
	methodEcho = plugimpl.NewMethod(saved.Schema(), saved.ID(), saved.Request(), saved.Response(), codec)

	s, err := plug.NewServer(&Schema, plug.ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Shutdown(context.Background()) })

	var (
		got     EchoRequest
		callErr error
	)
	if err := RegisterHandlers(s, Handlers{
		Echo: func(_ context.Context, r EchoRequest) error {
			got = r
			return callErr
		},
	}); err != nil {
		t.Fatal(err)
	}
	h, err := plughttp.New(s, HTTPRoutes(s), plughttp.Options{})
	if err != nil {
		t.Fatal(err)
	}

	serve := func() *httptest.ResponseRecorder {
		var (
			r = httptest.NewRequest(http.MethodPost, "/echo/3?id=2", strings.NewReader(`{"id":1,"data":[[{"type":"one","label":"ok"}]]}`))
			w = httptest.NewRecorder()
		)
		h.ServeHTTP(w, r)
		return w
	}
	w := serve()
	testutil.AssertEqual(t, w.Code, http.StatusNoContent)
	testutil.AssertEqual(t, got.ID, uint32(3))
	testutil.AssertEqual(t, got.Data.OrZero(), [][]Choice{
		{
			&One{
				Label: "ok",
			},
		},
	})

	callErr = fmt.Errorf("private wrapper: %w", &Missing{
		Code: 42,
	})
	w = serve()
	testutil.AssertEqual(t, w.Code, http.StatusNotFound)
	if strings.Contains(w.Body.String(), "private wrapper") || !strings.Contains(w.Body.String(), `"details":{"type":"missing","code":42}`) {
		t.Fatalf("declared error = %s", w.Body.String())
	}

	var missing *Missing
	callErr = fmt.Errorf("wrapped nil: %w", missing)
	w = serve()
	testutil.AssertEqual(t, w.Code, http.StatusInternalServerError)
	if strings.Contains(w.Body.String(), "wrapped nil") {
		t.Fatalf("nil error = %s", w.Body.String())
	}
}
