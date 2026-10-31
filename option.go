// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import "go.astrophena.name/plug/plugimpl"

// Option holds a value that may be absent. Its zero value is [None].
type Option[T any] = plugimpl.Option[T]

// Some returns an [Option] containing value.
func Some[T any](value T) Option[T] { return plugimpl.Some(value) }

// None returns an absent Option.
func None[T any]() Option[T] { return plugimpl.None[T]() }
