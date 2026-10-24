// Package reqcompress encodes outbound HTTP request bodies.
package reqcompress

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zlib"
	"github.com/klauspost/compress/zstd"
)

// IsCompressibleContentType reports whether body of this type is worth
// compressing. Only JSON is accepted: multipart uploads and opaque payloads
// are forwarded untouched.
func IsCompressibleContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

// encoders maps a method name to its writer constructor. zstd's constructor
// needs an option, so the table keeps a common (io.WriteCloser, error) shape.
var encoders = map[string]func(io.Writer) (io.WriteCloser, error){
	"gzip": func(w io.Writer) (io.WriteCloser, error) { return gzip.NewWriter(w), nil },
	"deflate": func(w io.Writer) (io.WriteCloser, error) {
		// HTTP "deflate" is a zlib-wrapped stream.
		return zlib.NewWriter(w), nil
	},
	"br": func(w io.Writer) (io.WriteCloser, error) { return brotli.NewWriter(w), nil },
	"zstd": func(w io.Writer) (io.WriteCloser, error) {
		return zstd.NewWriter(w, zstd.WithEncoderConcurrency(1))
	},
}

// Encode compresses body with method. Supported methods are gzip, deflate,
// br and zstd.
func Encode(method string, body []byte) ([]byte, error) {
	newWriter, ok := encoders[method]
	if !ok {
		return nil, fmt.Errorf("unsupported request compression %q", method)
	}
	var buf bytes.Buffer
	buf.Grow(len(body) / 2)
	w, err := newWriter(&buf)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(body); err != nil {
		_ = w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
