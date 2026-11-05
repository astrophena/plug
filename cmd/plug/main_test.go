// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/cli/clitest"
	"go.astrophena.name/base/testutil"
	"go.astrophena.name/plug"
	"go.astrophena.name/plug/internal/schema"
)

func TestGenerate(t *testing.T) {
	cases := map[string]struct {
		flag   string
		golden string
	}{
		"native Go": {
			golden: "generate.go.golden",
		},
		"Go HTTP": {
			flag:   "-http-client",
			golden: "generate_http.go.golden",
		},
		"TypeScript": {
			flag:   "-ts",
			golden: "generate.ts.golden",
		},
		"JSON": {
			flag:   "-json",
			golden: "generate.json.golden",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var (
				dir    = filepath.Join("..", "..", "internal", "generate", "testdata")
				output = filepath.Join(t.TempDir(), "generated")
				args   = []string{"generate", "-o", output}
			)
			if tc.flag != "" {
				args = append(args, tc.flag)
			}
			args = append(args, filepath.Join(dir, "generate.plug"))
			if err := runApp(t.Context(), args, io.Discard); err != nil {
				t.Fatal(err)
			}

			got, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join(dir, tc.golden))
			if err != nil {
				t.Fatal(err)
			}
			testutil.AssertEqual(t, string(got), string(want))
		})
	}
}

func TestInspect(t *testing.T) {
	parsed, err := schema.Parse("inspect.plug", []byte("package example"))
	if err != nil {
		t.Fatal(err)
	}
	metadata := schema.RuntimeSchema(parsed)
	server, err := plug.NewServer(&metadata, plug.ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "plug.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-serveDone:
			if err != nil && !errors.Is(err, net.ErrClosed) {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("server did not stop")
		}
	})

	var output bytes.Buffer
	if err := runApp(ctx, []string{"inspect", "-network", "unix", path}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "version: 1\n") || !strings.Contains(output.String(), "fingerprint:") || !strings.Contains(output.String(), metadata.Source) {
		t.Fatalf("inspect output:\n%s", output.String())
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "init")
	git(t, dir, "config", "user.name", "Test User")
	git(t, dir, "config", "user.email", "test@example.com")
}

