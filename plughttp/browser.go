// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plughttp

import (
	"io/fs"
	"net/http"

	"go.astrophena.name/plug"
	"go.astrophena.name/plug/internal/browser"
)

// NewBrowser returns a schema browser with HTML pages and an index.md download.
// Mount it with [http.StripPrefix] and serve its assets with [web.Server] and
// [StaticFS]:
//
//	mux.Handle("/docs/", http.StripPrefix("/docs", browser))
//
// It reports invalid schemas and categories as errors.
func NewBrowser(schema *plug.Schema) (http.Handler, error) { return browser.New(schema) }

// StaticFS combines files with the browser assets. Earlier files take precedence.
// Pass the result to [web.Server.StaticFS].
func StaticFS(files ...fs.FS) fs.FS { return browser.StaticFS(files...) }
