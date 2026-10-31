// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"testing"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/internal/schema"
)

func parse(filename string, source []byte) (plug.Schema, error) {
	s, err := schema.Parse(filename, source)
	if err != nil {
		return plug.Schema{}, err
	}
	return schema.RuntimeSchema(s), nil
}

func mustParse(t *testing.T, source string) plug.Schema {
	t.Helper()
	s, err := parse("test.plug", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
