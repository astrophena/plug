// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package jsoncontract

//go:generate go tool plug generate -http-client -o client.go schema.plug

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
)

func TestPaths(t *testing.T) {
	for name, tc := range map[string]struct {
		path Option[Path]
		want string
	}{
		"root": {
			want: "/repos/group%2Fnested%2Frepo/contents/",
		},
		"nested": {
			path: Some(Path("docs/a file%?.txt")),
			want: "/repos/group%2Fnested%2Frepo/contents/docs/a%20file%25%3F.txt",
		},
	} {
		t.Run(name, func(t *testing.T) {
			hc := testutil.MockHTTPClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				testutil.AssertEqual(t, r.URL.EscapedPath(), tc.want)
				testutil.AssertEqual(t, r.URL.Query().Get("rev"), "feature/a")
				testutil.AssertEqual(t, r.URL.Query().Has("path"), false)
				w.WriteHeader(http.StatusNoContent)
			}))

			if err := NewClient("https://example.test", WithHTTPClient(hc)).Browse(t.Context(), BrowseRequest{
				Repository: "group/nested/repo",
				Path:       tc.path,
				Rev:        Some("feature/a"),
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestQueryLists(t *testing.T) {
	cases := map[string]struct {
		req  QueryRequest
		want url.Values
	}{
		"omitted": {},
		"present": {
			req: QueryRequest{
				Required: []string{"a b", "c&d"},
				Labels:   Some([]string{"first", "second"}),
				Numbers:  Some([]uint32{1, 2}),
				Blobs:    Some([][]byte{{0}, {255}}),
				Ids:      Some([]ID{3, 4}),
			},
			want: url.Values{
				"required": {"a b", "c&d"},
				"labels":   {"first", "second"},
				"numbers":  {"1", "2"},
				"blobs":    {"AA==", "/w=="},
				"ids":      {"3", "4"},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			hc := testutil.MockHTTPClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				testutil.AssertEqual(t, r.URL.Query().Encode(), tc.want.Encode())
				w.WriteHeader(http.StatusNoContent)
			}))

			if err := NewClient("https://example.test", WithHTTPClient(hc)).Query(t.Context(), tc.req); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestJSON(t *testing.T) {
	var (
		choice   = `{"type":"empty"}`
		optional = "null\n"
		choices  = "null\n"
	)
	hc := testutil.MockHTTPClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/choice":
			io.WriteString(w, choice)
		case "/optional":
			io.WriteString(w, optional)
		case "/choices":
			io.WriteString(w, choices)
		case "/echo":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
				return
			}
			want := `{"record":{"choices":[],"numbers":[[]],"data":"","signed":-9223372036854775808,"unsigned":18446744073709551615}}`
			testutil.AssertEqual(t, string(body), want)
			io.WriteString(w, strings.TrimSuffix(strings.TrimPrefix(string(body), `{"record":`), "}"))
		}
	}))
	c := NewClient("https://example.test", WithHTTPClient(hc))

	got, err := c.GetChoice(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, got, Choice(&Empty{}))

	opt, err := c.GetOptional(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, opt, Option[Choice]{})

	optional = `{"type":"empty"}` + "\n"
	opt, err = c.GetOptional(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, opt, Some[Choice](&Empty{}))

	items, err := c.ListChoices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, items, Option[[][]Choice]{})

	choices = `[[{"type":"empty"}],[]]` + "\n"
	items, err = c.ListChoices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, items, Some([][]Choice{{&Empty{}}, {}}))

	rec, err := c.Echo(t.Context(), EchoRequest{
		Record: Record{
			Numbers:  [][]uint32{nil},
			Signed:   -9223372036854775808,
			Unsigned: 18446744073709551615,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, rec.Signed, int64(-9223372036854775808))
	testutil.AssertEqual(t, rec.Unsigned, uint64(18446744073709551615))
	testutil.AssertEqual(t, rec.Numbers, [][]uint32{{}})

	for name, body := range map[string]string{
		"missing":     `{}`,
		"wrong case":  `{"Type":"empty"}`,
		"numeric tag": `{"type":1}`,
		"bad fields":  `{"type":"value","label":7}`,
	} {
		t.Run(name, func(t *testing.T) {
			choice = body
			if got, err := c.GetChoice(t.Context()); err == nil {
				t.Fatalf("accepted %s: %#v", body, got)
			}
		})
	}
	choice = `{"type":"future","value":18446744073709551615,"extra":{"type":"nested"}}`
	got, err = c.GetChoice(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	unknown, ok := got.(*UnknownChoice)
	if !ok {
		t.Fatalf("choice = %T; want *UnknownChoice", got)
	}
	testutil.AssertEqual(t, unknown.Constructor, "future")

	data, err := json.Marshal(unknown)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, string(data), choice)

	data, err = json.Marshal(&Empty{})
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, string(data), `{"type":"empty"}`)

	known := &Value{
		Flag: Some(false),
	}
	data, err = json.Marshal(known)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, string(data), `{"type":"value","label":"","blob":"","tags":[],"flag":false}`)

	err = c.decodeError("oops", jsontext.Value(`{}`))
	if _, ok := errors.AsType[*Problem](err); !ok {
		t.Fatalf("declared error = %v; want *Problem", err)
	}
}
