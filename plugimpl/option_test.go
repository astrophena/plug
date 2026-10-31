// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plugimpl

import (
	"encoding/json/v2"
	"testing"
)

func TestOptionJSON(t *testing.T) {
	cases := map[string]struct {
		value any
		want  string
	}{
		"absent": {
			None[string](),
			"null",
		},
		"false": {
			Some(false),
			"false",
		},
		"zero": {
			Some(int64(0)),
			"0",
		},
		"empty string": {
			Some(""),
			`""`,
		},
		"nil bytes": {
			Some[[]byte](nil),
			`""`,
		},
		"nil list": {
			Some[[]string](nil),
			"[]",
		},
		"nested lists": {
			Some([][]string{nil, {}}),
			"[[],[]]",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(tc.value)
			if err != nil || string(data) != tc.want {
				t.Fatalf("Marshal() = %s, %v; want %s", data, err, tc.want)
			}
		})
	}

	data, err := json.Marshal(struct {
		Absent Option[string]   `json:"absent,omitzero"`
		Zero   Option[int64]    `json:"zero,omitzero"`
		False  Option[bool]     `json:"false,omitzero"`
		Bytes  Option[[]byte]   `json:"bytes,omitzero"`
		List   Option[[]string] `json:"list,omitzero"`
	}{
		Zero:  Some(int64(0)),
		False: Some(false),
		Bytes: Some[[]byte](nil),
		List:  Some[[]string](nil),
	})
	if err != nil || string(data) != `{"zero":0,"false":false,"bytes":"","list":[]}` {
		t.Fatalf("Marshal() = %s, %v", data, err)
	}
}
