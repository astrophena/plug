// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"

	"go.astrophena.name/plug/internal/schema"
)

type indexSchema struct {
	path    string
	oldPath string
	deleted bool
}

func indexSchemas(ctx context.Context, dir string) ([]indexSchema, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("not inside a git repository: %w", err)
	}

	items := make(map[string]indexSchema)
	cmd = exec.CommandContext(ctx, "git", "diff-index", "--cached", "-z", "-M", "HEAD")
	cmd.Dir = dir
	data, err := cmd.Output()
	if err == nil {
		parts := strings.Split(string(data), "\x00")
		for i := 0; i < len(parts); i++ {
			fields := strings.Fields(parts[i])
			if len(fields) == 0 {
				continue
			}
			status := fields[len(fields)-1][:1]

			if i+1 >= len(parts) {
				break
			}
			path := parts[i+1]
			i++

			if status == "R" || status == "C" {
				if i+1 >= len(parts) {
					break
				}
				newPath := parts[i+1]
				i++
				if strings.HasSuffix(path, ".plug") || strings.HasSuffix(newPath, ".plug") {
					items[newPath] = indexSchema{
						path:    newPath,
						oldPath: path,
					}
				}
			} else if strings.HasSuffix(path, ".plug") {
				items[path] = indexSchema{
					path:    path,
					deleted: status == "D",
				}
			}
		}
	}

	cmd = exec.CommandContext(ctx, "git", "ls-files", "--cached", "-z", "*.plug")
	cmd.Dir = dir
	data, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed: %w", err)
	}
	for path := range strings.SplitSeq(string(data), "\x00") {
		path = strings.TrimSpace(path)
		if strings.HasSuffix(path, ".plug") {
			if _, exists := items[path]; !exists {
				items[path] = indexSchema{
					path: path,
				}
			}
		}
	}

	var result []indexSchema
	for _, item := range items {
		result = append(result, item)
	}
	slices.SortFunc(result, func(a, b indexSchema) int {
		return strings.Compare(a.path, b.path)
	})
	return result, nil
}

func runPreCommit(ctx context.Context, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("plug pre-commit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", ".", "directory or repository root to check")
	noColor := fs.Bool("no-color", false, "disable ASCII color formatting")
	if err := fs.Parse(args); err != nil {
		return err
	}

	useColor := !*noColor && os.Getenv("NO_COLOR") == ""
	if file, ok := stderr.(*os.File); ok {
		if stat, err := file.Stat(); err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
			useColor = !*noColor && os.Getenv("FORCE_COLOR") != ""
		}
	}

	var green, red, cyan, yellow, reset string
	if useColor {
		green, red, cyan, yellow, reset = "\033[1;32m", "\033[1;31m", "\033[1;36m", "\033[33m", "\033[0m"
	}

	files, err := indexSchemas(ctx, *dir)
	if err != nil {
		return fmt.Errorf("git index inspection failed: %w", err)
	}

	if len(files) == 0 {
		fmt.Fprintf(stderr, "%s✓ No Plug schemas found in %s.%s\n", green, *dir, reset)
		return nil
	}

	type issue struct {
		path     string
		problems []string
	}
	var issues []issue

	for _, item := range files {
		problems := checkIndexSchema(ctx, *dir, item)
		if len(problems) > 0 {
			issues = append(issues, issue{
				path:     item.path,
				problems: problems,
			})
		}
	}

	if len(issues) == 0 {
		var msg string
		if len(files) == 1 {
			msg = "✓ 1 Plug schema is valid for its declared version."
		} else {
			msg = fmt.Sprintf("✓ All %d Plug schemas are valid for their declared versions.", len(files))
		}
		fmt.Fprintf(stderr, "%s%s%s\n", green, msg, reset)
		return nil
	}

	schemaWord := "schemas"
	message := fmt.Sprintf("✗ Pre-commit check failed: %d schema files have issues:", len(issues))
	if len(issues) == 1 {
		schemaWord = "schema"
		message = "✗ Pre-commit check failed: 1 schema file has issues:"
	}

	fmt.Fprintf(stderr, "%s%s%s\n\n", red, message, reset)
	for _, issue := range issues {
		fmt.Fprintf(stderr, "  %s%s%s:\n", cyan, issue.path, reset)
		for _, item := range issue.problems {
			fmt.Fprintf(stderr, "    %s- %s%s\n", yellow, item, reset)
		}
	}
	fmt.Fprintln(stderr)

	return fmt.Errorf("pre-commit check failed for %d %s", len(issues), schemaWord)
}

func checkIndexSchema(ctx context.Context, dir string, item indexSchema) []string {
	if item.deleted {
		return []string{"schema file was deleted"}
	}

	data, err := gitShow(ctx, dir, ":"+item.path)
	if err != nil {
		return []string{fmt.Sprintf("read staged index: %v", err)}
	}

	newer, err := schema.Parse(item.path, data)
	if err != nil {
		return []string{err.Error()}
	}

	var issues []string
	for _, err := range schema.Lint(newer) {
		if e, ok := err.(*schema.Error); ok {
			issues = append(issues, e.Message)
		} else {
			issues = append(issues, err.Error())
		}
	}

	path := item.path
	if item.oldPath != "" {
		path = item.oldPath
	}

	data, err = gitShow(ctx, dir, "HEAD:"+path)
	if err != nil || len(data) == 0 {
		return issues
	}

	older, err := schema.Parse(path, data)
	if err != nil {
		return append(issues, fmt.Sprintf("parse HEAD schema %s: %v", path, err))
	}

	// A higher major version starts a new compatibility family.
	var (
		before = schema.RuntimeSchema(older)
		after  = schema.RuntimeSchema(newer)
	)
	if before.Package == after.Package && after.Major > before.Major {
		return issues
	}
	if err := schema.CheckCompatibility(older, newer); err != nil {
		for line := range strings.SplitSeq(err.Error(), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && line != "incompatible schema changes:" {
				issues = append(issues, strings.TrimPrefix(line, "- "))
			}
		}
	}
	return issues
}

func gitShow(ctx context.Context, dir, rev string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "show", rev)
	cmd.Dir = dir
	return cmd.Output()
}
