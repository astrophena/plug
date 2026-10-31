// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package purl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"go.astrophena.name/base/cli"

	"github.com/chzyer/readline"
	"golang.org/x/sys/unix"
)

// Run separately so terminal state and signal handlers cannot affect the tests.
func TestREPLChild(t *testing.T) {
	endpoint := os.Getenv("PURL_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("subprocess helper")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	args := []string{"-no-history", "-timeout", "5s"}
	if os.Getenv("PURL_TEST_JSON") != "" {
		args = append(args, "-json")
	}
	ctx = cli.WithEnv(ctx, &cli.Env{
		Args:   append(args, endpoint),
		Getenv: os.Getenv,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if err := cli.Run(ctx, new(Command)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestPagerChild(t *testing.T) {
	mode := os.Getenv("PURL_TEST_PAGER")
	if mode == "" {
		t.Skip("subprocess helper")
	}
	if mode == "early" {
		fmt.Println("PAGER QUIT")
		os.Exit(0)
	}

	text, err := io.ReadAll(os.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("PURL_TEST_PAGE"), text, 0600); err != nil {
		t.Fatal(err)
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	settings, err := unix.IoctlGetTermios(int(tty.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Lflag&unix.ICANON == 0 {
		t.Fatal("pager inherited raw mode")
	}
	state, err := readline.MakeRaw(int(tty.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("PAGER READY")
	key := make([]byte, 1)
	if _, err := io.ReadFull(tty, key); err != nil {
		t.Fatal(err)
	}
	if err := readline.Restore(int(tty.Fd()), state); err != nil {
		t.Fatal(err)
	}
	if key[0] != 'q' {
		t.Fatalf("pager received %q, want q", key)
	}
	fmt.Println("PAGER KEY q")
	os.Exit(0)
}

type replTerm struct {
	f    *os.File
	cmd  *exec.Cmd
	text strings.Builder
}

func childEnv() []string {
	// REPL and pager helpers otherwise each pay the race detector's exit delay.
	return append(os.Environ(), "GORACE="+os.Getenv("GORACE")+" atexit_sleep_ms=0")
}

func BenchmarkChildExit(b *testing.B) {
	for b.Loop() {
		cmd := exec.CommandContext(b.Context(), os.Args[0], "-test.run=^TestPagerChild$")
		cmd.Env = append(childEnv(), "PURL_TEST_PAGER=early")
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("child: %v\n%s", err, out)
		}
	}
}

func startREPL(t *testing.T, endpoint string, height int, env ...string) *replTerm {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("PTY unavailable: %v", err)
	}
	t.Cleanup(func() { master.Close() })
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer slave.Close()
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{
		Row: uint16(height),
		Col: 100,
	}); err != nil {
		t.Fatal(err)
	}

	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestREPLChild$")
	child.Env = append(childEnv(), "PURL_TEST_ENDPOINT="+endpoint, "NO_COLOR=1", "TERM=xterm", "NO_PAGER=1")
	child.Env = append(child.Env, env...)
	child.Stdin, child.Stdout, child.Stderr = slave, slave, slave
	child.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    0,
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if child.ProcessState == nil {
			child.Process.Kill()
			child.Wait()
		}
	})
	return &replTerm{
		f:   master,
		cmd: child,
	}
}

func (r *replTerm) until(t *testing.T, needle string) string {
	t.Helper()
	if err := r.f.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	buf := make([]byte, 8192)
	for !strings.Contains(replOutput.Replace(out.String()), replOutput.Replace(needle)) {
		n, err := r.f.Read(buf)
		out.Write(buf[:n])
		r.text.Write(buf[:n])
		if err != nil {
			t.Fatalf("waiting for %q: %v\noutput: %q", needle, err, out.String())
		}
	}
	return out.String()
}

func (r *replTerm) send(t *testing.T, text string) {
	t.Helper()
	if _, err := r.f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

func (r *replTerm) quit(t *testing.T) {
	t.Helper()
	r.send(t, ":quit\r")
	if err := r.cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

const replPrompt = "\x1b[J\x1b[2K\rpurl> "

// Ignore cursor redraws, which can surround evaluation output between reads.
var replOutput = strings.NewReplacer("\x1b[J\x1b[2K\r", "", " \b", "")

func TestREPLEOF(t *testing.T) {
	server, _, _ := testServer(t)
	endpoint := serveTest(t, server, "tcp", "127.0.0.1:0")
	for name, source := range map[string]string{"prompt": "", "continuation": "[\r"} {
		t.Run(name, func(t *testing.T) {
			r := startREPL(t, endpoint, 30)
			r.until(t, replPrompt)
			prompt := replPrompt
			if source != "" {
				r.send(t, source)
				prompt = "\x1b[J\x1b[2K\r ...> "
				r.until(t, prompt)
			}
			r.send(t, "\x04")
			if err := r.cmd.Wait(); err != nil {
				t.Fatal(err)
			}

			out, err := io.ReadAll(r.f)
			if err != nil && !errors.Is(err, syscall.EIO) {
				t.Fatal(err)
			}
			// Readline may redraw the prompt and clear the line while exiting.
			text := strings.TrimPrefix(string(out), prompt)
			text = strings.ReplaceAll(text, "\x1b[J\x1b[2K\r", "")
			if strings.TrimSpace(text) != "" {
				t.Fatalf("Ctrl-D printed %q", out)
			}
			settings, err := unix.IoctlGetTermios(int(r.f.Fd()), unix.TCGETS)
			if err != nil {
				t.Fatal(err)
			}
			if settings.Lflag&(unix.ICANON|unix.ECHO) != unix.ICANON|unix.ECHO {
				t.Fatal("Ctrl-D did not restore terminal mode")
			}
		})
	}
}

func TestREPLTerminal(t *testing.T) {
	server, _, waiting := testServer(t)
	endpoint := serveTest(t, server, "tcp", "127.0.0.1:0")
	r := startREPL(t, endpoint, 30)
	greeting := r.until(t, replPrompt)
	want := "Connected to greeter (schema v1).\r\n\r\n" +
		"Tab completes methods and arguments.\r\n" +
		":help explores the API; Ctrl-C cancels; Ctrl-D exits.\r\n\r\n" + replPrompt
	if !strings.HasPrefix(greeting, want) {
		t.Fatalf("greeting = %q, want prefix %q", greeting, want)
	}
	r.send(t, "1 + 2\r")
	r.until(t, "\r\n3\r\n"+replPrompt)
	r.send(t, "greeter.getUser(id: 42)\r")
	out := r.until(t, "\r\n)\r\n"+replPrompt)
	if !strings.Contains(out, "\r\n  name: \"Alice\",\r\n") {
		t.Fatalf("bad response layout: %q", out)
	}
	r.send(t, "missing()\r")
	r.until(t, "unknown name \"missing\"\r\n"+replPrompt)
	r.send(t, "[\r")
	r.until(t, " ...> ")
	r.send(t, "1, 2]\r")
	r.until(t, "\r\n[1, 2]\r\n"+replPrompt)
	r.send(t, "greeter.\t")
	r.until(t, "listUsers")
	r.send(t, "\x03")
	r.until(t, "^C\r\n"+replPrompt)
	r.send(t, ":vars\r")
	r.until(t, "_: list\r\n"+replPrompt)
	r.send(t, "greeter.getU\t(id: 3)\r")
	r.until(t, "\r\n)\r\n"+replPrompt)
	r.send(t, "greeter.getUser(id: -1)\r")
	select {
	case <-waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("RPC did not start")
	}
	r.send(t, "\x03")
	r.until(t, "context canceled\r\n"+replPrompt)
	r.send(t, "4 + 5\r")
	r.until(t, "\r\n9\r\n"+replPrompt)
	r.quit(t)

	// LF must reset the column, or multiline responses form a staircase.
	text := r.text.String()
	for i := range text {
		if text[i] == '\n' && (i == 0 || text[i-1] != '\r') {
			t.Fatalf("bare newline at %d in terminal output: %q", i, text)
		}
	}
}

func TestREPLPager(t *testing.T) {
	server, calls, _ := testServer(t)
	endpoint := serveTest(t, server, "tcp", "127.0.0.1:0")
	page := t.TempDir() + "/page"
	pager := fmt.Sprintf("%q -test.run=^TestPagerChild$", os.Args[0])
	r := startREPL(t, endpoint, 8, "NO_PAGER=", "PAGER="+pager, "PURL_TEST_PAGER=normal", "PURL_TEST_PAGE="+page)
	r.until(t, replPrompt)
	r.send(t, "greeter.getUser(id: 42)\r")
	r.until(t, "PAGER READY\r\n")
	r.send(t, "q")
	r.until(t, "PAGER KEY q\r\n"+replPrompt)
	if calls.Load() != 1 {
		t.Fatal("pager repeated the request")
	}
	text, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "Alice") || strings.ContainsRune(string(text), '\x1b') {
		t.Fatalf("unexpected page: %q", text)
	}

	// Pager input must not leak into the next expression, and the result stays
	// available without another RPC.
	r.send(t, "_.name\r")
	r.until(t, "\r\n\"Alice\"\r\n"+replPrompt)
	if calls.Load() != 1 {
		t.Fatal("reading the result repeated the request")
	}
	for _, command := range []string{":schema", ":help", ":help greeter.getUser"} {
		r.send(t, command+"\r")
		r.until(t, "PAGER READY\r\n")
		r.send(t, "q")
		r.until(t, "PAGER KEY q\r\n"+replPrompt)
	}
	r.send(t, "let double = x => x + x\r")
	r.until(t, "double (deferred)\r\n"+replPrompt)
	r.send(t, `"0123456789"`+strings.Repeat(" | double", 12)+"\r")
	r.until(t, "PAGER READY\r\n")
	r.send(t, "q")
	r.until(t, "PAGER KEY q\r\n"+replPrompt)
	text, err = os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if want := `"` + strings.Repeat("0123456789", 1<<12) + "\"\n"; string(text) != want {
		t.Fatalf("pager received %d bytes, want %d complete bytes", len(text), len(want))
	}
	r.send(t, "count(_)\r")
	r.until(t, "\r\n40960\r\n"+replPrompt)
	r.quit(t)
}

func TestREPLHighlight(t *testing.T) {
	server, calls, _ := testServer(t)
	endpoint := serveTest(t, server, "tcp", "127.0.0.1:0")
	r := startREPL(t, endpoint, 30, "NO_COLOR=")
	r.until(t, replPrompt)
	r.send(t, "let draft = nil")
	r.until(t, "\x1b[33mnil\x1b[39m")
	r.send(t, "\x03")
	r.until(t, "^C\r\n"+replPrompt)
	r.send(t, "/* open\r")
	r.until(t, " ...> ")
	r.send(t, "still a comment")
	r.until(t, "\x1b[90mstill a comment\x1b[39m")
	r.send(t, "\x03")
	r.until(t, "\r\n"+replPrompt)
	r.send(t, "greeter.getUser\r")
	out := r.until(t, "Use the [UserID] returned by [greeter.listUsers].\r\n\r\n"+replPrompt)
	if !strings.Contains(out, "\x1b[34mgreeter.getUser\x1b[39m") || strings.Contains(out, "<function") {
		t.Fatalf("method did not show highlighted documentation: %q", out)
	}
	r.send(t, ":help\r")
	out = r.until(t, "\x1b[34mgreeter.ping\x1b[39m\r\n\r\n"+replPrompt)
	if !strings.Contains(out, "\x1b[35mlet\x1b[39m") {
		t.Fatalf("help examples are not highlighted: %q", out)
	}
	r.send(t, ":schema\r")
	r.until(t, "\x1b[35mpackage\x1b[39m")
	// A subsequent expression also checks that schema output left input usable.
	r.send(t, "1 + 1\r")
	r.until(t, "\r\n\x1b[33m2\x1b[39m\r\n"+replPrompt)
	if calls.Load() != 0 {
		t.Fatal("viewing documentation sent a request")
	}
	r.quit(t)
}

func TestREPLNoColor(t *testing.T) {
	server, _, _ := testServer(t)
	endpoint := serveTest(t, server, "tcp", "127.0.0.1:0")
	for name, env := range map[string][]string{
		"NO_COLOR":      {"NO_COLOR=1"},
		"dumb terminal": {"NO_COLOR=", "TERM=dumb"},
	} {
		t.Run(name, func(t *testing.T) {
			r := startREPL(t, endpoint, 30, env...)
			r.until(t, replPrompt)
			r.send(t, "nil\r")
			r.until(t, "\r\nnil\r\n"+replPrompt)
			r.send(t, ":help UserID\r")
			r.until(t, "type UserID = i64\r\n\r\n"+replPrompt)
			text := r.text.String()
			if uncolor(text) != text {
				t.Fatalf("color was not disabled: %q", text)
			}
			r.quit(t)
		})
	}
}

func TestREPLTypeahead(t *testing.T) {
	server, _, _ := testServer(t)
	endpoint := serveTest(t, server, "tcp", "127.0.0.1:0")
	r := startREPL(t, endpoint, 30, "NO_COLOR=")
	r.until(t, replPrompt)
	var source strings.Builder
	source.WriteString(":schema\r")
	for i := range 50 {
		fmt.Fprintf(&source, "%d + 1\r", i)
	}
	r.send(t, source.String())
	r.until(t, "\r\n\x1b[33m50\x1b[39m\r\n"+replPrompt)
	text := replOutput.Replace(r.text.String())
	for i := range 50 {
		want := fmt.Sprintf("\r\n\x1b[33m%d\x1b[39m\r\n", i+1)
		if !strings.Contains(text, want) {
			t.Fatalf("missing result %d in %q", i+1, text)
		}
	}
	r.quit(t)
}

func TestREPLTables(t *testing.T) {
	for name, env := range map[string][]string{
		"plain": nil,
		"JSON":  {"PURL_TEST_JSON=1", "NO_COLOR="},
	} {
		t.Run(name, func(t *testing.T) {
			server, calls, _ := testServer(t)
			endpoint := serveTest(t, server, "tcp", "127.0.0.1:0")
			r := startREPL(t, endpoint, 30, env...)
			r.until(t, replPrompt)
			r.send(t, "greeter.listUsers(limit: 2)\r")
			if name == "JSON" {
				out := r.until(t, `"email":null}]`+"\r\n"+replPrompt)
				if strings.Contains(out, "Use _[0]") || uncolor(out) != out {
					t.Fatalf("JSON used table formatting: %q", out)
				}
			} else {
				out := r.until(t, "2 rows. Use _[0] for a full row.\r\n"+replPrompt)
				if !strings.Contains(out, "#  id") || !strings.Contains(out, `"Alice"`) {
					t.Fatalf("missing record table: %q", out)
				}
			}
			r.send(t, "let saved = _\r")
			if name == "JSON" {
				r.until(t, "\r\n"+replPrompt)
			} else {
				r.until(t, "saved (deferred)\r\n"+replPrompt)
			}
			r.send(t, "saved[0].name\r")
			r.until(t, "\r\n\"Alice\"\r\n"+replPrompt)
			r.send(t, "count(saved)\r")
			r.until(t, "\r\n2\r\n"+replPrompt)
			if calls.Load() != 1 {
				t.Fatal("table display or exploration repeated the RPC")
			}
			r.quit(t)
		})
	}
}

func TestREPLTablePager(t *testing.T) {
	server, calls, _ := testServer(t)
	endpoint := serveTest(t, server, "tcp", "127.0.0.1:0")
	page := t.TempDir() + "/page"
	pager := fmt.Sprintf("%q -test.run=^TestPagerChild$", os.Args[0])
	r := startREPL(t, endpoint, 5, "NO_COLOR=", "NO_PAGER=", "PAGER="+pager, "PURL_TEST_PAGER=normal", "PURL_TEST_PAGE="+page)
	r.until(t, replPrompt)
	r.send(t, "greeter.listUsers(limit: 2)\r")
	r.until(t, "PAGER READY\r\n")
	r.send(t, "q")
	r.until(t, "PAGER KEY q\r\n"+replPrompt)
	text, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(text), "\x1b[34m#  id") || !strings.Contains(string(text), "2 rows. Use _[0]") {
		t.Fatalf("pager did not receive a highlighted table: %q", text)
	}
	r.send(t, "_[0].name\r")
	r.until(t, "\r\n\x1b[32m\"Alice\"\x1b[39m\r\n"+replPrompt)
	if calls.Load() != 1 {
		t.Fatal("table paging repeated the RPC")
	}
	r.quit(t)
}

func TestREPLTimestamp(t *testing.T) {
	server, _, _ := testServer(t)
	endpoint := serveTest(t, server, "tcp", "127.0.0.1:0")
	r := startREPL(t, endpoint, 30)
	r.until(t, replPrompt)
	stamp := time.Now().UTC().Add(-3*time.Hour - time.Minute).Format(time.RFC3339Nano)
	r.send(t, `"`+stamp+`"`+"\r")
	r.until(t, `"`+stamp+`" (3 hours ago)`+"\r\n"+replPrompt)
	r.send(t, "toJSON(_)\r")
	r.until(t, `"\"`+stamp+`\""`+"\r\n"+replPrompt)
	r.quit(t)
}

func TestREPLPagerColor(t *testing.T) {
	server, _, _ := testServer(t)
	endpoint := serveTest(t, server, "tcp", "127.0.0.1:0")
	page := t.TempDir() + "/page"
	pager := fmt.Sprintf("%q -test.run=^TestPagerChild$", os.Args[0])
	r := startREPL(t, endpoint, 8, "NO_COLOR=", "NO_PAGER=", "PAGER="+pager, "PURL_TEST_PAGER=normal", "PURL_TEST_PAGE="+page)
	r.until(t, replPrompt)
	r.send(t, "greeter.getUser(id: 42)\r")
	r.until(t, "PAGER READY\r\n")
	r.send(t, "q")
	r.until(t, "PAGER KEY q\r\n"+replPrompt)
	text, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "\x1b[32m\"Alice\"\x1b[39m") {
		t.Fatalf("pager lost highlighting: %q", text)
	}
	r.send(t, "1 + 1\r")
	r.until(t, "\r\n\x1b[33m2\x1b[39m\r\n"+replPrompt)
	r.quit(t)
}

func TestREPLLess(t *testing.T) {
	if _, err := exec.LookPath("less"); err != nil {
		t.Skip("less is not installed")
	}
	server, _, _ := testServer(t)
	endpoint := serveTest(t, server, "tcp", "127.0.0.1:0")
	r := startREPL(t, endpoint, 8, "NO_PAGER=", "PAGER=", "LESS=", "LESSOPEN=", "LESSCLOSE=")
	r.until(t, replPrompt)
	r.send(t, ":schema\r")
	r.until(t, "package greeter version 1")
	r.send(t, "q")
	r.until(t, replPrompt)
	r.send(t, "1 + 1\r")
	r.until(t, "\r\n2\r\n"+replPrompt)
	r.quit(t)
}

func TestREPLPagerFallback(t *testing.T) {
	server, _, _ := testServer(t)
	endpoint := serveTest(t, server, "tcp", "127.0.0.1:0")
	pager := fmt.Sprintf("%q -test.run=^TestPagerChild$", os.Args[0])
	broken := t.TempDir() + "/pager"
	if err := os.WriteFile(broken, []byte("#!/purl-no-such-interpreter\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		env  []string
		want string
	}{
		{
			"disabled",
			[]string{"PAGER=" + pager},
			"\r\n)\r\n" + replPrompt,
		},
		{
			"start failure",
			[]string{"NO_PAGER=", "PAGER=" + broken},
			"\r\n)\r\n" + replPrompt,
		},
		{
			"early quit",
			[]string{"NO_PAGER=", "PAGER=" + pager, "PURL_TEST_PAGER=early"},
			"PAGER QUIT\r\n" + replPrompt,
		},
		{
			"JSON",
			[]string{"NO_PAGER=", "NO_COLOR=", "PAGER=" + pager, "PURL_TEST_JSON=1"},
			"\"email\":\"alice@example.test\"}\r\n" + replPrompt,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := startREPL(t, endpoint, 8, tc.env...)
			r.until(t, replPrompt)
			r.send(t, "greeter.getUser(id: 42)\r")
			r.until(t, tc.want)
			if tc.name == "early quit" {
				r.send(t, "let double = x => x + x\r")
				r.until(t, "double (deferred)\r\n"+replPrompt)
				r.send(t, `"0123456789"`+strings.Repeat(" | double", 15)+"\r")
				r.until(t, "PAGER QUIT\r\n"+replPrompt)
			}
			r.send(t, "1 + 1\r")
			r.until(t, "\r\n2\r\n"+replPrompt)
			if tc.name == "JSON" {
				r.send(t, ":help UserID\r")
				r.until(t, "type UserID = i64\r\n\r\n"+replPrompt)
				text := r.text.String()
				if uncolor(text) != text || strings.Contains(text, "Connected to") {
					t.Fatalf("JSON mode printed colors or a greeting: %q", text)
				}
			}
			r.quit(t)
		})
	}
}
