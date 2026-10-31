// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plughttp

import (
	"encoding/json/jsontext"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.astrophena.name/plug"
)

func TestValidateJSONSum(t *testing.T) {
	schema := &plug.Schema{
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
								Type: plug.TypeExpr{
									Kind: plug.TypeExprPrimitive,
									Name: "u32",
								},
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
		"legacy tag": {
			`{"_":"guest"}`,
			false,
		},
		"both tags": {
			`{"type":"guest","_":"guest"}`,
			false,
		},
		"empty tag": {
			`{"type":""}`,
			false,
		},
		"null tag": {
			`{"type":null}`,
			false,
		},
		"numeric tag": {
			`{"type":1}`,
			false,
		},
		"tag case": {
			`{"Type":"guest"}`,
			false,
		},
		"constructor case": {
			`{"type":"Guest"}`,
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
		"string enum": {
			`"guest"`,
			false,
		},
		"null": {
			`null`,
			false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := validateJSONValue(schema, plug.TypeExpr{
				Kind: plug.TypeExprNamed,
				Name: "Role",
			}, jsontext.Value(tc.body))
			if (err == nil) != tc.ok {
				t.Fatalf("validateJSONValue(%s) = %v; valid = %v", tc.body, err, tc.ok)
			}
		})
	}
}

func TestErrorJSONDetails(t *testing.T) {
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
			response := httptest.NewRecorder()
			writeError(response, http.StatusBadRequest, "oops", errors.New("oops"), tc.details)
			if got := response.Body.String(); got != tc.want {
				t.Fatalf("error response = %s; want %s", got, tc.want)
			}
		})
	}
}

func TestWildcardPathValues(t *testing.T) {
	schema := &plug.Schema{}
	method := plug.MethodInfo{
		Fields: []plug.FieldInfo{
			{
				Name: "repository",
				Type: plug.TypeExpr{
					Kind: plug.TypeExprPrimitive,
					Name: "string",
				},
			},
			{
				Name: "path",
				Type: plug.TypeExpr{
					Kind:     plug.TypeExprPrimitive,
					Name:     "string",
					Optional: true,
				},
			},
		},
	}
	binding := Binding{
		Verb: "GET",
		Path: "/repos/{repository}/contents/{path...}",
	}
	for name, path := range map[string]string{"root": "", "nested": "roles/git/README.md", "literal percent": "docs/a%2Fb"} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/?path=ignored", nil)
			request.SetPathValue("repository", "aur/nested/repo")
			request.SetPathValue("path", path)
			var result struct {
				Repository string              `json:"repository"`
				Path       plug.Option[string] `json:"path"`
			}
			if err := decodeRequest(request, schema, method, binding, &result, 1024); err != nil {
				t.Fatal(err)
			}
			if result.Repository != "aur/nested/repo" || !result.Path.Present() || result.Path.OrZero() != path {
				t.Fatalf("decoded request = %#v", result)
			}
		})
	}
}
