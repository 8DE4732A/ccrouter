// Package script executes payload scripts against a request context.
//
// Scripts are written in the expr language (github.com/expr-lang/expr).
// Each script receives a "request" environment with body, headers, combo,
// path, provider, model, and api_format fields, and may call built-in
// mutation helpers.
//
// Environment exposed to scripts:
//
//	body        — parsed JSON body (map); mutate via set/del/setpath
//	headers     — HTTP headers (map); mutate via seth/delh
//	combo       — client-facing combo/model name (string, read-only)
//	path        — request path, e.g. "/v1/chat/completions" (string, read-only)
//	provider    — selected upstream provider name (string, read-only)
//	model       — rewritten model name sent to upstream (string, read-only)
//	api_format  — API format in use: openai/anthropic/… (string, read-only)
//
// Built-in functions:
//
//	set(map, key, value)                — map[key] = value; returns nil
//	del(map, key)                       — delete map[key]; returns nil
//	setpath(map, key1, key2, …, value)  — nested set; creates intermediate maps
//	get(map, key, default)              — map[key] if present, else default
//	clamp(v, min, max)                  — min(max(v, min), max)
package script

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// Env is the environment passed to each script.
// Body and Headers are mutable; all other fields are read-only.
type Env struct {
	Body      map[string]any    `expr:"body"`
	Headers   map[string]string `expr:"headers"`
	Combo     string            `expr:"combo"`
	Path      string            `expr:"path"`
	Provider  string            `expr:"provider"`
	Model     string            `expr:"model"`
	APIFormat string            `expr:"api_format"`

	// Built-in mutation functions (bound at Compile time).
	Set     func(map[string]any, string, any) any      `expr:"set"`
	Del     func(map[string]any, string) any            `expr:"del"`
	SetPath func(args ...any) any                       `expr:"setpath"`
	Get     func(map[string]any, string, any) any       `expr:"get"`
	Clamp   func(float64, float64, float64) float64     `expr:"clamp"`
	SetH    func(map[string]string, string, string) any `expr:"seth"`
	DelH    func(map[string]string, string) any         `expr:"delh"`
}

// Program is a compiled, reusable script.
// A script may contain multiple lines; each non-empty, non-comment line is
// compiled as an independent expr expression and executed in order.
type Program struct {
	stmts []statement // one per non-empty source line
	src   string
}

type statement struct {
	prog *vm.Program
}

// Compile parses and type-checks a script.
// Statements are separated by newlines; lines are joined when parentheses are
// unbalanced (continuation), so multi-line ternary expressions work naturally.
// Lines starting with '#' are comments and are ignored.
// Returns an error on the first statement with invalid syntax.
func Compile(src string) (*Program, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return &Program{src: src}, nil
	}

	env := Env{}
	p := &Program{src: src}
	for _, stmt := range splitStatements(src) {
		prog, err := expr.Compile(stmt,
			expr.Env(env),
			expr.AllowUndefinedVariables(),
		)
		if err != nil {
			return nil, fmt.Errorf("script compile error: %w (expr: %q)", err, stmt)
		}
		p.stmts = append(p.stmts, statement{prog: prog})
	}
	return p, nil
}

// splitStatements splits a multi-line script into individual expression strings.
//
// Rules:
//  1. Lines starting with '#' are comments — skipped.
//  2. Blank lines act as explicit statement separators.
//  3. A statement continues onto the next line when:
//     a. parentheses / brackets are not yet balanced, OR
//     b. the current line (trimmed) ends with a binary operator
//        (?, :, &&, ||, and, or, +, -, *, /, %, ,, ==, !=, <, >, <=, >=, !), OR
//     c. the NEXT line starts with a continuation operator (?, :)
//        — checked via one-line lookahead.
func splitStatements(src string) []string {
	rawLines := strings.Split(src, "\n")
	// Pre-filter: strip comments, keep blanks as separators.
	lines := make([]string, 0, len(rawLines))
	for _, l := range rawLines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#") {
			lines = append(lines, "") // treat comment as blank separator
		} else {
			lines = append(lines, t)
		}
	}

	var stmts []string
	var cur strings.Builder
	depth := 0

	flush := func() {
		s := strings.TrimSpace(cur.String())
		if s != "" {
			stmts = append(stmts, s)
		}
		cur.Reset()
		depth = 0
	}

	for i, line := range lines {
		if line == "" {
			if cur.Len() > 0 {
				flush()
			}
			continue
		}

		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(line)

		for _, ch := range line {
			switch ch {
			case '(', '[':
				depth++
			case ')', ']':
				depth--
			}
		}

		if depth > 0 {
			continue // unbalanced parens — always continue
		}
		if depth < 0 {
			depth = 0 // guard against mismatched parens in user script
		}

		if endsWithContinuationOp(line) {
			continue
		}

		// Peek at next non-blank line: if it starts with ? or : → continue.
		if startsNextWithContinuation(lines, i+1) {
			continue
		}

		flush()
	}
	if cur.Len() > 0 {
		flush()
	}
	return stmts
}

