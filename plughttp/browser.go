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

// NewBrowser serves schema documentation, including index.md.
// Mount it with [http.StripPrefix] and serve its assets with [StaticFS].
func NewBrowser(schema *plug.Schema) (http.Handler, error) { return browser.New(schema) }

// StaticFS combines files with browser assets.
// Earlier files take precedence.
// Pass it to [go.astrophena.name/base/web.Server.StaticFS].
func StaticFS(files ...fs.FS) fs.FS { return browser.StaticFS(files...) }
