package proxy

import (
	"time"

	"ccrouter/internal/db"
)

// record enqueues a request row to the SQLite recorder (no-op when disabled).
func (s *Service) record(combo, provider, model, key, apiFormat string, isStream bool,
	statusCode *int, success bool, matchedRule string, usage map[string]any, t0 time.Time,
	errText *string, matchedPayload *string) {

	if s.Recorder == nil {
		return
	}
	if usage == nil {
		usage = map[string]any{}
	}
	keyPrefix := keyPrefixOf(key)
	dur := int(time.Since(t0).Milliseconds())
	successInt := 0
	if success {
		successInt = 1
	}
	streamInt := 0
	if isStream {
		streamInt = 1
	}
	s.Recorder.Record(&db.Row{
		TS:               float64(time.Now().UnixNano()) / 1e9,
		Combo:            strPtr(combo),
		Provider:         strPtr(provider),
		Model:            strPtr(model),
		KeyPrefix:        strPtr(keyPrefix),
		APIFormat:        strPtr(apiFormat),
		IsStream:         streamInt,
		StatusCode:       statusCode,
		Success:          successInt,
		MatchedRule:      strPtrIf(matchedRule != "", matchedRule),
		PromptTokens:     intPtr(usage["prompt_tokens"]),
		CompletionTokens: intPtr(usage["completion_tokens"]),
		TotalTokens:      intPtr(usage["total_tokens"]),
		CacheReadTokens:  intPtr(usage["cache_read_tokens"]),
		CacheWriteTokens: intPtr(usage["cache_write_tokens"]),
		DurationMs:       &dur,
		Error:            errText,
		MatchedPayload:   matchedPayload,
	})
}

func keyPrefixOf(key string) string {
	if len(key) > 8 {
		return key[:8]
	}
	return key
}

func strPtr(s string) *string { return &s }

func strPtrIf(cond bool, s string) *string {
	if !cond {
		return nil
	}
	return &s
}

func intPtr(v any) *int {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case float64:
		x := int(t)
		return &x
	case int:
		return &t
	case int64:
		x := int(t)
		return &x
	case *int:
		return t
	}
	return nil
}
