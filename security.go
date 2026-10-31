// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"context"
	"net"

	"go.astrophena.name/base/ctxkey"
)

// Principal is an authenticated caller identity. Attributes are
// application-defined.
type Principal struct {
	// Subject is the stable caller identifier.
	Subject string
	// Attributes contains optional identity claims used by an authorizer.
	Attributes map[string]string
}

// Authenticator derives a principal from a connection.
type Authenticator func(context.Context, net.Conn) (Principal, error)

// Authorizer decides whether principal may call method with request.
type Authorizer func(context.Context, Principal, string, MethodInfo, any) error

// Security configures authentication and authorization.
type Security struct {
	// Authenticate optionally derives a principal per Plug connection.
	Authenticate Authenticator
	// Authorize enforces every non-public method before its handler runs.
	Authorize Authorizer
}

var principalKey = ctxkey.New("plug.Principal", Principal{})

// WithPrincipal returns a context carrying principal.
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return principalKey.WithValue(ctx, principal)
}

// PrincipalFromContext returns the authenticated principal attached by a Plug
// or HTTP transport.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	return principalKey.ValueOk(ctx)
}
