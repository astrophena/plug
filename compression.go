// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync"

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

var zstdFrames = sync.OnceValues(func() (*frameCompression, error) {
	encoder, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.SpeedFastest),
		zstd.WithWindowSize(zstdWindowSize),
	)
	if err != nil {
		return nil, err
	}
	decoder, err := zstd.NewReader(nil,
		zstd.WithDecodeAllCapLimit(true),
		zstd.WithDecoderMaxMemory(math.MaxUint32),
		zstd.WithDecoderMaxWindow(zstdWindowSize),
	)
	if err != nil {
		return nil, err
	}
	return &frameCompression{
		encoder: encoder,
		decoder: decoder,
	}, nil
})

func compressFramePayload(payload []byte) ([]byte, error) {
	if len(payload) < compressionThreshold {
		return nil, nil
	}
	compression, err := zstdFrames()
	if err != nil {
		return nil, fmt.Errorf("plug: initialize zstd: %w", err)
	}
	// A nonempty destination avoids zstd's input-sized allocation.
	result := binary.AppendUvarint(nil, uint64(len(payload)))
	result = compression.encoder.EncodeAll(payload, result)
	if len(result) >= len(payload) {
		return nil, nil
	}
	return result, nil
}

func decompressFramePayload(payload []byte, maximum uint64) ([]byte, error) {
	size, n := binary.Uvarint(payload)
	if n <= 0 {
		return nil, fmt.Errorf("plug: invalid compressed payload size")
	}
	if size == 0 || size > maximum {
		return nil, fmt.Errorf("plug: decompressed payload size %d exceeds maximum %d", size, maximum)
	}
	compression, err := zstdFrames()
	if err != nil {
		return nil, fmt.Errorf("plug: initialize zstd: %w", err)
	}
	result, err := compression.decoder.DecodeAll(payload[n:], make([]byte, 0, int(size)))
	if err != nil {
		return nil, fmt.Errorf("plug: decompress frame: %w", err)
	}
	if len(result) != int(size) {
		return nil, fmt.Errorf("plug: decompressed payload has %d bytes, want %d", len(result), size)
	}
	return result, nil
}
