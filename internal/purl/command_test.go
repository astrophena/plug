// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.astrophena.name/base/cli"
	"go.astrophena.name/base/cli/clitest"
	"go.astrophena.name/base/version"
	"go.astrophena.name/plug/plugupgrade"
)

func TestCommand(t *testing.T) {
	server, _, _ := testServer(t)
	endpoint := serveTest(t, server, "tcp", "127.0.0.1:0")
	file := filepath.Join(t.TempDir(), "script.purl")
	if err := os.WriteFile(file, []byte("greeter.getUser(id: 2).name"), 0600); err != nil {
		t.Fatal(err)
	}
	clitest.Run(t, func(*testing.T) *Command { return new(Command) }, map[string]clitest.Case[*Command]{
		"expression": {
			Args:         []string{"-e", "greeter.getUser(id: 1).name", endpoint},
			WantInStdout: `"Alice"`,
		},
		"bare TCP": {
			Args:         []string{"-e", "greeter.getUser(id: 1).name", strings.TrimPrefix(endpoint, "tcp://")},
			WantInStdout: `"Alice"`,
		},
		"JSON": {
			Args:         []string{"-json", "-e", "greeter.listUsers(limit: 2) | map(u => u.name)", endpoint},
			WantInStdout: `["Alice","Bob"]`,
		},
		"stdin": {
			Args:         []string{endpoint},
			Stdin:        strings.NewReader("let user = greeter.getUser(id: 2)\nuser.name\n"),
			WantInStdout: `"Alice"`,
		},
		"file": {
			Args:         []string{"-f", file, endpoint},
			WantInStdout: `"Alice"`,
		},
		"empty expression": {
			Args:               []string{"-e", "", endpoint},
			Stdin:              strings.NewReader("missing()"),
			WantNothingPrinted: true,
		},
		"no endpoint": {
			WantErr: cli.ErrInvalidArgs,
		},
		"conflicting modes": {
			Args:    []string{"-e", "1", "-f", file, endpoint},
			WantErr: cli.ErrInvalidArgs,
		},
		"negative timeout": {
			Args:    []string{"-timeout", "-1s", endpoint},
			WantErr: cli.ErrInvalidArgs,
		},
	})
}

func TestUpgradeConnection(t *testing.T) {
	for _, scheme := range []string{"http", "https", "ws", "wss"} {
		t.Run(scheme, func(t *testing.T) {
			server, _, _ := testServer(t)
			upgrade, err := plugupgrade.NewServer(server, plugupgrade.Options{})
			if err != nil {
				t.Fatal(err)
			}
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/plug" || r.Header.Get("Authorization") != "Bearer test" {
					http.Error(w, "denied", http.StatusForbidden)
					return
				}
				if got, want := r.Header.Get("User-Agent"), version.UserAgent(); got != want {
					http.Error(w, fmt.Sprintf("User-Agent = %q; want %q", got, want), http.StatusBadRequest)
					return
				}
				upgrade.ServeHTTP(w, r)
			})
			web := httptest.NewUnstartedServer(handler)
			web.Config.ErrorLog = log.New(io.Discard, "", 0)
			if scheme == "https" || scheme == "wss" {
				web.StartTLS()
			} else {
				web.Start()
			}
			t.Cleanup(web.Close)
			t.Cleanup(func() {
				if err := upgrade.Shutdown(context.Background()); err != nil {
					t.Error(err)
				}
			})
			endpoint := web.URL
			if scheme == "ws" || scheme == "wss" {
				endpoint = strings.Replace(endpoint, "http", "ws", 1)
			}
			s, err := connect(t.Context(), endpoint+"/plug", http.Header{"Authorization": []string{"Bearer test"}})
			if scheme == "https" || scheme == "wss" {
				if err == nil {
					s.client.Close()
					t.Fatal("HTTPS accepted an untrusted certificate")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.client.Close() })
			for range 2 {
				if _, err := evaluate(t, s, "greeter.getUser(id: 1)"); err != nil {
					t.Fatal(err)
				}
				if err := s.client.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestUnixConnection(t *testing.T) {
	server, _, _ := testServer(t)
	// Keep socket paths below sockaddr_un's small platform-dependent limit.
	dir, err := os.MkdirTemp("", "purl-")
	if err != nil {
		t.Fatal(err)
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "s%?# ")
	serveTest(t, server, "unix", path)
	u := url.URL{
		Scheme: "unix",
		Path:   path,
	}
	for name, endpoint := range map[string]string{"path": path, "URL": u.String()} {
		t.Run(name, func(t *testing.T) {
			s, err := connect(t.Context(), endpoint, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer s.client.Close()
			if _, err := evaluate(t, s, "greeter.ping()"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeEndpoints(t *testing.T) {
	for _, endpoint := range []string{
		"/run/overseer/socket",
		"localhost:9000",
		"127.0.0.1:9000",
		"[::1]:9000",
		"[fe80::1%eth0]:9000",
	} {
		t.Run(endpoint, func(t *testing.T) {
			if _, err := dialer(endpoint, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := dialer(endpoint, http.Header{"Authorization": []string{"Bearer test"}}); err == nil {
				t.Fatal("accepted HTTP headers for a native endpoint")
			}
		})
	}
}

func TestInvalidEndpoints(t *testing.T) {
	for _, endpoint := range []string{
		"",
		"localhost",
		"localhost:",
		"localhost:-1",
		"localhost:65536",
		"localhost:9000/path",
		"localhost:9000?query",
		"::1:9000",
		"/",
		"tcp://host",
		"tcp://host:9000/path",
		"tcp://host:9000?q=x",
		"unix://run/path",
		"unix:///",
		"unix:relative",
		"https:///plug",
		"https://user:password@example.test/plug",
		"https://example.test/plug#fragment",
	} {
		t.Run(endpoint, func(t *testing.T) {
			if _, err := dialer(endpoint, nil); err == nil {
				t.Fatal("accepted invalid endpoint")
			}
		})
	}
}

func TestScriptStopsOnError(t *testing.T) {
	for source, want := range map[string]string{
		"missing()":             "unknown name",
		`assert(false, "stop")`: "assert: stop",
	} {
		t.Run(source, func(t *testing.T) {
			s, calls := testSession(t)
			err := new(Command).script(t.Context(), s, strings.NewReader(source+"\ngreeter.ping()"), io.Discard)
			if err == nil || !strings.Contains(err.Error(), want) || calls.Load() != 0 {
				t.Fatalf("script: %v, calls=%d; want %q and no calls", err, calls.Load(), want)
			}
		})
	}
}

func TestCommandSanitizesErrors(t *testing.T) {
	ctx := cli.WithEnv(t.Context(), &cli.Env{
		Args:   []string{"bad\x1b[2J://host"},
		Getenv: func(string) string { return "" },
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	err := new(Command).Run(ctx)
	if err == nil || strings.ContainsRune(err.Error(), '\x1b') {
		t.Fatalf("unsafe error: %v", err)
	}
}
