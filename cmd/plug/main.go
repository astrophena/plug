// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/plug"
	"go.astrophena.name/plug/internal/schema"
)

func main() { cli.Main(cli.AppFunc(run)) }

func run(ctx context.Context) error {
	env := cli.GetEnv(ctx)
	args := env.Args
	if len(args) == 0 {
		return fmt.Errorf("%w: command is required; use -help for usage", cli.ErrInvalidArgs)
	}

	switch args[0] {
	case "check":
		return runCheck(args[1:])
	case "lint":
		return runLint(args[1:])
	case "doc":
		return runDoc(ctx, args[1:])
	case "generate":
		return runGenerate(args[1:])
	case "inspect":
		return runInspect(ctx, args[1:], env.Stdout)
	case "pre-commit":
		return runPreCommit(ctx, args[1:], env.Stderr)
	default:
		return fmt.Errorf("%w: unknown command %q; use -help for usage", cli.ErrInvalidArgs, args[0])
	}
}

func runCheck(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("%w: usage: plug check OLD.plug NEW.plug", cli.ErrInvalidArgs)
	}

	older, err := readSchema(args[0])
	if err != nil {
		return err
	}

	newer, err := readSchema(args[1])
	if err != nil {
		return err
	}
	return schema.CheckCompatibility(older, newer)
}

func runLint(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%w: usage: plug lint SCHEMA.plug", cli.ErrInvalidArgs)
	}

	s, err := readSchema(args[0])
	if err != nil {
		return err
	}
	issues := schema.Lint(s)
	if len(issues) > 0 {
		var msgs []string
		for _, issue := range issues {
			msgs = append(msgs, issue.Error())
		}
		return fmt.Errorf("lint issues:\n- %s", strings.Join(msgs, "\n- "))
	}
	return nil
}

func readSchema(path string) (*schema.Schema, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read schema: %w", err)
	}
	return schema.Parse(path, source)
}

func runInspect(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("plug inspect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	network := fs.String("network", "unix", "network accepted by net.Dial")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%w: usage: plug inspect [-network NETWORK] ADDRESS", cli.ErrInvalidArgs)
	}

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, *network, fs.Arg(0))
	if err != nil {
		return err
	}

	remote, err := plug.Inspect(ctx, conn)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(stdout, "version: %d\nfamily: %x\nfingerprint: %x\n\n%s", remote.Version, remote.Family, remote.Fingerprint, remote.Source)
	return err
}
