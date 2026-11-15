// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
)

func TestValidation(t *testing.T) {
	cases := map[string]struct {
		src  string
		want string
	}{
		"duplicate field": {
			`item id: i64 id: string = Item`,
			"duplicate field",
		},
		"error response": {
			`failed = Failure; x.get -> Failure throws Failure`,
			"cannot be used as a response",
		},
		"non-scalar newtype": {
			`type ID = void`,
			"must use a scalar primitive",
		},
		"raw field": {
			`item stream: raw = Item`,
			"raw is only valid",
		},
		"raw HTTP": {
			`x.open -> raw http GET "/open"`,
			"cannot have an HTTP binding",
		},
		"primitive name": {
			`type string = i64`,
			"reserved for a primitive",
		},
		"error field": {
			`failed error: string = Failure; x.get -> void throws Failure`,
			"generated Error method",
		},
		"optional list element": {
			`item labels: list<string?> = Item`,
			"list elements cannot be optional",
		},
		"recursive type": {
			`item children: list<Item> = Item`,
			"recursive type cycle",
		},
		"unknown type": {
			`item id: Missing = Item`,
			"unknown type",
		},
		"generated name": {
			`type Schema = string`,
			"generated Go declaration",
		},
		"client Close": {
			`x.close -> void`,
			"generated Client.Close",
		},
		"raw function": {
			`type OpenTunnel = string; x.tunnel -> raw`,
			"generated Go declaration",
		},
		"request type": {
			`type EchoRequest = string; x.echo value: string -> string`,
			"generated Go declaration",
		},
		"qualified method": {
			`x.admin.get -> void`,
			"form group.name",
		},
		"Go method name": {
			`x.get -> void; y.get -> void`,
			"generated Go declaration",
		},
		"required after optional": {
			`item note: string? id: i64 = Item`,
			"follows an optional extension field",
		},
		"undeclared permission": {
			`x.get -> void requires item.read`,
			"undeclared permission",
		},
		"non-scalar path": {
			`item id: i64 = Item; x.get item: Item -> void http GET "/items/{item}"`,
			"must be a scalar field",
		},
		"HTTP verb": {
			`x.get -> void http CONNECT "/items"`,
			"unsupported HTTP verb",
		},
		"status on response": {
			`item id: i64 = Item httpStatus 400; x.get -> Item`,
			"only valid on declared error constructors",
		},
		"empty category": {
			`category ""; x.ping -> void`,
			"category name is empty",
		},
		"padded category": {
			`category " Items "; x.get -> void`,
			"leading or trailing whitespace",
		},
		"unused category": {
			`category "Items"`,
			"contains no types or methods",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse("schema.plug", []byte("package x\n"+tc.src))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse() = %v; want %q", err, tc.want)
			}
		})
	}
}

func TestGoNames(t *testing.T) {
	cases := map[string]string{
		"raw Close":      `type Close = string; x.close -> raw`,
		"type":           `type Value = string; x.value -> Value`,
		"unused request": `type EchoRequest = string; x.echo -> EchoRequest`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			mustParse(t, "package x\n"+src)
		})
	}
}

func TestHTTPBindings(t *testing.T) {
	cases := map[string]struct {
		src string
		ok  bool
	}{
		"partial wildcard": {
			`x.get id: string -> void http GET "/items/prefix{id}"`,
			false,
		},
		"conflict": {
			`x.get id: string name: string -> void http GET "/items/{id}" http GET "/items/{name}"`,
			false,
		},
		"specific path and different verbs": {
			`x.get id: string -> void http GET "/items/{id}" http GET "/items/latest" http POST "/items/{id}"`,
			true,
		},
		"wildcard newtype": {
			`type Path = string; x.get path: Path? -> void http GET "/items/{path...}"`,
			true,
		},
		"wildcard integer": {
			`x.get path: u32 -> void http GET "/items/{path...}"`,
			false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse("schema.plug", []byte("package x\n"+tc.src))
			testutil.AssertEqual(t, err == nil, tc.ok)
		})
	}
}
