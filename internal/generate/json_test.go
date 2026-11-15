// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package generate

import (
	"bytes"
	"testing"
)

func TestJSONEmpty(t *testing.T) {
	got, err := JSON(mustParse(t, "package empty"))
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(got, []byte("null")) {
		t.Fatalf("empty collections encoded as null:\n%s", got)
	}
	for _, want := range []string{`"documentation": ""`, `"permissions": []`} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("missing JSON field %s", want)
		}
	}
}
