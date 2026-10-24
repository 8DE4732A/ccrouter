package proxy

import (
	"log"
	"net/http"

	"ccrouter/internal/config"
	"ccrouter/internal/reqcompress"
)

// compressionApplies reports whether the provider's configured outbound
// compression would be used for this payload.
func compressionApplies(p *config.ProviderConfig, contentType string, bodyLen int) bool {
	return p != nil && p.RequestCompression != "" && bodyLen > 0 &&
		reqcompress.IsCompressibleContentType(contentType)
}

// compressUpstreamBody encodes the request body for the upstream when the
// provider opts in and the payload is JSON, updating headers to match. On any
// encoding error it returns the original body so a misconfiguration cannot
// break requests.
func (s *Service) compressUpstreamBody(providerName string, body []byte, headers http.Header) []byte {
	p := s.providers[providerName]
	if !compressionApplies(p, headers.Get("Content-Type"), len(body)) {
		return body
	}
	encoded, err := reqcompress.Encode(p.RequestCompression, body)
	if err != nil {
		log.Printf("request compression failed: provider=%s method=%s err=%v", providerName, p.RequestCompression, err)
		return body
	}
	headers.Set("Content-Encoding", p.RequestCompression)
	headers.Del("Content-Length")
	headers.Del("Transfer-Encoding")
	return encoded
}
