// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/plug/internal/generate"
	"go.astrophena.name/plug/internal/schema"
)

func runGenerate(args []string) error {
	fs := flag.NewFlagSet("plug generate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var (
		out        = fs.String("o", "", "generated Go, TypeScript, or JSON output path")
		check      = fs.Bool("check", false, "check that output is up-to-date without writing")
		httpClient = fs.Bool("http-client", false, "generate Go HTTP client (standard library only)")
		ts         = fs.Bool("ts", false, "generate TypeScript types and HTTP client")
		json       = fs.Bool("json", false, "export the resolved schema as JSON")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *out == "" || fs.NArg() != 1 {
		return fmt.Errorf("%w: usage: plug generate [-check] [-http-client|-ts|-json] -o OUTPUT SCHEMA.plug", cli.ErrInvalidArgs)
	}
	if *httpClient && *ts {
		return fmt.Errorf("%w: cannot specify both -http-client and -ts", cli.ErrInvalidArgs)
	}
	if *json && (*httpClient || *ts) {
		return fmt.Errorf("%w: cannot combine -json with -http-client or -ts", cli.ErrInvalidArgs)
	}

	path := fs.Arg(0)
	s, err := readSchema(path)
	if err != nil {
		return err
	}

	var (
		meta = schema.RuntimeSchema(s)
		data []byte
	)
	switch {
	case *httpClient:
		data, err = generate.GoHTTPClient(meta)
	case *ts:
		data, err = generate.TypeScript(meta)
	case *json:
		data, err = generate.JSON(meta)
	default:
		data, err = generate.Go(meta)
	}
	if err != nil {
		return err
	}

	if *check {
		old, err := os.ReadFile(*out)
		if err != nil {
			return fmt.Errorf("generated output file %s is missing or unreadable: %w", *out, err)
		}
		if !bytes.Equal(old, data) {
			return fmt.Errorf("generated output file %s is out of date", *out)
		}
		return nil
	}

	if err := writeAtomic(*out, data); err != nil {
		return fmt.Errorf("write generated output: %w", err)
	}
	return nil
}

func writeAtomic(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}

	tmp := f.Name()
	defer func() {
		if removeErr := os.Remove(tmp); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()

	if err := f.Chmod(0o644); err != nil {
		return errors.Join(err, f.Close())
	}
	if _, err := f.Write(data); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
