// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math/rand"
	"net"
	"strings"
	"testing"
	"time"
)

func TestFrameRoundTripWithShortWrites(t *testing.T) {
	reader, rawWriter := net.Pipe()
	writer := shortWriteConn{
		Conn: rawWriter,
	}
	written := make(chan error, 1)
	go func() {
		written <- newFramer(writer, nil, Limits{
			MaxFrameSize: 1024,
		}, false).writeFrame(frame{
			kind:      frameRequest,
			requestID: 42,
			code:      99,
			payload:   []byte("payload"),
		})
	}()
	got, err := newFramer(nil, reader, Limits{
		MaxFrameSize: 1024,
	}, false).readFrame()
	if err != nil {
		t.Fatal(err)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if got.kind != frameRequest || got.requestID != 42 || got.code != 99 || string(got.payload) != "payload" {
		t.Fatalf("frame = %#v", got)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rawWriter.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFrameSizeValidation(t *testing.T) {
	cases := map[string]struct {
		data []byte
		max  uint32
		want string
	}{
		"smaller than minimum": {
			data: []byte{minimumFrameSize - 1},
			max:  1024,
			want: "smaller than minimum",
		},
		"over limit": {
			data: binary.AppendUvarint(nil, 1025),
			max:  1024,
			want: "exceeds maximum",
		},
		"truncated request ID": {
			data: []byte{2, byte(frameRequest), 0x80},
			max:  1024,
			want: "unexpected EOF",
		},
		"invalid kind": {
			data: []byte{2, 99, 1},
			max:  1024,
			want: "invalid frame kind",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := newFramer(nil, &bufferConn{
				Reader: bytes.NewReader(tc.data),
			}, Limits{
				MaxFrameSize: tc.max,
			}, false).readFrame()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestFrameGoldenEncoding(t *testing.T) {
	cases := map[string]struct {
		frame frame
		want  []byte
	}{
		"request": {
			frame: frame{
				kind:      frameRequest,
				requestID: 1,
				code:      0x12345678,
				payload:   []byte{'x'},
			},
			want: []byte{7, 1, 1, 0x78, 0x56, 0x34, 0x12, 'x'},
		},
		"empty request": {
			frame: frame{
				kind:      frameRequest,
				requestID: 1,
				code:      1,
			},
			want: []byte{6, 1, 1, 1, 0, 0, 0},
		},
		"response": {
			frame: frame{
				kind:      frameResponse,
				requestID: 1,
			},
			want: []byte{2, 2, 1},
		},
		"application error": {
			frame: frame{
				kind:      frameApplicationError,
				requestID: 1,
				code:      0x12345678,
			},
			want: []byte{6, 3, 1, 0x78, 0x56, 0x34, 0x12},
		},
		"protocol error": {
			frame: frame{
				kind:      frameProtocolError,
				requestID: 1,
				code:      uint32(ErrorInternal),
				payload:   []byte{'x'},
			},
			want: []byte{4, 4, 1, byte(ErrorInternal), 'x'},
		},
		"cancel": {
			frame: frame{
				kind:      frameCancel,
				requestID: 1,
			},
			want: []byte{2, 5, 1},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			conn := new(memoryConn)
			if err := newFramer(conn, nil, Limits{
				MaxFrameSize: 1024,
			}, false).writeFrame(tc.frame); err != nil {
				t.Fatal(err)
			}
			if got := conn.Bytes(); !bytes.Equal(got, tc.want) {
				t.Fatalf("encoded frame = %x, want %x", got, tc.want)
			}
			got, err := newFramer(nil, bytes.NewReader(tc.want), Limits{
				MaxFrameSize: 1024,
			}, false).readFrame()
			if err != nil {
				t.Fatal(err)
			}
			if got.kind != tc.frame.kind || got.requestID != tc.frame.requestID || got.code != tc.frame.code || !bytes.Equal(got.payload, tc.frame.payload) {
				t.Fatalf("decoded frame = %#v, want %#v", got, tc.frame)
			}
		})
	}
}

func TestFrameCompression(t *testing.T) {
	payload := bytes.Repeat([]byte("compress me "), 200)
	written := new(memoryConn)
	limits := Limits{
		MaxFrameSize: 4096,
	}
	stats, err := newFramer(written, nil, limits, true).writeFrameWithStats(frame{
		kind:      frameRequest,
		requestID: 1,
		code:      2,
		payload:   payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.size != len(payload) || !stats.compressed {
		t.Errorf("payload stats = %+v, want size %d and compression", stats, len(payload))
	}
	if stats.compressedSize == 0 || stats.compressedSize >= stats.size {
		t.Errorf("compressed payload has %d bytes, original payload has %d", stats.compressedSize, stats.size)
	}

	encoded := written.Bytes()
	_, prefixSize := binary.Uvarint(encoded)
	if prefixSize <= 0 || encoded[prefixSize]&compressedFrameFlag == 0 {
		t.Fatalf("frame is not compressed: %x", encoded)
	}
	got, err := newFramer(nil, bytes.NewReader(encoded), limits, true).readFrame()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.payload, payload) {
		t.Fatalf("payload has %d bytes, want %d", len(got.payload), len(payload))
	}
	if _, err := newFramer(nil, bytes.NewReader(encoded), limits, false).readFrame(); err == nil || !strings.Contains(err.Error(), "not negotiated") {
		t.Fatalf("unnegotiated compression error = %v", err)
	}
}

func TestReadEmptyFrameAllocs(t *testing.T) {
	for name, value := range map[string]frame{
		"request":           {kind: frameRequest, requestID: 128, code: 42},
		"response":          {kind: frameResponse, requestID: 128},
		"application error": {kind: frameApplicationError, requestID: 128, code: 42},
		"protocol error":    {kind: frameProtocolError, requestID: 128, code: uint32(ErrorInternal)},
		"cancel":            {kind: frameCancel, requestID: 128},
	} {
		t.Run(name, func(t *testing.T) {
			var encoded bytes.Buffer
			limits := Limits{MaxFrameSize: 1024}
			if err := newFramer(&encoded, nil, limits, false).writeFrame(value); err != nil {
				t.Fatal(err)
			}

			var reader bytes.Reader
			f := newFramer(nil, struct{ io.Reader }{&reader}, limits, false)
			allocs := testing.AllocsPerRun(100, func() {
				reader.Reset(encoded.Bytes())
				if _, err := f.readFrame(); err != nil {
					t.Fatal(err)
				}
			})
			if allocs != 0 {
				t.Fatalf("readFrame allocated %g times; want 0", allocs)
			}
		})
	}
}

func TestFramePayloadOwnership(t *testing.T) {
	noise := make([]byte, compressionThreshold)
	if _, err := rand.New(rand.NewSource(1)).Read(noise); err != nil {
		t.Fatal(err)
	}
	payloads := [][]byte{
		bytes.Repeat([]byte("first "), 200),
		noise,
		bytes.Repeat([]byte("second "), 200),
		[]byte("last"),
		nil,
	}
	for name, compress := range map[string]bool{"plain": false, "compressed": true} {
		t.Run(name, func(t *testing.T) {
			var encoded bytes.Buffer
			limits := Limits{MaxFrameSize: 4096}
			writer := newFramer(&encoded, nil, limits, compress)
			for i, payload := range payloads {
				stats, err := writer.writeFrameWithStats(frame{
					kind:      frameRequest,
					requestID: uint64(i + 1),
					code:      uint32(i),
					payload:   payload,
				})
				if err != nil {
					t.Fatal(err)
				}
				wantCompressed := compress && (i == 0 || i == 2)
				if stats.compressed != wantCompressed {
					t.Fatalf("frame %d compressed = %t; want %t", i, stats.compressed, wantCompressed)
				}
			}

			reader := newFramer(nil, bytes.NewReader(encoded.Bytes()), limits, compress)
			frames := make([]frame, len(payloads))
			for i := range frames {
				var err error
				frames[i], err = reader.readFrame()
				if err != nil {
					t.Fatal(err)
				}
			}
			for i, fr := range frames {
				if fr.requestID != uint64(i+1) || fr.code != uint32(i) || !bytes.Equal(fr.payload, payloads[i]) {
					t.Fatalf("frame %d changed after reading later frames", i)
				}
			}
		})
	}
}

func TestFrameBufferRetention(t *testing.T) {
	noise := make([]byte, 2*maxRetainedBufferSize)
	if _, err := rand.New(rand.NewSource(1)).Read(noise); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat(noise, 2)
	var encoded bytes.Buffer
	limits := Limits{MaxFrameSize: 1 << 20}
	writer := newFramer(&encoded, nil, limits, true)
	stats, err := writer.writeFrameWithStats(frame{
		kind:      frameResponse,
		requestID: 1,
		payload:   payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !stats.compressed || stats.compressedSize <= maxRetainedBufferSize {
		t.Fatalf("payload stats = %+v; want compressed data larger than retained buffer limit", stats)
	}
	if cap(writer.wbuf) > maxRetainedBufferSize || cap(writer.cbuf) > maxRetainedBufferSize {
		t.Fatal("writer retained a large buffer")
	}

	reader := newFramer(nil, bytes.NewReader(encoded.Bytes()), limits, true)
	got, err := reader.readFrame()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.payload, payload) {
		t.Fatal("decoded payload differs from input")
	}
	if cap(reader.rbuf) > maxRetainedBufferSize {
		t.Fatal("reader retained a large buffer")
	}
}

func TestCompressedFrameSizeLimit(t *testing.T) {
	payload, err := compressFramePayload(nil, bytes.Repeat([]byte("a"), compressionThreshold))
	if err != nil {
		t.Fatal(err)
	}
	_, prefixSize := binary.Uvarint(payload)
	oversized := binary.AppendUvarint(nil, 1025)
	oversized = append(oversized, payload[prefixSize:]...)
	if _, err := decompressFramePayload(oversized, 1024); err == nil || !strings.Contains(err.Error(), "exceeds maximum") {
		t.Fatalf("size limit error = %v", err)
	}
}

func TestFramerWriteErrorIsSticky(t *testing.T) {
	want := errors.New("write failed")
	writer := &failingWriter{
		err: want,
	}
	framer := newFramer(writer, nil, Limits{
		MaxFrameSize: 1024,
	}, false)

	if err := framer.writeResponse(1, nil); err != want {
		t.Fatalf("first write error = %v, want %v", err, want)
	}
	if err := framer.writeResponse(2, nil); err != want {
		t.Fatalf("second write error = %v, want sticky error %v", err, want)
	}
	if writer.calls != 1 {
		t.Fatalf("writer called %d times after terminal error, want 1", writer.calls)
	}
}

func FuzzReadFrame(f *testing.F) {
	f.Add([]byte{6, 1, 1, 1, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if _, err := newFramer(nil, &bufferConn{
			Reader: bytes.NewReader(data),
		}, Limits{
			MaxFrameSize: 1024,
		}, false).readFrame(); err != nil {
			return
		}
	})
}

type shortWriteConn struct {
	net.Conn
}

func (c shortWriteConn) Write(data []byte) (int, error) {
	if len(data) > 1 {
		data = data[:1]
	}
	return c.Conn.Write(data)
}

type failingWriter struct {
	err   error
	calls int
}

func (w *failingWriter) Write([]byte) (int, error) {
	w.calls++
	return 0, w.err
}

type bufferConn struct {
	io.Reader
}

func (c *bufferConn) Read(data []byte) (int, error)  { return c.Reader.Read(data) }
func (*bufferConn) Write(data []byte) (int, error)   { return len(data), nil }
func (*bufferConn) Close() error                     { return nil }
func (*bufferConn) LocalAddr() net.Addr              { return testAddr("local") }
func (*bufferConn) RemoteAddr() net.Addr             { return testAddr("remote") }
func (*bufferConn) SetDeadline(time.Time) error      { return nil }
func (*bufferConn) SetReadDeadline(time.Time) error  { return nil }
func (*bufferConn) SetWriteDeadline(time.Time) error { return nil }

type memoryConn struct {
	bytes.Buffer
}

func (*memoryConn) Close() error                     { return nil }
func (*memoryConn) LocalAddr() net.Addr              { return testAddr("local") }
func (*memoryConn) RemoteAddr() net.Addr             { return testAddr("remote") }
func (*memoryConn) SetDeadline(time.Time) error      { return nil }
func (*memoryConn) SetReadDeadline(time.Time) error  { return nil }
func (*memoryConn) SetWriteDeadline(time.Time) error { return nil }

type testAddr string

func (a testAddr) Network() string { return "test" }
func (a testAddr) String() string  { return string(a) }
