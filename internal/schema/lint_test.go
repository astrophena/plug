// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package schema

import (
	"slices"
	"strings"
	"testing"
)

func TestLint(t *testing.T) {
	cases := map[string]struct {
		schema     string
		wantIssues []string
	}{
		"valid schema": {
			schema: `
// This schema describes a lint test service.
package test version 1

// This permission allows callers to ping the service.
permission test.ping

// This type represents a ping request identifier.
type PingID = i64

// This method pings the service.
test.ping
  // This field identifies the ping request.
  id: PingID
-> void
  requires test.ping
`,
			wantIssues: nil,
		},
		"unused permission and unused type": {
			schema: `
// This schema describes a lint test service.
package test version 1

// This permission allows callers to ping the service.
permission test.ping

// This permission allows callers to perform an unused operation.
permission test.unused

// This type represents an unreferenced identifier.
type UnusedID = i64

// This method pings the service.
test.ping -> void
  requires test.ping
`,
			wantIssues: []string{
				"permission \"test.unused\" is declared but never required by any method",
				"newtype \"UnusedID\" is declared but never referenced",
			},
		},
		"missing documentation": {
			schema: `
package test version 1

permission test.ping

test.ping -> void
  requires test.ping
`,
			wantIssues: []string{
				"method \"test.ping\" is missing a documentation comment",
				"package \"test\" is missing a documentation comment",
				"permission \"test.ping\" is missing a documentation comment",
			},
		},
		"natural documentation": {
			schema: `
// A lint test service.
package test version 1

// Permission to ping the service.
permission test.ping

// Data exchanged by the service.
category "Data"

// A ping request identifier.
type PingID = i64

// A ping response.
pong
  // The echoed identifier.
  id: PingID
= Pong

// This type represents a ping failure.
type PingError

// Represents a failed ping.
failed = PingError

// Pings the service.
test.ping
  // The ping request identifier.
  id: PingID
-> Pong
  throws PingError
  requires test.ping
`,
			wantIssues: nil,
		},
		"documentation without punctuation": {
			schema: `
// This schema describes a lint test service
package test version 1

// This method pings the service
test.ping -> void public
`,
			wantIssues: nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s, err := Parse("test.plug", []byte(tc.schema))
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			issues := Lint(s)
			for _, want := range tc.wantIssues {
				found := slices.ContainsFunc(issues, func(err error) bool {
					return strings.Contains(err.Error(), want)
				})
				if !found {
					t.Errorf("got issues %v, want to contain %q", issues, want)
				}
			}
			if len(issues) != len(tc.wantIssues) {
				t.Errorf("got %d issues, want %d: %v", len(issues), len(tc.wantIssues), issues)
			}
		})
	}
}
