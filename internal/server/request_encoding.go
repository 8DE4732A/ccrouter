package server

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/flate"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zlib"
	"github.com/klauspost/compress/zstd"
)

// maxDecompressedBody caps the decoded size of a compressed request body so a
// small payload cannot expand into an unbounded allocation. Bodies sent without
// Content-Encoding are not affected.
const maxDecompressedBody = 256 << 20

var (
	errUnsupportedEncoding  = errors.New("unsupported content-encoding")
	errDecompressedTooLarge = errors.New("decompressed request body too large")
)

// decompressRequestBody transparently decodes gzip, deflate, brotli and zstd
// request bodies. Downstream handlers always see a plain body: Content-Encoding
// is removed and ContentLength is rewritten so the decoded bytes are never
// mistaken for the original payload.
func decompressRequestBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		encodings := parseContentEncodings(c.GetHeader("Content-Encoding"))
		if len(encodings) == 0 || c.Request.Body == nil || c.Request.Body == http.NoBody {
			c.Next()
			return
		}

		body, err := decodeBody(c.Request.Body, encodings)
		if err != nil {
			status := http.StatusBadRequest
			switch {
			case errors.Is(err, errUnsupportedEncoding):
				status = http.StatusUnsupportedMediaType
			case errors.Is(err, errDecompressedTooLarge):
				status = http.StatusRequestEntityTooLarge
			}
			abortRequestError(c, status, err.Error())
			return
		}

		_ = c.Request.Body.Close()
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		c.Request.ContentLength = int64(len(body))
		c.Request.TransferEncoding = nil
		c.Request.Header.Del("Content-Encoding")
		c.Next()
	}
}

// parseContentEncodings splits a Content-Encoding header into the list of
// codings that were applied, outermost last. "identity" is a no-op and dropped.
func parseContentEncodings(header string) []string {
	var encodings []string
	for part := range strings.SplitSeq(header, ",") {
		enc := strings.ToLower(strings.TrimSpace(part))
		if enc == "" || enc == "identity" {
			continue
		}
		encodings = append(encodings, enc)
	}
	return encodings
}

// decodeBody validates the coding list, then decodes data layer by layer. The
// header lists codings in the order they were applied, so decoding runs in
// reverse.
func decodeBody(r io.Reader, encodings []string) ([]byte, error) {
	for _, enc := range encodings {
		if !supportedEncoding(enc) {
			return nil, fmt.Errorf("%w: %q", errUnsupportedEncoding, enc)
		}
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	for _, enc := range slices.Backward(encodings) {
		if data, err = decodeLayer(enc, data); err != nil {
			return nil, err
		}
	}
	return data, nil
}

func supportedEncoding(enc string) bool {
	switch enc {
	case "gzip", "x-gzip", "deflate", "zstd", "br":
		return true
	}
	return false
}

func decodeLayer(encoding string, data []byte) ([]byte, error) {
	switch encoding {
	case "gzip", "x-gzip":
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("invalid %s request body: %w", encoding, err)
		}
		defer zr.Close()
		return readLimited(zr, encoding, maxDecompressedBody)

	case "deflate":
		// The HTTP spec specifies a zlib wrapper, but plenty of clients send a
		// raw DEFLATE stream instead; fall back when the zlib header is absent.
		if zr, err := zlib.NewReader(bytes.NewReader(data)); err == nil {
			defer zr.Close()
			return readLimited(zr, encoding, maxDecompressedBody)
		}
		return readLimited(flate.NewReader(bytes.NewReader(data)), encoding, maxDecompressedBody)

	case "zstd":
		zr, err := zstd.NewReader(bytes.NewReader(data), zstd.WithDecoderConcurrency(1))
		if err != nil {
			return nil, fmt.Errorf("invalid %s request body: %w", encoding, err)
		}
		defer zr.Close()
		return readLimited(zr, encoding, maxDecompressedBody)

	case "br":
		return readLimited(brotli.NewReader(bytes.NewReader(data)), encoding, maxDecompressedBody)

	default:
		return nil, fmt.Errorf("%w: %q", errUnsupportedEncoding, encoding)
	}
}

func readLimited(r io.Reader, encoding string, limit int64) ([]byte, error) {
	out, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("invalid %s request body: %w", encoding, err)
	}
	if int64(len(out)) > limit {
		return nil, errDecompressedTooLarge
	}
	return out, nil
}

// abortRequestError writes an error in the format the caller expects (Gemini
// routes use their own envelope; everything else is OpenAI-shaped).
func abortRequestError(c *gin.Context, status int, msg string) {
	if isGeminiRequest(c) {
		c.AbortWithStatusJSON(status, gin.H{
			"error": gin.H{
				"code":    status,
				"message": msg,
				"status":  "INVALID_ARGUMENT",
			},
		})
		return
	}
	c.AbortWithStatusJSON(status, gin.H{"error": msg, "type": "invalid_request_error"})
}
