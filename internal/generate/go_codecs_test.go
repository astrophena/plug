// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"fmt"
	"testing"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/plugimpl"
)

func BenchmarkEncodeFields(b *testing.B) {
	for _, n := range []int{8, 64, 512} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			fields := optionalFields(n)
			b.ReportAllocs()
			for b.Loop() {
				var g generator
				g.writeEncodeFields("", fields)
			}
		})
	}
}

func optionalFields(n int) []plug.FieldInfo {
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
	return fields
}
