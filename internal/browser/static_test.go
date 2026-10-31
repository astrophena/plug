// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package browser_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"go.astrophena.name/plug/internal/browser"
)

func TestStaticFS(t *testing.T) {
	applicationStaticFS := fstest.MapFS{
		"static/css/plug-schema.css": &fstest.MapFile{
			Data: []byte("application override"),
		},
		"static/css/application.css": &fstest.MapFile{
			Data: []byte("application asset"),
		},
	}
	staticFS := browser.StaticFS(applicationStaticFS)
	cases := map[string]struct {
		path string
		want string
	}{
		"application asset": {
			path: "static/css/application.css",
			want: "application asset",
		},
		"application takes precedence": {
			path: "static/css/plug-schema.css",
			want: "application override",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			contents, err := fs.ReadFile(staticFS, tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if string(contents) != tc.want {
				t.Fatalf("contents = %q, want %q", contents, tc.want)
			}
		})
	}
}

func TestStaticFSIncludesBrowserFavicon(t *testing.T) {
	contents, err := fs.ReadFile(browser.StaticFS(), "static/icons/plug.svg")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "<svg") {
		t.Fatal("browser favicon is not an SVG")
	}
}
