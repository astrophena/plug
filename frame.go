// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
)

const (
	minimumFrameSize        = 1 + 1
	minimumRequestFrameSize = 1 + 1 + 4
	maxRetainedBufferSize   = 64 * 1024
	compressedFrameFlag     = 1 << 7
)

type frameKind uint8

const (
	frameRequest frameKind = iota + 1
	frameResponse
	frameApplicationError
	frameProtocolError
	frameCancel
)

type frame struct {
	kind      frameKind
	requestID uint64
	code      uint32
	payload   []byte
}

type payloadStats struct {
	size           int
	compressed     bool
	compressedSize int
}

// framer reads and writes frames for a connection.
type framer struct {
	r        io.Reader
	w        io.Writer
	limits   Limits
	compress bool

	mu   sync.Mutex
	werr error
	wbuf []byte
}

func newFramer(w io.Writer, r io.Reader, limits Limits, compress bool) *framer {
	return &framer{
		r:        r,
		w:        w,
		limits:   limits,
		compress: compress,
		wbuf:     make([]byte, 0, 256),
	}
}

type streamByteReader struct {
	r io.Reader
}

func (r *streamByteReader) ReadByte() (byte, error) {
	var b [1]byte
	_, err := io.ReadFull(r.r, b[:])
	return b[0], err
}

func readStreamUvarint(r io.Reader) (uint64, error) {
	if br, ok := r.(io.ByteReader); ok {
		return binary.ReadUvarint(br)
	}
	return binary.ReadUvarint(&streamByteReader{
		r: r,
	})
}

func readFrameUvarint(r io.ByteReader, n *uint64) (uint64, error) {
	var v uint64
	for i := range binary.MaxVarintLen64 {
		if *n == 0 {
			return 0, io.ErrUnexpectedEOF
		}
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		*n--
		if b < 0x80 {
			if i == binary.MaxVarintLen64-1 && b > 1 {
				return 0, fmt.Errorf("varint overflows 64 bits")
			}
			return v | uint64(b)<<uint(i*7), nil
		}
		v |= uint64(b&0x7f) << uint(i*7)
	}
	return 0, fmt.Errorf("varint overflows 64 bits")
}

func (f *framer) readFrame() (frame, error) {
	n, err := readStreamUvarint(f.r)
	if err != nil {
		return frame{}, fmt.Errorf("plug: read frame size: %w", err)
	}
	if n < minimumFrameSize {
		return frame{}, fmt.Errorf("plug: frame size %d is smaller than minimum %d", n, minimumFrameSize)
	}
	if n > uint64(f.limits.MaxFrameSize) {
		return frame{}, fmt.Errorf("plug: frame size %d exceeds maximum %d", n, f.limits.MaxFrameSize)
	}

	br, ok := f.r.(io.ByteReader)
	if !ok {
		br = &streamByteReader{
			r: f.r,
		}
	}
	remaining := n
	b, err := br.ReadByte()
	if err != nil {
		return frame{}, err
	}
	remaining--
	compressed := b&compressedFrameFlag != 0
	if compressed && !f.compress {
		return frame{}, fmt.Errorf("plug: compressed frame was not negotiated")
	}
	kind := frameKind(b &^ compressedFrameFlag)
	switch kind {
	case frameRequest, frameResponse, frameApplicationError, frameProtocolError, frameCancel:
	default:
		return frame{}, fmt.Errorf("plug: invalid frame kind %d", kind)
	}
	id, err := readFrameUvarint(br, &remaining)
	if err != nil {
		return frame{}, fmt.Errorf("plug: read frame request ID: %w", err)
	}
	v := frame{
		kind:      kind,
		requestID: id,
	}

	switch v.kind {
	case frameRequest, frameApplicationError:
		if remaining < 4 {
			return frame{}, io.ErrUnexpectedEOF
		}
		var code [4]byte
		if _, err := io.ReadFull(f.r, code[:]); err != nil {
			return frame{}, err
		}
		v.code = binary.LittleEndian.Uint32(code[:])
		remaining -= 4
	case frameProtocolError:
		if remaining < 1 {
			return frame{}, io.ErrUnexpectedEOF
		}
		code, err := br.ReadByte()
		if err != nil {
			return frame{}, err
		}
		v.code = uint32(code)
		remaining--
	case frameResponse, frameCancel:
	}
	v.payload = make([]byte, int(remaining))
	if _, err := io.ReadFull(f.r, v.payload); err != nil {
		return frame{}, err
	}
	if compressed {
		headerSize := n - remaining
		v.payload, err = decompressFramePayload(v.payload, uint64(f.limits.MaxFrameSize)-headerSize)
		if err != nil {
			return frame{}, err
		}
	}
	return v, nil
}

