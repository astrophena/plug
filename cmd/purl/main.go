// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package main

import (
	"go.astrophena.name/base/cli"
	"go.astrophena.name/plug/internal/purl"
)

func main() { cli.Main(new(purl.Command)) }
