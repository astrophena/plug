// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package names

import "testing"

func TestTypeScriptMethod(t *testing.T) {
	cases := map[string]struct {
		in   string
		want string
	}{
		"simple": {
			in:   "greeter.getUser",
			want: "getUser",
		},
		"ping": {
			in:   "greeter.ping",
			want: "ping",
		},
		"initialism": {
			in:   "service.id",
			want: "id",
		},
		"qr": {
			in:   "service.qrCode",
			want: "qrCode",
		},
		"sql": {
			in:   "service.sqlExec",
			want: "sqlExec",
		},
		"trailing init": {
			in:   "service.getUserID",
			want: "getUserID",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := TypeScriptMethod(tc.in)
			if got != tc.want {
				t.Fatalf("TypeScriptMethod(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
