// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"strings"
	"testing"

	"go.astrophena.name/plug/plugimpl"
)

func TestNilRuntime(t *testing.T) {
	var method plugimpl.Method[plugimpl.Void, plugimpl.Void]
	cases := map[string]func() error{
		"call": func() error {
			_, err := Call(t.Context(), nil, method, plugimpl.Void{})
			return err
		},
		"invoke": func() error {
			_, err := Invoke(t.Context(), nil, method, plugimpl.Void{})
			return err
		},
		"seal": func() error { return (*Server)(nil).Seal() },
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			if err := run(); err == nil || !strings.Contains(err.Error(), "is nil") {
				t.Fatalf("got %v; want nil runtime error", err)
			}
		})
	}
}
