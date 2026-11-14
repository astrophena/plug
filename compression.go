// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/klauspost/compress/zstd"
)

const (
	compressionThreshold = 1 << 10
	zstdWindowSize       = 1 << 20
)

type frameCompression struct {
	encoder *zstd.Encoder
	decoder *zstd.Decoder
}

func (c *frameCompression) compress(dst, payload []byte) ([]byte, error) {
	if len(payload) < compressionThreshold {
		return dst[:0], nil
	}
	if c.encoder == nil {
		encoder, err := zstd.NewWriter(nil,
			zstd.WithEncoderLevel(zstd.SpeedFastest),
			zstd.WithWindowSize(zstdWindowSize),
			zstd.WithEncoderConcurrency(1),
		)
		if err != nil {
			return nil, fmt.Errorf("plug: initialize zstd: %w", err)
		}
		c.encoder = encoder
	}
	// A nonempty destination avoids zstd's input-sized allocation.
	dst = binary.AppendUvarint(dst[:0], uint64(len(payload)))
	return c.encoder.EncodeAll(payload, dst), nil
}

func (c *frameCompression) decompress(payload []byte, maximum uint64) ([]byte, error) {
	size, n := binary.Uvarint(payload)
	if n <= 0 {
		return nil, fmt.Errorf("plug: invalid compressed payload size")
	}
	if size == 0 || size > maximum {
		return nil, fmt.Errorf("plug: decompressed payload size %d exceeds maximum %d", size, maximum)
	}
	if c.decoder == nil {
		decoder, err := zstd.NewReader(nil,
			zstd.WithDecodeAllCapLimit(true),
			zstd.WithDecoderMaxMemory(math.MaxUint32),
			zstd.WithDecoderMaxWindow(zstdWindowSize),
			zstd.WithDecoderConcurrency(1),
		)
		if err != nil {
			return nil, fmt.Errorf("plug: initialize zstd: %w", err)
		}
		c.decoder = decoder
	}
	result, err := c.decoder.DecodeAll(payload[n:], make([]byte, 0, int(size)))
	if err != nil {
		return nil, fmt.Errorf("plug: decompress frame: %w", err)
	}
	if len(result) != int(size) {
		return nil, fmt.Errorf("plug: decompressed payload has %d bytes, want %d", len(result), size)
	}
	return result, nil
}
