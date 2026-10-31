// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"strings"
	"testing"
)

func TestGeneratedDeclarations(t *testing.T) {
	for _, name := range []string{"Schema", "Client", "Handlers", "NewClient", "RegisterHandlers"} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse("collision.plug", []byte("package test\ntype "+name+" = string\n"))
			if err == nil || !strings.Contains(err.Error(), "generated Go declaration") {
				t.Fatalf("Parse() = %v; want generated Go declaration collision", err)
			}
		})
	}
	for name, source := range map[string]string{
		"raw function": `package test; type OpenTunnel = string; test.tunnel -> raw public`,
		"request":      `package test; type EchoRequest = string; test.echo value: string -> string public`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse("collision.plug", []byte(source))
			if err == nil || !strings.Contains(err.Error(), "generated Go declaration") {
				t.Fatalf("Parse() = %v; want generated Go declaration collision", err)
			}
		})
	}
}

func TestSeparateGoNames(t *testing.T) {
	for name, source := range map[string]string{
		"raw close":         `package test; test.close -> raw public`,
		"type Close":        `package test; type Close = string; test.echo value: Close -> Close public`,
		"method and type":   `package test; type Value = string; test.value -> Value public`,
		"unused request":    `package test; type EchoRequest = string; test.echo -> EchoRequest public`,
		"method and client": `package test; test.client -> void public`,
		"raw and type":      `package test; type Close = string; test.close -> raw public`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse("close.plug", []byte(source)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHTTPPatterns(t *testing.T) {
	cases := map[string]struct {
		bindings string
		wantErr  bool
	}{
		"partial wildcard": {
			`http GET "/users/prefix{id}"`,
			true,
		},
		"equivalent": {
			`http GET "/users/{id}" http GET "/users/{name}"`,
			true,
		},
		"overlap": {
			`http GET "/users/{id}" http GET "/{name}/settings"`,
			true,
		},
		"specific route": {
			`http GET "/users/{id}" http GET "/users/settings"`,
			false,
		},
		"different verbs": {
			`http GET "/users/{id}" http POST "/users/{name}"`,
			false,
		},
		"trailing wildcard": {
			`http GET "/users/{id}/{name...}"`,
			false,
		},
		"middle wildcard": {
			`http GET "/users/{id...}/settings"`,
			true,
		},
		"duplicate wildcard": {
			`http GET "/users/{id}/{id...}"`,
			true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			source := "package test; test.get id: string name: string -> void public " + tc.bindings
			_, err := Parse("routes.plug", []byte(source))
			if (err != nil) != tc.wantErr {
				t.Fatalf("Parse() = %v; want error %v", err, tc.wantErr)
			}
		})
	}
}

func TestHTTPWildcardTypes(t *testing.T) {
	for name, tc := range map[string]struct {
		declarations string
		field        string
		wantErr      bool
	}{
		"string": {
			field: "string",
		},
		"optional string": {
			field: "string?",
		},
		"newtype": {
			declarations: "type Path = string",
			field:        "Path?",
		},
		"integer": {
			field:   "u32",
			wantErr: true,
		},
		"bytes": {
			field:   "bytes",
			wantErr: true,
		},
		"list": {
			field:   "list<string>",
			wantErr: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			source := "package test\n" + tc.declarations + "\ntest.browse path: " + tc.field + " -> void public http GET \"/contents/{path...}\""
			_, err := Parse("wildcard.plug", []byte(source))
			if (err != nil) != tc.wantErr {
				t.Fatalf("Parse() = %v; want error %v", err, tc.wantErr)
			}
		})
	}
}