var continuationSuffixes = []string{
	"&&", "||", "and", "or",
	"?", ":",
	"==", "!=", "<=", ">=", "<", ">",
	"+", "-", "*", "/", "%",
	",",
}

func endsWithContinuationOp(line string) bool {
	for _, op := range continuationSuffixes {
		if strings.HasSuffix(line, op) {
			return true
		}
	}
	return false
}

func startsNextWithContinuation(lines []string, from int) bool {
	for i := from; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			return false // blank line = separator, no continuation
		}
		return strings.HasPrefix(t, "?") || strings.HasPrefix(t, ":")
	}
	return false
}

// Run executes each statement in order against body and headers (both mutated in place).
// Returns a status string: "" = empty/no-op, "ok" = executed, "error:…" = first failure.
// On error the remaining statements are skipped and original state is unaffected for
// the failed statement (earlier mutations are kept — same semantics as sense-roll).
func (p *Program) Run(
	body map[string]any,
	headers map[string]string,
	combo, path, provider, model, apiFormat string,
) (status string) {
	if len(p.stmts) == 0 {
		return ""
	}

	env := &Env{
		Body:      body,
		Headers:   headers,
		Combo:     combo,
		Path:      path,
		Provider:  provider,
		Model:     model,
		APIFormat: apiFormat,
		Set:       builtinSet,
		Del:       builtinDel,
		SetPath:   builtinSetPath,
		Get:       builtinGet,
		Clamp:     builtinClamp,
		SetH:      builtinSetH,
		DelH:      builtinDelH,
	}

	machine := vm.VM{}
	for _, stmt := range p.stmts {
		if _, err := machine.Run(stmt.prog, env); err != nil {
			return "error: " + err.Error()
		}
	}
	return "ok"
}

// ── built-in functions ──────────────────────────────────────────────────────

func builtinSet(m map[string]any, key string, val any) any {
	if m != nil {
		m[key] = val
	}
	return nil
}

func builtinDel(m map[string]any, key string) any {
	delete(m, key)
	return nil
}

// builtinSetPath(map, key1, key2, …, value) — any number of intermediate keys.
func builtinSetPath(args ...any) any {
	if len(args) < 3 {
		return nil
	}
	m, ok := args[0].(map[string]any)
	if !ok || m == nil {
		return nil
	}
	keys := args[1 : len(args)-1]
	val := args[len(args)-1]

	cur := m
	for _, k := range keys[:len(keys)-1] {
		ks, ok := k.(string)
		if !ok {
			return nil
		}
		next, exists := cur[ks]
		if !exists {
			sub := map[string]any{}
			cur[ks] = sub
			cur = sub
		} else {
			sub, ok := next.(map[string]any)
			if !ok {
				// overwrite non-map with a new map
				sub = map[string]any{}
				cur[ks] = sub
			}
			cur = sub
		}
	}
	lastKey, ok := keys[len(keys)-1].(string)
	if !ok {
		return nil
	}
	cur[lastKey] = val
	return nil
}

func builtinGet(m map[string]any, key string, def any) any {
	if m == nil {
		return def
	}
	if v, ok := m[key]; ok {
		return v
	}
	return def
}

func builtinClamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func builtinSetH(m map[string]string, key, val string) any {
	if m != nil {
		m[key] = val
	}
	return nil
}

func builtinDelH(m map[string]string, key string) any {
	delete(m, key)
	return nil
}

// ── RunBytes convenience wrapper ────────────────────────────────────────────

// RunBytes executes a pre-compiled script against raw JSON bytes.
// Returns rewritten body bytes, rewritten headers, and a status string.
// If body is not valid JSON, it is passed through unchanged (headers still mutate).
func RunBytes(
	p *Program,
	rawBody []byte,
	headers map[string]string,
	combo, path, provider, model, apiFormat string,
) ([]byte, map[string]string, string) {
	if p == nil || len(p.stmts) == 0 {
		return rawBody, headers, ""
	}

	var bodyMap map[string]any
	isJSON := json.Unmarshal(rawBody, &bodyMap) == nil && bodyMap != nil
	if !isJSON {
		// Non-JSON or JSON null: keep body bytes as-is but still run header mutations.
		bodyMap = map[string]any{}
	}

	// Work on a copy of headers so partial mutations aren't lost on error.
	hCopy := make(map[string]string, len(headers))
	for k, v := range headers {
		hCopy[k] = v
	}

	status := p.Run(bodyMap, hCopy, combo, path, provider, model, apiFormat)
	if strings.HasPrefix(status, "error:") {
		// On error: return original body unchanged, but headers unchanged too.
		return rawBody, headers, status
	}

	outBody := rawBody
	if isJSON {
		if b, err := json.Marshal(bodyMap); err == nil {
			outBody = b
		}
	}
	return outBody, hCopy, status
}