func TestPreCommit(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	schemaPath := filepath.Join(dir, "test.plug")
	validSchema := []byte(`
// This schema describes a pre-commit test service.
package test version 1

// This method pings the service.
test.ping -> void public
`)
	if err := os.WriteFile(schemaPath, validSchema, 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "test.plug")
	git(t, dir, "commit", "-m", "initial commit")

	var stderr bytes.Buffer
	if err := cli.Run(cli.WithEnv(t.Context(), &cli.Env{
		Args:   []string{"pre-commit", "-dir", dir, "-no-color"},
		Getenv: os.Getenv,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: &stderr,
	}), cli.AppFunc(run)); err != nil {
		t.Fatalf("pre-commit error = %v, stderr = %s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "valid for its declared version") {
		t.Fatalf("stderr output:\n%s", stderr.String())
	}

	invalidSchema := []byte(`
// This schema describes a pre-commit test service.
package test version 1

// This method pings the service.
test.ping -> invalidType public
`)
	if err := os.WriteFile(schemaPath, invalidSchema, 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "test.plug")
	if err := os.WriteFile(schemaPath, validSchema, 0o644); err != nil {
		t.Fatal(err)
	}

	stderr.Reset()
	if err := cli.Run(cli.WithEnv(t.Context(), &cli.Env{
		Args:   []string{"pre-commit", "-dir", dir, "-no-color"},
		Getenv: os.Getenv,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: &stderr,
	}), cli.AppFunc(run)); err == nil {
		t.Fatal("expected pre-commit error for staged invalid schema, got nil")
	}
	if !strings.Contains(stderr.String(), "Pre-commit check failed") || !strings.Contains(stderr.String(), "unknown type") {
		t.Fatalf("stderr output:\n%s", stderr.String())
	}

	if err := os.WriteFile(schemaPath, validSchema, 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "test.plug")
	if err := os.WriteFile(schemaPath, invalidSchema, 0o644); err != nil {
		t.Fatal(err)
	}

	stderr.Reset()
	if err := cli.Run(cli.WithEnv(t.Context(), &cli.Env{
		Args:   []string{"pre-commit", "-dir", dir, "-no-color"},
		Getenv: os.Getenv,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: &stderr,
	}), cli.AppFunc(run)); err != nil {
		t.Fatalf("expected staged valid content to pass despite unstaged breakage, got error = %v, stderr = %s", err, stderr.String())
	}
}

func TestUsage(t *testing.T) {
	clitest.Run(t, func(*testing.T) cli.AppFunc { return run }, map[string]clitest.Case[cli.AppFunc]{
		"empty": {
			WantErr:            cli.ErrInvalidArgs,
			WantNothingPrinted: true,
		},
		"unknown": {
			Args:               []string{"wat"},
			WantErr:            cli.ErrInvalidArgs,
			WantNothingPrinted: true,
		},
	})
}

func runApp(ctx context.Context, args []string, stdout io.Writer) error {
	return cli.Run(cli.WithEnv(ctx, &cli.Env{
		Args:   args,
		Getenv: os.Getenv,
		Stdin:  strings.NewReader(""),
		Stdout: stdout,
		Stderr: io.Discard,
	}), cli.AppFunc(run))
}

func TestGenerateFreshness(t *testing.T) {
	schemaPath := filepath.Join(t.TempDir(), "schema.plug")
	if err := os.WriteFile(schemaPath, []byte("package example version 1\ntype ID = i64\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(t.TempDir(), "generated")
	args := []string{"generate", "-o", output, schemaPath}
	checkArgs := append([]string{"generate", "-check"}, args[1:]...)

	if err := runApp(t.Context(), checkArgs, io.Discard); !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), "is missing or unreadable") {
		t.Fatalf("check missing output: %v; want missing-file error", err)
	}
	if _, err := os.Stat(output); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("check created output: %v", err)
	}
	if err := runApp(t.Context(), args, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := runApp(t.Context(), checkArgs, io.Discard); err != nil {
		t.Fatalf("check fresh output: %v", err)
	}

	const stale = "stale output\n"
	if err := os.WriteFile(output, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runApp(t.Context(), checkArgs, io.Discard); err == nil || !strings.Contains(err.Error(), "is out of date") {
		t.Fatalf("check stale output: %v; want out-of-date error", err)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != stale {
		t.Fatalf("check changed stale output to %q", got)
	}
}

func TestPreCommitMajorVersionMigration(t *testing.T) {
	cases := map[string]struct {
		before, after string
		wantErr       bool
	}{
		"same version": {
			before:  "package test version 1",
			after:   "package test version 1",
			wantErr: true,
		},
		"higher version": {
			before: "package test version 1",
			after:  "package test version 2",
		},
		"lower version": {
			before:  "package test version 2",
			after:   "package test version 1",
			wantErr: true,
		},
		"renamed package": {
			before:  "package test version 1",
			after:   "package renamed version 2",
			wantErr: true,
		},
		"undocumented higher version": {
			before:  "package test version 1",
			after:   "package test version 2",
			wantErr: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			initGitRepo(t, dir)
			path := filepath.Join(dir, "test.plug")
			before := "// This schema describes a test service.\n" + tc.before + "\n\n// This method pings the service.\ntest.ping -> void public\n"
			if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
				t.Fatal(err)
			}
			git(t, dir, "add", "test.plug")
			git(t, dir, "commit", "-m", "initial schema")
			// Removing a method breaks compatibility within the same version.
			after := "// This schema describes a test service.\n" + tc.after + "\n"
			if name == "undocumented higher version" {
				after = tc.after + "\n"
			}
			if err := os.WriteFile(path, []byte(after), 0o644); err != nil {
				t.Fatal(err)
			}
			git(t, dir, "add", "test.plug")
			var stderr bytes.Buffer
			err := runPreCommit(t.Context(), []string{"-dir", dir, "-no-color"}, &stderr)
			if (err != nil) != tc.wantErr {
				t.Fatalf("pre-commit: err=%v wantErr=%v\n%s", err, tc.wantErr, stderr.String())
			}
		})
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
