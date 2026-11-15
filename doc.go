// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package plug implements the Plug RPC protocol.
//
// Define a service in a .plug schema and generate Go bindings with the plug command.
// Pass the generated schema to [NewServer], register the generated handlers,
// and serve a listener or connection.
// Clients wrap a [Client] from [Dial] or [NewClient] with the generated client.
//
// A Client supports concurrent calls over a shared connection.
// Call contexts control cancellation.
// For JSON routes, see [go.astrophena.name/plug/plughttp];
// for native Plug over HTTP Upgrade and WebSocket, see [go.astrophena.name/plug/plugupgrade].
//
// See [Using Plug] for a complete Go service
// and [the protocol specification] for the wire format.
//
// [Using Plug]: https://github.com/astrophena/plug/blob/master/docs/intro.md
// [the protocol specification]: https://github.com/astrophena/plug/blob/master/docs/protocol.md
package plug
