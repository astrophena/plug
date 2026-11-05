// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/cli/clitest"
)

func TestGenerateUsage(t *testing.T) {
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
		"HTTP client and TypeScript": {
			Args:    []string{"generate", "-http-client", "-ts", "-o", "schema.json", "schema.plug"},
			WantErr: cli.ErrInvalidArgs,
		},
	}
	clitest.Run(t, func(*testing.T) cli.AppFunc { return run }, cases)
}

func TestGenerateJSONPreservesOutputOnFailure(t *testing.T) {
	var (
		dir    = t.TempDir()
		source = filepath.Join(dir, "schema.plug")
		output = filepath.Join(dir, "schema.json")
		stale  = []byte("previous output\n")
	)
	if err := os.WriteFile(source, []byte("package example\nexample.ping -> Missing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, stale, 0o644); err != nil {
		t.Fatal(err)
	}

	args := []string{"generate", "-json", "-o", output, source}
	if err := runApp(t.Context(), args, io.Discard); err == nil {
		t.Fatal("expected generation to fail")
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, stale) {
		t.Fatal("failed generation changed existing output")
	}
}
