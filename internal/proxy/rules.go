package proxy

import (
	"encoding/json"
	"strconv"
	"strings"

	"ccrouter/internal/config"

	"github.com/ohler55/ojg/jp"
)

// jsonPathExpr wraps a compiled JSONPath expression.
type jsonPathExpr struct{ expr jp.Expr }

// compileJSONPath compiles a JSONPath expression.
func compileJSONPath(path string) (jsonPathExpr, error) {
	e, err := jp.ParseString(path)
	return jsonPathExpr{expr: e}, err
}

// find returns all scalar values matched by the expression.
func (x jsonPathExpr) find(data any) []any {
	vals := x.expr.Get(data)
	out := make([]any, 0, len(vals))
	for _, v := range vals {
		if arr, ok := v.([]any); ok {
			out = append(out, arr...)
		} else {
			out = append(out, v)
		}
	}
	return out
}

// matchRotationRules returns the first matching health rule for provider+model.
// statusCode is the upstream HTTP status code (used for http_status_codes rules).
func (s *Service) matchRotationRules(body []byte, providerName, model string, statusCode int) *config.HealthCheckRule {
	var data any
	if err := json.Unmarshal(body, &data); err != nil {
		data = nil
	}
	for _, cr := range s.providerRules[providerName] {
		if len(cr.rule.Models) > 0 && !containsString(cr.rule.Models, model) {
			continue
		}
		// Check HTTP status code match first (if rule lists status codes).
		if len(cr.rule.HTTPStatusCodes) > 0 {
			for _, code := range cr.rule.HTTPStatusCodes {
				if code == statusCode {
					return cr.rule
				}
			}
		}
		// Skip JSONPath body check if no JSONPath or no body data.
		if cr.rule.JSONPath == "" || cr.expr.expr == nil || data == nil {
			continue
		}
		for _, v := range cr.expr.find(data) {
			if valueMatches(v, cr.rule) {
				return cr.rule
			}
		}
	}
	return nil
}

func valueMatches(value any, rule *config.HealthCheckRule) bool {
	text := valueToString(value)
	switch rule.MatchType {
	case "contains":
		return strings.Contains(text, rule.MatchValue)
	case "regex":
		if re := rule.CompiledRegex(); re != nil {
			return re.MatchString(text)
		}
		return false
	default: // equals
		return text == rule.MatchValue
	}
}

func valueToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return ""
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
