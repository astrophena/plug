// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"bytes"
	"context"
)

func (s *Server) invoke(ctx context.Context, methodID uint32, payload []byte) frame {
	entry := s.handlers[methodID]
	result := invokeSafely(func() callResult {
		return entry.invoke(ctx, payload)
	})
	if result.enc != nil {
		payload = bytes.Clone(result.payload)
		result.release()
		result.payload = payload
	}
	result.observe(s, payloadStats{
		size: len(result.payload),
	})
	return frame{
		kind:    result.kind,
		code:    result.code,
		payload: result.payload,
	}
}
