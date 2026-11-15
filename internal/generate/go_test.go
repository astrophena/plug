// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"fmt"
	"strings"
	"testing"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/plugimpl"
)

func TestGoNames(t *testing.T) {
	s := mustParse(t, `package test
type APIError
sqlError message: string = APIError
otherError = APIError
result httpPath: string = Result
test.run -> Result throws APIError
`)
	src, err := Go(s)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"type SQLError struct", "HTTPPath string"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("generated source does not contain %q", want)
		}
	}
}

func TestGoError(t *testing.T) {
	s := mustParse(t, `package test
failed reason: string = Failure
test.ping -> void throws Failure
`)
	src, err := Go(s)
	if err != nil {
		t.Fatal(err)
	}

	for _, unwanted := range []string{"UnknownFailure", `"bytes"`} {
		if strings.Contains(string(src), unwanted) {
			t.Errorf("generated a sum or an unused import: %q", unwanted)
		}
	}
	if !strings.Contains(string(src), `dec.UnknownConstructor("Failure", id)`) {
		t.Fatal("single-constructor error does not reject unknown constructors")
	}
}

func BenchmarkEncodeFields(b *testing.B) {
	for _, n := range []int{8, 64, 512} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			fields := []plug.FieldInfo{
				{
					Name: "id",
					Type: plugimpl.PrimitiveType("u32"),
				},
			}
			for i := range n {
				fields = append(fields, plug.FieldInfo{
					Name: fmt.Sprintf("field%d", i),
					Type: plugimpl.OptionalType(plugimpl.PrimitiveType("bool")),
				})
			}

			b.ReportAllocs()
			for b.Loop() {
				var g generator
				g.writeEncodeFields("", fields)
			}
		})
	}
}
