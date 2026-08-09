// Package translate wraps CLIProxyAPI translator to convert between API formats.
//
// ccrouter uses its own format names ("openai", "anthropic", "openai-responses",
// "openai-images", "gemini") which differ slightly from CLIProxyAPI's constants.
// This package normalises the names and exposes a simple three-function API that
// the proxy layer calls without knowing about CLIProxyAPI internals.
package translate

import (
	"context"
	"strings"

	_ "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator/builtin" // register all translators
	sdkt "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

// ccrouter format name → CLIProxyAPI format name.
//
// CLIProxyAPI uses "codex" to represent the OpenAI Responses API (/v1/responses)
// as an *upstream* format (i.e. the target the proxy sends requests to).
// "openai-response" (no 's') is CLIProxyAPI's *entry* format for clients that
// speak the Responses API — it is never used as an upstream target.
//
// Mapping table (ccrouter → CLIProxyAPI):
//   openai           → openai           (chat/completions, same name)
//   anthropic        → claude           (Anthropic Messages API)
//   openai-responses → codex            (OpenAI Responses API as upstream)
//   gemini           → gemini           (same name)
var nameMap = map[string]string{
	"anthropic":        "claude",
	"openai-responses": "codex",
}

func toSDKFormat(f string) sdkt.Format {
	if mapped, ok := nameMap[f]; ok {
		return sdkt.FromString(mapped)
	}
	return sdkt.FromString(f)
}

// NeedTranslate reports whether a request/response translation is needed
// between clientFmt (what the caller sent) and upstreamFmt (what the upstream expects).
func NeedTranslate(clientFmt, upstreamFmt string) bool {
	if clientFmt == upstreamFmt {
		return false
	}
	from := toSDKFormat(clientFmt)
	to := toSDKFormat(upstreamFmt)
	return sdkt.HasRequestTransformer(from, to) || sdkt.HasResponseTransformer(from, to)
}

// TranslateRequest converts a raw JSON request body from clientFmt to upstreamFmt.
// If no translator is registered the original body is returned unchanged.
func TranslateRequest(clientFmt, upstreamFmt, model string, rawJSON []byte, stream bool) []byte {
	from := toSDKFormat(clientFmt)
	to := toSDKFormat(upstreamFmt)
	return sdkt.TranslateRequest(from, to, model, rawJSON, stream)
}

// TranslateResponseStream converts a single SSE data line from upstreamFmt back to clientFmt.
// param must be passed across consecutive calls for the same stream so the translator
// can carry state between chunks (e.g. usage accumulation in Claude).
// Returns zero or more output lines (each is a complete "data: …\n\n" frame or similar).
func TranslateResponseStream(
	ctx context.Context,
	clientFmt, upstreamFmt, model string,
	originalReq, translatedReq, chunk []byte,
	param *any,
) [][]byte {
	from := toSDKFormat(upstreamFmt) // upstream → client
	to := toSDKFormat(clientFmt)
	return sdkt.TranslateStream(ctx, from, to, model, originalReq, translatedReq, chunk, param)
}

// TranslateResponseNonStream converts a complete response body from upstreamFmt back to clientFmt.
func TranslateResponseNonStream(
	ctx context.Context,
	clientFmt, upstreamFmt, model string,
	originalReq, translatedReq, body []byte,
	param *any,
) []byte {
	from := toSDKFormat(upstreamFmt)
	to := toSDKFormat(clientFmt)
	return sdkt.TranslateNonStream(ctx, from, to, model, originalReq, translatedReq, body, param)
}

// NormaliseUpstreamFormat maps an upstream API format name to its endpoint path suffix.
// For formats that are handled by translation (e.g. sending a Claude request to an OpenAI
// endpoint), the caller should use the *upstream* format's URL, not the client format's.
// This helper isn't strictly necessary but helps document intent.
func NormaliseUpstreamFormat(upstreamFmt string) string {
	return strings.ToLower(strings.TrimSpace(upstreamFmt))
}
