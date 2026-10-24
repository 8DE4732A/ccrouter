package reqcompress

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zlib"
	"github.com/klauspost/compress/zstd"
)

func TestIsCompressibleContentType(t *testing.T) {
	for _, tc := range []struct {
		ct   string
		want bool
	}{
		{"application/json", true},
		{"application/json; charset=utf-8", true},
		{"application/vnd.api+json", true},
		{"Application/JSON", true},
		{"multipart/form-data; boundary=x", false},
		{"text/plain", false},
		{"", false},
		{"not a media type", false},
	} {
		if got := IsCompressibleContentType(tc.ct); got != tc.want {
			t.Fatalf("IsCompressibleContentType(%q) = %v, want %v", tc.ct, got, tc.want)
		}
	}
}

func TestEncodeRoundtrip(t *testing.T) {
	payload := []byte(`{"model":"gpt","messages":[{"role":"user","content":"` + strings.Repeat("hi ", 200) + `"}]}`)

	decompressors := map[string]func([]byte) ([]byte, error){
		"gzip": func(b []byte) ([]byte, error) {
			r, err := gzip.NewReader(bytes.NewReader(b))
			if err != nil {
				return nil, err
			}
			defer r.Close()
			return io.ReadAll(r)
		},
		"deflate": func(b []byte) ([]byte, error) {
			r, err := zlib.NewReader(bytes.NewReader(b))
			if err != nil {
				return nil, err
			}
			defer r.Close()
			return io.ReadAll(r)
		},
		"br": func(b []byte) ([]byte, error) {
			return io.ReadAll(brotli.NewReader(bytes.NewReader(b)))
		},
		"zstd": func(b []byte) ([]byte, error) {
			r, err := zstd.NewReader(bytes.NewReader(b))
			if err != nil {
				return nil, err
			}
			defer r.Close()
			return io.ReadAll(r)
		},
	}

	for method, decompress := range decompressors {
		t.Run(method, func(t *testing.T) {
			encoded, err := Encode(method, payload)
			if err != nil {
				t.Fatal(err)
			}
			if len(encoded) == 0 {
				t.Fatal("empty encoded body")
			}
			decoded, err := decompress(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(decoded, payload) {
				t.Fatal("roundtrip mismatch")
			}
		})
	}
}

func TestEncodeUnknownMethod(t *testing.T) {
	if _, err := Encode("snappy", []byte("x")); err == nil {
		t.Fatal("expected error for unsupported method")
	}
}
