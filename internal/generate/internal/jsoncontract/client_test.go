// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package jsoncontract

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestWildcardPaths(t *testing.T) {
	for name, tc := range map[string]struct {
		path Option[Path]
		want string
	}{
		"root": {
			want: "/repos/group%2Fnested%2Frepo/contents/",
		},
		"empty": {
			path: Some(Path("")),
			want: "/repos/group%2Fnested%2Frepo/contents/",
		},
		"nested": {
			path: Some(Path("docs/a file%?.txt")),
			want: "/repos/group%2Fnested%2Frepo/contents/docs/a%20file%25%3F.txt",
		},
		"Unicode": {
			path: Some(Path("日本語/資料.txt")),
			want: "/repos/group%2Fnested%2Frepo/contents/%E6%97%A5%E6%9C%AC%E8%AA%9E/%E8%B3%87%E6%96%99.txt",
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.EscapedPath() != tc.want || r.URL.Query().Get("rev") != "feature/a" || r.URL.Query().Has("path") {
					t.Errorf("request URL = %s; want %s?rev=feature/a", r.URL.String(), tc.want)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			if err := NewClient(server.URL).Browse(t.Context(), BrowseRequest{
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
		"empty": {
			req: QueryRequest{
				Labels:  Some([]string{}),
				Numbers: Some([]uint32{}),
				Blobs:   Some([][]byte{}),
				Ids:     Some([]ID{}),
			},
		},
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
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got := r.URL.Query()
				if got.Encode() != tc.want.Encode() {
					t.Errorf("query = %v; want %v", got, tc.want)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			if err := NewClient(server.URL).Query(t.Context(), tc.req); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestJSONClient(t *testing.T) {
	var (
		choice   = `{"type":"empty"}`
		optional = "null\n"
		choices  = "null\n"
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/choice":
			io.WriteString(w, choice)
		case "/optional":
			io.WriteString(w, optional)
		case "/choices":
			io.WriteString(w, choices)
		case "/bytes":
			io.WriteString(w, `"AP8="`)
		case "/empty":
			io.WriteString(w, "[]\n")
		case "/echo":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
				return
			}
			want := `{"record":{"choices":[],"numbers":[[]],"data":"","signed":-9223372036854775808,"unsigned":18446744073709551615}}`
			if string(body) != want {
				t.Errorf("request = %s; want %s", body, want)
			}
			io.WriteString(w, strings.TrimSuffix(strings.TrimPrefix(string(body), `{"record":`), "}"))
		}
	}))
	defer server.Close()
	client := NewClient(server.URL)

	got, err := client.GetChoice(t.Context())
	if _, ok := got.(*Empty); err != nil || !ok {
		t.Fatalf("choice = %#v, %v", got, err)
	}
	gotOptional, err := client.GetOptional(t.Context())
	if err != nil || !gotOptional.IsZero() {
		t.Fatalf("optional = %#v, %v", gotOptional, err)
	}
	optional = `{"type":"empty"}` + "\n"
	gotOptional, err = client.GetOptional(t.Context())
	if got, ok := gotOptional.Value(); err != nil || !ok {
		t.Fatalf("present optional = %#v, %v", gotOptional, err)
	} else if _, ok := got.(*Empty); !ok {
		t.Fatalf("present optional = %#v", got)
	}
	gotChoices, err := client.ListChoices(t.Context())
	if err != nil || !gotChoices.IsZero() {
		t.Fatalf("choices = %#v, %v", gotChoices, err)
	}
	choices = `[[{"type":"empty"}],[]]` + "\n"
	gotChoices, err = client.ListChoices(t.Context())
	if items, ok := gotChoices.Value(); err != nil || !ok || len(items) != 2 || len(items[0]) != 1 || len(items[1]) != 0 {
		t.Fatalf("present choices = %#v, %v", gotChoices, err)
	}
	data, err := client.GetBytes(t.Context())
	if err != nil || string(data) != string([]byte{0, 255}) {
		t.Fatalf("bytes = %v, %v", data, err)
	}
	empty, err := client.ListEmpty(t.Context())
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty = %#v, %v", empty, err)
	}
	record, err := client.Echo(t.Context(), EchoRequest{
		Record: Record{
			Numbers:  [][]uint32{nil},
			Signed:   -9223372036854775808,
			Unsigned: 18446744073709551615,
		},
	})
	if err != nil || record.Signed != -9223372036854775808 || record.Unsigned != 18446744073709551615 || len(record.Numbers) != 1 || record.Numbers[0] == nil {
		t.Fatalf("record = %#v, %v", record, err)
	}

	for name, body := range map[string]string{
		"missing":     `{}`,
		"legacy":      `{"_":"empty"}`,
		"wrong case":  `{"Type":"empty"}`,
		"null tag":    `{"type":null}`,
		"empty tag":   `{"type":""}`,
		"numeric tag": `{"type":1}`,
		"bad fields":  `{"type":"value","label":7}`,
	} {
		t.Run(name, func(t *testing.T) {
			choice = body
			if got, err := client.GetChoice(t.Context()); err == nil {
				t.Fatalf("accepted %s: %#v", body, got)
			}
		})
	}
	choice = `{"type":"future","value":18446744073709551615,"extra":{"type":"nested"}}`
	got, err = client.GetChoice(t.Context())
	unknown, ok := got.(*UnknownChoice)
	if err != nil || !ok || unknown.Constructor != "future" {
		t.Fatalf("unknown = %#v, %v", got, err)
	}
	encoded, err := json.Marshal(unknown)
	if err != nil || string(encoded) != choice {
		t.Fatalf("forwarded unknown = %s, %v; want %s", encoded, err, choice)
	}
	encoded, err = json.Marshal(&Empty{})
	if err != nil || string(encoded) != `{"type":"empty"}` {
		t.Fatalf("fieldless constructor = %s, %v", encoded, err)
	}
	known := &Value{
		Flag: Some(false),
	}
	encoded, err = json.Marshal(known)
	if err != nil || string(encoded) != `{"type":"value","label":"","blob":"","tags":[],"flag":false}` {
		t.Fatalf("payload = %s, %v", encoded, err)
	}
	if got := client.decodeError("oops", jsontext.Value(`{}`)); got == nil {
		t.Fatal("empty declared error was not decoded")
	} else {
		if _, ok := errors.AsType[*Problem](got); !ok {
			t.Fatalf("declared error = %T", got)
		}
	}
}
