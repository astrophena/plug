// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/cli/clitest"
)

func TestGenerateJSONUsage(t *testing.T) {
	cases := map[string]clitest.Case[cli.AppFunc]{
		"missing output": {
			Args:    []string{"generate", "-json", "schema.plug"},
			WantErr: cli.ErrInvalidArgs,
		},
		"missing schema": {
			Args:    []string{"generate", "-json", "-o", "schema.json"},
			WantErr: cli.ErrInvalidArgs,
		},
		"extra schema": {
			Args:    []string{"generate", "-json", "-o", "schema.json", "a.plug", "b.plug"},
			WantErr: cli.ErrInvalidArgs,
		},
		"HTTP client": {
			Args:    []string{"generate", "-json", "-http-client", "-o", "schema.json", "schema.plug"},
			WantErr: cli.ErrInvalidArgs,
		},
		"TypeScript": {
			Args:    []string{"generate", "-json", "-ts", "-o", "schema.json", "schema.plug"},
			WantErr: cli.ErrInvalidArgs,
		},
		"all targets": {
			Args:    []string{"generate", "-json", "-http-client", "-ts", "-o", "schema.json", "schema.plug"},
			WantErr: cli.ErrInvalidArgs,
		},
	}
	clitest.Run(t, func(*testing.T) cli.AppFunc { return run }, cases)
}

func TestGenerateJSONPreservesOutputOnFailure(t *testing.T) {
	cases := map[string]struct {
		source string
		check  bool
	}{
		"stale output": {
			source: "package example\nexample.ping -> void",
			check:  true,
		},
		"invalid syntax": {
			source: "package example\nexample.ping ->",
			check:  false,
		},
		"invalid type": {
			source: "package example\nexample.ping -> Missing",
			check:  false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var (
				dir    = t.TempDir()
				source = filepath.Join(dir, "schema.plug")
				output = filepath.Join(dir, "schema.json")
				stale  = []byte("previous output\n")
				args   = []string{"generate", "-json", "-o", output}
			)
			if err := os.WriteFile(source, []byte(tc.source), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(output, stale, 0o644); err != nil {
				t.Fatal(err)
			}
			if tc.check {
				args = append(args, "-check")
			}
			args = append(args, source)
			if err := runApp(t.Context(), args, &bytes.Buffer{}); err == nil {
				t.Fatal("expected generation to fail")
			}
			got, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, stale) {
				t.Fatal("failed generation changed existing output")
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 2 {
				t.Fatalf("unexpected temporary output files: %v", entries)
			}
		})
	}
}
