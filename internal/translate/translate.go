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
// "openai-responses" has dual semantics in CLIProxyAPI:
//   - As client entry format → "openai-response" (OpenaiResponse)
//   - As upstream target format → "codex" (Codex)
//
// We use separate maps for request direction (client→upstream) and
// response direction (upstream→client) to pick the right SDK name.
//
// Request translation: from=clientFmt(entry), to=upstreamFmt(target)
//   openai-responses as FROM (client) → "openai-response"
//   openai-responses as TO (upstream) → "codex"
//
// Response translation: from=upstreamFmt, to=clientFmt — same mapping reversed.
var clientFmtMap = map[string]string{
	"anthropic":        "claude",
	"openai-responses": "openai-response", // client speaks responses API
}

var upstreamFmtMap = map[string]string{
	"anthropic":        "claude",
	"openai-responses": "codex", // upstream speaks responses API (/v1/responses)
}

func toClientSDKFormat(f string) sdkt.Format {
	if mapped, ok := clientFmtMap[f]; ok {
		return sdkt.FromString(mapped)
	}
	return sdkt.FromString(f)
}

func toUpstreamSDKFormat(f string) sdkt.Format {
	if mapped, ok := upstreamFmtMap[f]; ok {
		return sdkt.FromString(mapped)
	}
	return sdkt.FromString(f)
}

// NeedTranslate reports whether a request/response translation is needed.
func NeedTranslate(clientFmt, upstreamFmt string) bool {
	if clientFmt == upstreamFmt {
		return false
	}
	clientSDK := toClientSDKFormat(clientFmt)
	upstreamSDK := toUpstreamSDKFormat(upstreamFmt)
	// Request translation:  clientFmt → upstreamFmt  =>  HasRequestTransformer(clientSDK, upstreamSDK)
	// Response translation: upstreamFmt → clientFmt  =>  HasResponseTransformer(upstreamSDK, clientSDK)
	return sdkt.HasRequestTransformer(clientSDK, upstreamSDK) ||
		sdkt.HasResponseTransformer(upstreamSDK, clientSDK)
}

// TranslateRequest converts a raw JSON request body from clientFmt to upstreamFmt.
func TranslateRequest(clientFmt, upstreamFmt, model string, rawJSON []byte, stream bool) []byte {
	from := toClientSDKFormat(clientFmt)
	to := toUpstreamSDKFormat(upstreamFmt)
	return sdkt.TranslateRequest(from, to, model, rawJSON, stream)
}

// TranslateResponseStream converts a single SSE data line from upstreamFmt back to clientFmt.
// Response direction: upstream → client.
// upstream format uses upstreamFmtMap (same as request "to") because the upstream responds
// in its native format (e.g. codex responds with codex SSE, not openai-response SSE).
func TranslateResponseStream(
	ctx context.Context,
	clientFmt, upstreamFmt, model string,
	originalReq, translatedReq, chunk []byte,
	param *any,
) [][]byte {
	from := toUpstreamSDKFormat(upstreamFmt) // upstream responds in its native format
	to := toClientSDKFormat(clientFmt)
	return sdkt.TranslateStream(ctx, from, to, model, originalReq, translatedReq, chunk, param)
}

// TranslateResponseNonStream converts a complete response body from upstreamFmt back to clientFmt.
func TranslateResponseNonStream(
	ctx context.Context,
	clientFmt, upstreamFmt, model string,
	originalReq, translatedReq, body []byte,
	param *any,
) []byte {
	from := toUpstreamSDKFormat(upstreamFmt)
	to := toClientSDKFormat(clientFmt)
	return sdkt.TranslateNonStream(ctx, from, to, model, originalReq, translatedReq, body, param)
}

// NormaliseUpstreamFormat maps an upstream API format name to its endpoint path suffix.
// For formats that are handled by translation (e.g. sending a Claude request to an OpenAI
// endpoint), the caller should use the *upstream* format's URL, not the client format's.
// This helper isn't strictly necessary but helps document intent.
func NormaliseUpstreamFormat(upstreamFmt string) string {
	return strings.ToLower(strings.TrimSpace(upstreamFmt))
}
