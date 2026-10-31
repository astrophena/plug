// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package browser

import (
	"embed"
	"errors"
	"io/fs"
)

//go:embed static
var assets embed.FS

// StaticFS combines files with the browser assets. Earlier files take precedence.
// Pass the result to web.Server.StaticFS.
func StaticFS(files ...fs.FS) fs.FS {
	result := make(overlayFS, 0, len(files)+1)
	for _, f := range files {
		if f != nil {
			result = append(result, f)
		}
	}
	return append(result, assets)
}

type overlayFS []fs.FS

func (o overlayFS) Open(name string) (fs.File, error) {
	for _, f := range o {
		file, err := f.Open(name)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}

	return nil, &fs.PathError{
		Op:   "open",
		Path: name,
		Err:  fs.ErrNotExist,
	}
}
