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
	"strings"
	"testing"

	"go.astrophena.name/base/testutil"
)

func TestFrameEncoding(t *testing.T) {
	cases := map[string]struct {
		frame frame
		wire  []byte
	}{
		"request": {
			frame{
				kind:      frameRequest,
				requestID: 1,
				code:      0x12345678,
				payload:   []byte("x"),
			},
			[]byte{7, 1, 1, 0x78, 0x56, 0x34, 0x12, 'x'},
		},
		"empty request": {
			frame{
				kind:      frameRequest,
				requestID: 1,
				code:      1,
			},
			[]byte{6, 1, 1, 1, 0, 0, 0},
		},
		"response": {
			frame{
				kind:      frameResponse,
				requestID: 1,
			},
			[]byte{2, 2, 1},
		},
		"multibyte ID": {
			frame{
				kind:      frameResponse,
				requestID: 128,
			},
			[]byte{3, 2, 0x80, 1},
		},
		"application error": {
			frame{
				kind:      frameApplicationError,
				requestID: 1,
				code:      0x12345678,
			},
			[]byte{6, 3, 1, 0x78, 0x56, 0x34, 0x12},
		},
		"protocol error": {
			frame{
				kind:      frameProtocolError,
				requestID: 1,
				code:      uint32(ErrorInternal),
				payload:   []byte("x"),
			},
			[]byte{4, 4, 1, 2, 'x'},
		},
		"cancel": {
			frame{
				kind:      frameCancel,
				requestID: 1,
			},
			[]byte{2, 5, 1},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var (
				buf bytes.Buffer
				w   = newFramer(shortWriter{
					&buf,
				}, nil, Limits{
					MaxFrameSize: 1024,
				}, false)
			)

			if err := w.writeFrame(tc.frame); err != nil {
				t.Fatal(err)
			}
			testutil.AssertEqual(t, buf.Bytes(), tc.wire)

			got, err := newFramer(nil, bytes.NewReader(tc.wire), Limits{
				MaxFrameSize: 1024,
			}, false).readFrame()
			if err != nil {
				t.Fatal(err)
			}
			testutil.AssertEqual(t, got.kind, tc.frame.kind)
			testutil.AssertEqual(t, got.requestID, tc.frame.requestID)
			testutil.AssertEqual(t, got.code, tc.frame.code)
			testutil.AssertEqual(t, string(got.payload), string(tc.frame.payload))
		})
	}
}

func TestFrameValidation(t *testing.T) {
	for name, tc := range map[string]struct {
		wire []byte
		want string
	}{
		"too small": {
			[]byte{minimumFrameSize - 1},
			"smaller than minimum",
		},
		"over limit": {
			binary.AppendUvarint(nil, 1025),
			"exceeds maximum",
		},
		"truncated ID": {
			[]byte{2, byte(frameRequest), 0x80},
			"unexpected EOF",
		},
		"invalid kind": {
			[]byte{2, 99, 1},
			"invalid frame kind",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newFramer(nil, bytes.NewReader(tc.wire), Limits{
				MaxFrameSize: 1024,
			}, false).readFrame()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("readFrame() = %v; want %q", err, tc.want)
			}
		})
	}
}

func TestFrameCompression(t *testing.T) {
	var (
		msg = bytes.Repeat([]byte("compress me "), 200)
		buf bytes.Buffer
		lim = Limits{
			MaxFrameSize: 4096,
		}
		w = newFramer(&buf, nil, lim, true)
	)
	defer w.close()

	stats, err := w.writeFrameWithStats(frame{
		kind:      frameRequest,
		requestID: 1,
		code:      2,
		payload:   msg,
	})
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, stats.size, len(msg))
	testutil.AssertEqual(t, stats.compressed, true)
	if stats.compressedSize <= 0 || stats.compressedSize >= stats.size {
		t.Fatalf("compression stats = %+v", stats)
	}
	r := newFramer(nil, bytes.NewReader(buf.Bytes()), lim, true)
	defer r.close()
	got, err := r.readFrame()
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertEqual(t, got.payload, msg)

	_, err = newFramer(nil, bytes.NewReader(buf.Bytes()), lim, false).readFrame()
	if err == nil || !strings.Contains(err.Error(), "not negotiated") {
		t.Fatalf("unnegotiated compression = %v", err)
	}
}

func TestFramePayloadOwnership(t *testing.T) {
	noise := make([]byte, compressionThreshold)
	if _, err := rand.New(rand.NewSource(1)).Read(noise); err != nil {
		t.Fatal(err)
	}
	data := [][]byte{bytes.Repeat([]byte("first "), 200), noise, bytes.Repeat([]byte("second "), 200), []byte("last"), {}}
	for name, compress := range map[string]bool{"plain": false, "compressed": true} {
		t.Run(name, func(t *testing.T) {
			var (
				buf bytes.Buffer
				lim = Limits{
					MaxFrameSize: 4096,
				}
				w = newFramer(&buf, nil, lim, compress)
			)
			defer w.close()
			for i, msg := range data {
				if err := w.writeResponse(uint64(i+1), msg); err != nil {
					t.Fatal(err)
				}
			}
			r := newFramer(nil, bytes.NewReader(buf.Bytes()), lim, compress)
			defer r.close()
			got := make([][]byte, len(data))
			for i := range got {
				fr, err := r.readFrame()
				if err != nil {
					t.Fatal(err)
				}
				testutil.AssertEqual(t, fr.requestID, uint64(i+1))
				got[i] = fr.payload
			}
			testutil.AssertEqual(t, got, data)
		})
	}
}

func TestCompressedFrameSizeLimit(t *testing.T) {
	f := newFramer(nil, nil, Limits{}, true)
	defer f.close()
	msg, err := f.compression.compress(nil, bytes.Repeat([]byte("a"), compressionThreshold))
	if err != nil {
		t.Fatal(err)
	}
	_, n := binary.Uvarint(msg)
	big := append(binary.AppendUvarint(nil, 1025), msg[n:]...)
	if _, err := f.compression.decompress(big, 1024); err == nil || !strings.Contains(err.Error(), "exceeds maximum") {
		t.Fatalf("decompress() = %v; want size limit", err)
	}
}

func TestFramerWriteError(t *testing.T) {
	var (
		want = errors.New("write failed")
		w    = &failingWriter{
			err: want,
		}
		f = newFramer(w, nil, Limits{
			MaxFrameSize: 1024,
		}, false)
	)
	for id := uint64(1); id <= 2; id++ {
		if err := f.writeResponse(id, nil); err != want {
			t.Fatalf("writeResponse() = %v; want %v", err, want)
		}
	}
	testutil.AssertEqual(t, w.calls, 1)
}

func FuzzReadFrame(f *testing.F) {
	f.Add([]byte{6, 1, 1, 1, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		newFramer(nil, bytes.NewReader(data), Limits{
			MaxFrameSize: 1024,
		}, false).readFrame()
	})
}

type shortWriter struct {
	io.Writer
}

func (w shortWriter) Write(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return w.Writer.Write(p)
}

type failingWriter struct {
	err   error
	calls int
}

func (w *failingWriter) Write([]byte) (int, error) {
	w.calls++
	return 0, w.err
}
