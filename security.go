// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"context"
	"net"

	"go.astrophena.name/base/ctxkey"
)

// Principal identifies an authenticated caller.
type Principal struct {
	// Subject is the caller's stable identifier.
	Subject string
	// Attributes holds application-defined identity claims.
	Attributes map[string]string
}

// Authenticator derives a principal from a connection.
type Authenticator func(context.Context, net.Conn) (Principal, error)

// Authorizer checks a caller's permission for a method and request.
type Authorizer func(context.Context, Principal, string, MethodInfo, any) error

// Security configures authentication and authorization.
type Security struct {
	// Authenticate identifies each binary connection's caller, if set.
	Authenticate Authenticator
	// Authorize checks permissions for non-public methods.
	Authorize Authorizer
}

var principalKey = ctxkey.New("plug.Principal", Principal{})

// WithPrincipal returns a context carrying principal.
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return principalKey.WithValue(ctx, principal)
}

// PrincipalFromContext returns the principal stored in ctx.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	return principalKey.ValueOk(ctx)
}
