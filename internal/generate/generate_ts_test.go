// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTypeScriptErrors(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("testdata", "errors.plug"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := parse("errors.plug", source)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := TypeScript(s)
	if err != nil {
		t.Fatal(err)
	}
	runTypeScript(t, generated, "errors_test.ts")
}

func TestTypeScriptLiteralText(t *testing.T) {
	s, err := parse("literal.plug", []byte("package literal\nliteral.get id: string -> void public http GET \"/items/{id}\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the emission boundary, including metadata supplied by other tools.
	s.Documentation = "literal docs\u2028throw new Error('comment executed');"
	s.Methods[0].HTTP[0].Path = "/`+(globalThis.literalExecuted=true)+`/${text}/\\/{id}"
	generated, err := TypeScript(s)
	if err != nil {
		t.Fatal(err)
	}
	runTypeScript(t, generated, "literal_test.ts")
}

func TestTypeScriptWildcardPaths(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("testdata", "wildcard.plug"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := parse("wildcard.plug", source)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := TypeScript(s)
	if err != nil {
		t.Fatal(err)
	}
	runTypeScript(t, generated, "wildcard_test.ts")
}

func runTypeScript(t *testing.T, generated []byte, name string) {
	t.Helper()
	deno, err := exec.LookPath("deno")
	if err != nil {
		t.Skip("deno is not installed")
	}
	test, err := os.ReadFile(filepath.Join("testdata", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	for name, content := range map[string][]byte{"client.ts": generated, name: test} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.CommandContext(t.Context(), deno, "test", "--no-config", "--no-lock", name)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated TypeScript: %v\n%s", err, output)
	}
}
