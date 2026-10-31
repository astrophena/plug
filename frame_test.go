// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package plug

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
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

func TestCompressedFrameSizeLimit(t *testing.T) {
	payload, err := compressFramePayload(bytes.Repeat([]byte("a"), compressionThreshold))
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
