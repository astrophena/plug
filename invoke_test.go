// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"bytes"
	"context"
	"testing"

	"go.astrophena.name/plug/plugimpl"
)

func TestInvokeBytesRemainOwned(t *testing.T) {
	const methodID = 1
	schema := Schema{
		Package: "invoke_bytes_test",
		Major:   1,
		Family:  plugimpl.FamilyFingerprint("invoke_bytes_test", 1),
		Source:  "package invoke_bytes_test version 1\n",
		Methods: []MethodInfo{
			{
				Name:   "invoke_bytes_test.copy",
				ID:     methodID,
				Public: true,
				Fields: []FieldInfo{
					{
						Name: "value",
						Type: TypeExpr{
							Kind: TypeExprPrimitive,
							Name: "bytes",
						},
					},
				},
				Response: TypeExpr{
					Kind: TypeExprPrimitive,
					Name: "bytes",
				},
			},
		},
	}
	method := plugimpl.NewMethod(&schema, methodID, plugimpl.BytesCodec, plugimpl.BytesCodec, plugimpl.ErrorCodec{})
	server, err := NewServer(&schema, ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var (
		retained      []byte
		registrations []plugimpl.Registration
	)
	if err := plugimpl.AppendHandler(&registrations, method, func(_ context.Context, value []byte) ([]byte, error) {
		if retained == nil {
			retained = value
		}
		return bytes.Clone(value), nil
	}, "Copy"); err != nil {
		t.Fatal(err)
	}
	if err := server.RegisterHandlers(registrations); err != nil {
		t.Fatal(err)
	}

	first, err := Invoke(t.Context(), server, method, []byte{1})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Invoke(t.Context(), server, method, []byte{2})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, []byte{1}) || !bytes.Equal(second, []byte{2}) {
		t.Fatalf("responses = %v, %v", first, second)
	}
	if !bytes.Equal(retained, []byte{1}) {
		t.Fatalf("retained request = %v", retained)
	}
}
