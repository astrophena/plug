// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"os/exec"
	"testing"
)

func TestTypeScript(t *testing.T) {
	s := readSchema(t, "testdata/typescript/schema.plug")
	// Metadata from other tools can contain text rejected by the schema parser.
	s.Documentation = "literal docs\u2028throw new Error('comment executed');"
	for i := range s.Methods {
		if s.Methods[i].Name == "typescript.get" {
			s.Methods[i].HTTP[0].Path = "/`+(globalThis.literalExecuted=true)+`/${text}/\\/{id}"
		}
	}
	got, err := TypeScript(s)
	if err != nil {
		t.Fatal(err)
	}
	snapshot(t, "testdata/typescript/client.ts", got)

	deno, err := exec.LookPath("deno")
	if err != nil {
		t.Skip("deno is not installed")
	}

	// Stable paths let Deno reuse its type-check cache.
	cmd := exec.CommandContext(t.Context(), deno, "test", "--no-config", "--no-lock", "client_test.ts")
	cmd.Dir = "testdata/typescript"
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated TypeScript: %v\n%s", err, out)
	}
}
