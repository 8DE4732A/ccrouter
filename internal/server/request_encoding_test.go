package server

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
)

type decoderResult struct {
	status        int
	errorBody     string
	body          string
	contentLength int64
	encoding      string
}

func runDecodeMiddleware(t *testing.T, contentEncoding string, body []byte) decoderResult {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var res decoderResult
	r := gin.New()
	r.Use(decompressRequestBody())
	r.POST("/echo", func(c *gin.Context) {
		b, _ := io.ReadAll(c.Request.Body)
		res.body = string(b)
		res.contentLength = c.Request.ContentLength
		res.encoding = c.GetHeader("Content-Encoding")
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/echo", bytes.NewReader(body))
	if contentEncoding != "" {
		req.Header.Set("Content-Encoding", contentEncoding)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	res.status = rec.Code
	if rec.Code != http.StatusOK {
		res.errorBody = rec.Body.String()
	}
	return res
}

func compressData(t *testing.T, data []byte, newWriter func(io.Writer) (io.WriteCloser, error)) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := newWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func gzipData(t *testing.T, data []byte) []byte {
	return compressData(t, data, func(w io.Writer) (io.WriteCloser, error) { return gzip.NewWriter(w), nil })
}

func zlibData(t *testing.T, data []byte) []byte {
	return compressData(t, data, func(w io.Writer) (io.WriteCloser, error) { return zlib.NewWriter(w), nil })
}

func rawDeflateData(t *testing.T, data []byte) []byte {
	return compressData(t, data, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, flate.DefaultCompression)
	})
}

func zstdData(t *testing.T, data []byte) []byte {
	return compressData(t, data, func(w io.Writer) (io.WriteCloser, error) { return zstd.NewWriter(w) })
}

func brotliData(t *testing.T, data []byte) []byte {
	return compressData(t, data, func(w io.Writer) (io.WriteCloser, error) { return brotli.NewWriter(w), nil })
}

func TestDecompressRequestBody(t *testing.T) {
	payload := []byte(`{"model":"gpt","messages":[{"role":"user","content":"hello"}]}`)
	cases := []struct {
		name     string
		encoding string
		body     []byte
	}{
		{"gzip", "gzip", gzipData(t, payload)},
		{"x-gzip", "x-gzip", gzipData(t, payload)},
		{"deflate-zlib", "deflate", zlibData(t, payload)},
		{"deflate-raw", "deflate", rawDeflateData(t, payload)},
		{"zstd", "zstd", zstdData(t, payload)},
		{"brotli", "br", brotliData(t, payload)},
		{"stacked", "gzip, zstd", zstdData(t, gzipData(t, payload))},
		{"case-insensitive", "GZip", gzipData(t, payload)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runDecodeMiddleware(t, tc.encoding, tc.body)
			if res.status != http.StatusOK {
				t.Fatalf("status = %d, body = %s", res.status, res.errorBody)
			}
			if res.body != string(payload) {
				t.Fatalf("body = %q, want %q", res.body, string(payload))
			}
			if res.encoding != "" {
				t.Fatalf("Content-Encoding leaked downstream: %q", res.encoding)
			}
			if res.contentLength != int64(len(payload)) {
				t.Fatalf("ContentLength = %d, want %d", res.contentLength, len(payload))
			}
		})
	}
}

func TestDecompressRequestBodyPassthrough(t *testing.T) {
	payload := []byte(`{"model":"gpt"}`)
	for _, enc := range []string{"", "identity"} {
		res := runDecodeMiddleware(t, enc, payload)
		if res.status != http.StatusOK || res.body != string(payload) {
			t.Fatalf("enc=%q: status=%d body=%q", enc, res.status, res.body)
		}
	}
}

func TestDecompressRequestBodyUnsupportedEncoding(t *testing.T) {
	res := runDecodeMiddleware(t, "snappy", []byte("whatever"))
	if res.status != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", res.status)
	}
	if !strings.Contains(res.errorBody, "unsupported content-encoding") {
		t.Fatalf("unexpected error: %s", res.errorBody)
	}
}

func TestDecompressRequestBodyCorrupt(t *testing.T) {
	res := runDecodeMiddleware(t, "gzip", []byte("not-gzip"))
	if res.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", res.status)
	}
	if !strings.Contains(res.errorBody, "invalid gzip request body") {
		t.Fatalf("unexpected error: %s", res.errorBody)
	}
}

func TestReadLimitedEnforcesLimit(t *testing.T) {
	if _, err := readLimited(strings.NewReader(strings.Repeat("a", 1024)), "gzip", 16); !errors.Is(err, errDecompressedTooLarge) {
		t.Fatalf("err = %v, want errDecompressedTooLarge", err)
	}
}

func TestRouterDecompressesGzipProxyRequest(t *testing.T) {
	st, _ := newTestState(t)
	r := Router(st)

	payload := []byte(`{"model":"missing-combo","messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(gzipData(t, payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "missing-combo") {
		t.Fatalf("model was not parsed from the compressed body: %s", rec.Body.String())
	}
}

func TestRouterDecompressErrorUsesGeminiEnvelope(t *testing.T) {
	st, _ := newTestState(t)
	r := Router(st)

	req := httptest.NewRequest("POST", "/v1beta/models/gemini-pro:generateContent", strings.NewReader("nope"))
	req.Header.Set("Content-Encoding", "snappy")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error envelope: %v (body=%s)", err, rec.Body.String())
	}
	if payload.Error.Code != http.StatusUnsupportedMediaType || payload.Error.Status != "INVALID_ARGUMENT" {
		t.Fatalf("unexpected envelope: %+v", payload.Error)
	}
}