func (f *framer) writeFrame(v frame) error {
	_, err := f.writeFrameWithStats(v)
	return err
}

func (f *framer) writeFrameWithStats(v frame) (payloadStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.werr != nil {
		return payloadStats{size: len(v.payload)}, f.werr
	}

	stats, err := f.writeFrameLocked(v)
	f.werr = err
	return stats, err
}

func (f *framer) writeRequest(requestID uint64, methodID uint32, payload []byte) error {
	return f.writeFrame(frame{
		kind:      frameRequest,
		requestID: requestID,
		code:      methodID,
		payload:   payload,
	})
}

func (f *framer) writeResponse(requestID uint64, payload []byte) error {
	return f.writeFrame(frame{
		kind:      frameResponse,
		requestID: requestID,
		payload:   payload,
	})
}

func (f *framer) writeResult(requestID uint64, kind frameKind, code uint32, payload []byte) (payloadStats, error) {
	return f.writeFrameWithStats(frame{
		kind:      kind,
		requestID: requestID,
		code:      code,
		payload:   payload,
	})
}

func (f *framer) writeProtocolError(requestID uint64, code ErrorCode, message string) error {
	return f.writeFrame(frame{
		kind:      frameProtocolError,
		requestID: requestID,
		code:      uint32(code),
		payload:   []byte(strings.ToValidUTF8(message, "")),
	})
}

func (f *framer) writeCancel(requestID uint64) error {
	return f.writeFrame(frame{
		kind:      frameCancel,
		requestID: requestID,
	})
}

func (f *framer) writeFrameLocked(v frame) (payloadStats, error) {
	stats := payloadStats{
		size: len(v.payload),
	}
	codeSize := 0
	switch v.kind {
	case frameRequest, frameApplicationError:
		codeSize = 4
	case frameProtocolError:
		if v.code > math.MaxUint8 {
			return stats, fmt.Errorf("plug: core error code %d exceeds one byte", v.code)
		}
		codeSize = 1
	case frameResponse, frameCancel:
		if v.code != 0 {
			return stats, fmt.Errorf("plug: frame kind %d has nonzero code %d", v.kind, v.code)
		}
	default:
		return stats, fmt.Errorf("plug: invalid frame kind %d", v.kind)
	}

	var requestID [binary.MaxVarintLen64]byte
	requestIDSize := binary.PutUvarint(requestID[:], v.requestID)
	headerSize := 1 + requestIDSize + codeSize
	logicalSize := uint64(headerSize + len(v.payload))
	if logicalSize > uint64(f.limits.MaxFrameSize) {
		return stats, fmt.Errorf("plug: frame size %d exceeds maximum %d", logicalSize, f.limits.MaxFrameSize)
	}

	kind := byte(v.kind)
	payload := v.payload
	if f.compress {
		compressed, err := compressFramePayload(payload)
		if err != nil {
			return stats, err
		}
		if compressed != nil {
			kind |= compressedFrameFlag
			payload = compressed
			stats.compressed = true
			stats.compressedSize = len(compressed)
		}
	}
	size := uint64(headerSize + len(payload))

	f.wbuf = f.wbuf[:0]
	defer func() {
		if cap(f.wbuf) > maxRetainedBufferSize {
			f.wbuf = nil
		}
	}()
	f.wbuf = binary.AppendUvarint(f.wbuf, size)
	f.wbuf = append(f.wbuf, kind)
	f.wbuf = append(f.wbuf, requestID[:requestIDSize]...)
	switch v.kind {
	case frameRequest, frameApplicationError:
		f.wbuf = binary.LittleEndian.AppendUint32(f.wbuf, v.code)
	case frameProtocolError:
		f.wbuf = append(f.wbuf, byte(v.code))
	}
	f.wbuf = append(f.wbuf, payload...)

	return stats, writeAll(f.w, f.wbuf)
}

func writeAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}
