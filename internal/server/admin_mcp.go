package server

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ccrouter/internal/db"
	"ccrouter/internal/gateway"
	"ccrouter/internal/mcp/auth"
	"ccrouter/internal/mcp/client"
	"ccrouter/internal/mcp/protocol"

	"github.com/gin-gonic/gin"
)

func handleMcpAuthStart(c *gin.Context, state *gateway.State) {
	if state.MCP() == nil {
		c.String(http.StatusServiceUnavailable, "MCP Gateway not initialized")
		return
	}
	provider := strings.TrimSpace(c.Query("provider"))
	userID := strings.TrimSpace(c.Query("user_id"))
	if provider == "" || userID == "" {
		c.String(http.StatusBadRequest, "Missing provider or user_id query parameter")
		return
	}

	redirectURI := resolveRedirectURI(c, state)

	authURL, err := state.MCP().AuthManager().StartOAuthFlow(provider, userID, redirectURI)
	if err != nil {
		c.String(http.StatusBadRequest, "Failed to start auth: "+err.Error())
		return
	}
	c.Redirect(http.StatusFound, authURL)
}

func resolveRedirectURI(c *gin.Context, state *gateway.State) string {
	if ext := state.Service().Config.General.ExternalURL; ext != "" {
		return strings.TrimRight(ext, "/") + "/mcp/auth/callback"
	}
	scheme := "http"
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := c.Request.Host
	if fHost := c.GetHeader("X-Forwarded-Host"); fHost != "" {
		host = fHost
	}
	return fmt.Sprintf("%s://%s/mcp/auth/callback", scheme, host)
}

func handleMcpAuthCallback(c *gin.Context, state *gateway.State) {
	if state.MCP() == nil {
		c.String(http.StatusServiceUnavailable, "MCP Gateway not initialized")
		return
	}
	code := strings.TrimSpace(c.Query("code"))
	stateParam := strings.TrimSpace(c.Query("state"))
	if code == "" || stateParam == "" {
		c.String(http.StatusBadRequest, "Missing code or state parameter in callback")
		return
	}

	redirectURI := resolveRedirectURI(c, state)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	tok, err := state.MCP().AuthManager().HandleCallback(ctx, code, stateParam, redirectURI)
	if err != nil {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusBadRequest, `<!DOCTYPE html><html><body style="font-family:sans-serif;padding:40px;text-align:center;">
		<h2 style="color:#d9534f;">✗ 授权失败</h2>
		<p>`+html.EscapeString(err.Error())+`</p>
		<p>请返回聊天窗口重新尝试。</p>
		</body></html>`)
		return
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, `<!DOCTYPE html><html><body style="font-family:sans-serif;padding:40px;text-align:center;">
	<h2 style="color:#5cb85c;">✓ 授权成功</h2>
	<p>用户 <strong>`+html.EscapeString(tok.UserID)+`</strong> 已成功绑定 <strong>`+html.EscapeString(tok.Provider)+`</strong> 账号。</p>
	<p style="color:#666;">您可以关闭此标签页，并返回聊天窗口继续操作。</p>
	</body></html>`)
}

// ---- Admin API ----

func listMcpProviders(c *gin.Context) {
	s := stateOf(c)
	if s.MCP() == nil {
		c.JSON(http.StatusOK, gin.H{"providers": []any{}})
		return
	}
	providers := s.MCP().Providers()
	c.JSON(http.StatusOK, gin.H{"providers": providers})
}

func inspectProvider(ctx context.Context, cli client.Client) (*protocol.InitializeResult, []protocol.Tool, []protocol.Resource, []protocol.Prompt, []string, error) {
	initRes, err := cli.Initialize(ctx)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("initialize failed: %w", err)
	}

	var warnings []string
	tools, err := cli.ListTools(ctx)
	if err != nil {
		warnings = append(warnings, "tools: "+err.Error())
		tools = []protocol.Tool{}
	}
	resources, err := cli.ListResources(ctx)
	if err != nil {
		warnings = append(warnings, "resources: "+err.Error())
		resources = []protocol.Resource{}
	}
	prompts, err := cli.ListPrompts(ctx)
	if err != nil {
		warnings = append(warnings, "prompts: "+err.Error())
		prompts = []protocol.Prompt{}
	}

	if tools == nil {
		tools = []protocol.Tool{}
	}
	if resources == nil {
		resources = []protocol.Resource{}
	}
	if prompts == nil {
		prompts = []protocol.Prompt{}
	}

	return initRes, tools, resources, prompts, warnings, nil
}

func testMcpProvider(c *gin.Context) {
	s := stateOf(c)
	if s.MCP() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MCP Gateway not initialized"})
		return
	}

	var payload struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil || payload.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider name required"})
		return
	}

	cli, ok := s.MCP().Client(payload.Name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found: " + payload.Name})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	initRes, tools, resources, prompts, warnings, err := inspectProvider(ctx, cli)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"server_info": initRes.ServerInfo,
		"tools":       tools,
		"resources":   resources,
		"prompts":     prompts,
		"warnings":    warnings,
	})
}

func getMcpProviderCapabilities(c *gin.Context) {
	s := stateOf(c)
	if s.MCP() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MCP Gateway not initialized"})
		return
	}
	name := c.Param("name")
	cli, ok := s.MCP().Client(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found: " + name})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	initRes, tools, resources, prompts, warnings, err := inspectProvider(ctx, cli)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"server_info": initRes.ServerInfo,
		"tools":       tools,
		"resources":   resources,
		"prompts":     prompts,
		"warnings":    warnings,
	})
}

func listMcpCombos(c *gin.Context) {
	s := stateOf(c)
	if s.MCP() == nil {
		c.JSON(http.StatusOK, gin.H{"combos": []any{}})
		return
	}

	combos := s.MCP().Combos()
	type comboView struct {
		Name         string   `json:"name"`
		Description  string   `json:"description"`
		UserIDHeader string   `json:"user_id_header"`
		ToolCount    int      `json:"tool_count"`
		Tools        []string `json:"tools,omitempty"`
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	out := make([]comboView, 0, len(combos))
	for _, cb := range combos {
		tools, _ := cb.ListTools(ctx)
		toolNames := make([]string, 0, len(tools))
		for _, t := range tools {
			toolNames = append(toolNames, t.Name)
		}
		out = append(out, comboView{
			Name:         cb.Name(),
			Description:  cb.Description(),
			UserIDHeader: cb.UserIDHeader(),
			ToolCount:    len(tools),
			Tools:        toolNames,
		})
	}

	c.JSON(http.StatusOK, gin.H{"combos": out})
}

func getMcpComboTools(c *gin.Context) {
	s := stateOf(c)
	if s.MCP() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MCP Gateway not initialized"})
		return
	}

	name := c.Param("name")
	cb, ok := s.MCP().GetCombo(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "combo not found: " + name})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	tools, err := cb.ListTools(ctx)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"tools": []protocol.Tool{}, "error": err.Error()})
		return
	}
	if tools == nil {
		tools = []protocol.Tool{}
	}
	c.JSON(http.StatusOK, gin.H{"tools": tools})
}

func testMcpComboCall(c *gin.Context) {
	s := stateOf(c)
	if s.MCP() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MCP Gateway not initialized"})
		return
	}

	var payload struct {
		Combo     string         `json:"combo"`
		Tool      string         `json:"tool"`
		Arguments map[string]any `json:"arguments"`
		UserID    string         `json:"user_id"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil || payload.Combo == "" || payload.Tool == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "combo and tool are required"})
		return
	}

	cb, ok := s.MCP().GetCombo(payload.Combo)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "combo not found: " + payload.Combo})
		return
	}

	scheme := "http"
	if c.Request.TLS != nil || c.Request.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := c.Request.Host
	if fHost := c.Request.Header.Get("X-Forwarded-Host"); fHost != "" {
		host = fHost
	}
	reqBaseURL := fmt.Sprintf("%s://%s", scheme, host)
	ctx := context.WithValue(c.Request.Context(), auth.BaseURLContextKey, reqBaseURL)

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	start := time.Now()
	res, rpcErr := cb.CallTool(ctx, payload.Tool, payload.Arguments, payload.UserID)
	durationMs := int(time.Since(start).Milliseconds())

	// Record playground execution
	if s.Recorder() != nil {
		provName, _, _ := cb.ResolveTool(payload.Tool)
		var provPtr *string
		if provName != "" {
			provPtr = &provName
		}
		var userPtr *string
		if payload.UserID != "" {
			userPtr = &payload.UserID
		}
		var argsStr *string
		if len(payload.Arguments) > 0 {
			if b, err := json.Marshal(payload.Arguments); err == nil {
				s := string(b)
				if len(s) > 16384 {
					s = s[:16384] + "... [truncated]"
				}
				argsStr = &s
			}
		}
		var resStr *string
		var errMsg *string
		successInt := 1
		code := 200
		if rpcErr != nil {
			successInt = 0
			code = rpcErr.Code
			errStr := rpcErr.Message
			errMsg = &errStr
			if b, err := json.Marshal(rpcErr.Data); err == nil {
				s := string(b)
				resStr = &s
			}
		} else if res != nil {
			if b, err := json.Marshal(res); err == nil {
				s := string(b)
				if len(s) > 16384 {
					s = s[:16384] + "... [truncated]"
				}
				resStr = &s
			}
		}
		s.Recorder().RecordMcp(&db.McpRow{
			TS:         float64(time.Now().UnixNano()) / 1e9,
			Combo:      payload.Combo,
			Transport:  "playground",
			Method:     "tools/call",
			ToolName:   &payload.Tool,
			Provider:   provPtr,
			UserID:     userPtr,
			Arguments:  argsStr,
			Result:     resStr,
			DurationMs: &durationMs,
			StatusCode: &code,
			Success:    successInt,
			Error:      errMsg,
		})
	}

	if rpcErr != nil {
		c.JSON(http.StatusOK, gin.H{
			"success":     false,
			"duration_ms": durationMs,
			"error":       rpcErr,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"duration_ms": durationMs,
		"result":      res,
	})
}

func listMcpRequests(c *gin.Context) {
	st := stateOf(c)
	if st.Recorder() == nil {
		c.JSON(http.StatusOK, gin.H{"total": 0, "items": []any{}})
		return
	}

	limit := intQuery(c, "limit", 30)
	offset := intQuery(c, "offset", 0)

	var combo, provider, toolName, userID, method, transport, q *string
	if s := strings.TrimSpace(c.Query("combo")); s != "" {
		combo = &s
	}
	if s := strings.TrimSpace(c.Query("provider")); s != "" {
		provider = &s
	}
	if s := strings.TrimSpace(c.Query("tool")); s != "" {
		toolName = &s
	}
	if s := strings.TrimSpace(c.Query("user_id")); s != "" {
		userID = &s
	}
	if s := strings.TrimSpace(c.Query("method")); s != "" {
		method = &s
	}
	if s := strings.TrimSpace(c.Query("transport")); s != "" {
		transport = &s
	}
	if s := strings.TrimSpace(c.Query("q")); s != "" {
		q = &s
	}
	var success *bool
	if s := strings.TrimSpace(c.Query("success")); s != "" {
		b, err := strconv.ParseBool(s)
		if err == nil {
			success = &b
		}
	}
	since := floatQuery(c, "since")
	until := floatQuery(c, "until")

	res, err := db.QueryMcpList(st.Recorder().DBPath(), limit, offset, combo, provider, toolName, userID, method, transport, q, success, since, until)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

func getMcpStatsSummary(c *gin.Context) {
	st := stateOf(c)
	if st.Recorder() == nil {
		c.JSON(http.StatusOK, gin.H{"data": []any{}, "overview": gin.H{}, "group_by": "combo"})
		return
	}
	groupBy := c.DefaultQuery("group_by", "combo")
	since := floatQuery(c, "since")
	until := floatQuery(c, "until")

	rows, err := db.QueryMcpStats(st.Recorder().DBPath(), groupBy, since, until)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	overview, err := db.QueryMcpOverviewStats(st.Recorder().DBPath(), since, until)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "overview": overview, "group_by": groupBy})
}

func getMcpStatsTrend(c *gin.Context) {
	st := stateOf(c)
	if st.Recorder() == nil {
		c.JSON(http.StatusOK, gin.H{"data": []any{}, "bucket": "hour"})
		return
	}
	bucket := c.DefaultQuery("bucket", "hour")
	since := floatQuery(c, "since")
	until := floatQuery(c, "until")

	rows, err := db.QueryMcpTrend(st.Recorder().DBPath(), bucket, since, until)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "bucket": bucket})
}

func listMcpTokens(c *gin.Context) {
	s := stateOf(c)
	if s.Recorder() == nil {
		c.JSON(http.StatusOK, gin.H{"tokens": []any{}})
		return
	}

	tokens, err := s.Recorder().ListMcpTokens()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	type maskedToken struct {
		ID        int64  `json:"id"`
		Provider  string `json:"provider"`
		UserID    string `json:"user_id"`
		MaskedKey string `json:"masked_key"`
		TokenType string `json:"token_type"`
		Scopes    string `json:"scopes,omitempty"`
		ExpiresAt *int64 `json:"expires_at,omitempty"`
		UpdatedAt int64  `json:"updated_at"`
	}

	out := make([]maskedToken, 0, len(tokens))
	for _, t := range tokens {
		masked := t.AccessToken
		if len(masked) > 8 {
			masked = masked[:4] + "..." + masked[len(masked)-4:]
		} else {
			masked = "***"
		}
		out = append(out, maskedToken{
			ID:        t.ID,
			Provider:  t.Provider,
			UserID:    t.UserID,
			MaskedKey: masked,
			TokenType: t.TokenType,
			Scopes:    t.Scopes,
			ExpiresAt: t.ExpiresAt,
			UpdatedAt: t.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"tokens": out})
}

func deleteMcpToken(c *gin.Context) {
	s := stateOf(c)
	if s.Recorder() == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "recorder not available"})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token id"})
		return
	}

	if err := s.Recorder().DeleteMcpTokenByID(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func adminMcpInfo(c *gin.Context) {
	s := stateOf(c)
	svc := s.Service()

	providers := make([]map[string]any, 0, len(svc.Config.McpProviders))
	for i := range svc.Config.McpProviders {
		p := &svc.Config.McpProviders[i]
		mode := p.AuthMode
		if p.Auth != nil {
			if p.Auth.Mode != "" {
				mode = p.Auth.Mode
			} else if p.Auth.Type != "" {
				mode = p.Auth.Type
			}
		}
		if mode == "" {
			mode = "none"
		}
		isolated := (p.Auth != nil && p.Auth.Isolated) || p.AuthMode == "isolated"

		headersList := make([]string, 0, len(p.Headers))
		for k := range p.Headers {
			headersList = append(headersList, k)
		}

		hasClient := false
		if s.MCP() != nil {
			_, hasClient = s.MCP().Client(p.Name)
		}

		info := map[string]any{
			"name":            p.Name,
			"transport":       p.Transport,
			"url":             p.URL,
			"command":         p.Command,
			"args":            p.Args,
			"working_dir":     p.WorkingDir,
			"auth_mode":       mode,
			"isolated":        isolated,
			"timeout_seconds": p.TimeoutSeconds,
			"headers":         headersList,
			"is_ready":        hasClient,
		}
		providers = append(providers, info)
	}

	combos := make([]map[string]any, 0, len(svc.Config.McpCombos))
	totalTools := 0
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	for i := range svc.Config.McpCombos {
		cbCfg := &svc.Config.McpCombos[i]
		var toolsList []string
		if s.MCP() != nil {
			if cb, ok := s.MCP().GetCombo(cbCfg.Name); ok {
				tList, _ := cb.ListTools(ctx)
				for _, t := range tList {
					toolsList = append(toolsList, t.Name)
				}
			}
		}
		if toolsList == nil {
			toolsList = []string{}
		}
		totalTools += len(toolsList)

		members := make([]map[string]any, 0, len(cbCfg.Members))
		for _, m := range cbCfg.Members {
			tools := m.Tools
			if tools == nil {
				tools = []string{}
			}
			resources := m.Resources
			if resources == nil {
				resources = []string{}
			}
			prompts := m.Prompts
			if prompts == nil {
				prompts = []string{}
			}
			members = append(members, map[string]any{
				"provider":  m.Provider,
				"prefix":    m.Prefix,
				"tools":     tools,
				"resources": resources,
				"prompts":   prompts,
			})
		}

		uidHeader := cbCfg.UserIDHeader
		if uidHeader == "" {
			uidHeader = "X-User-Id"
		}

		ownedBy := cbCfg.OwnedBy
		if ownedBy == "" {
			ownedBy = "default"
		}

		combos = append(combos, map[string]any{
			"name":           cbCfg.Name,
			"owned_by":       ownedBy,
			"description":    cbCfg.Description,
			"user_id_header": uidHeader,
			"members":        members,
			"tool_count":     len(toolsList),
			"tools":          toolsList,
		})
	}

	tokensCount := 0
	userCount := 0
	providerTokens := make(map[string]int)
	if s.Recorder() != nil {
		if tokens, err := s.Recorder().ListMcpTokens(); err == nil {
			tokensCount = len(tokens)
			userSet := make(map[string]struct{})
			for _, t := range tokens {
				userSet[t.UserID] = struct{}{}
				providerTokens[t.Provider]++
			}
			userCount = len(userSet)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"version":         version,
		"runtime":         goRuntime(),
		"external_url":    svc.Config.General.ExternalURL,
		"combos":          combos,
		"providers":       providers,
		"total_tools":     totalTools,
		"tokens_count":    tokensCount,
		"user_count":      userCount,
		"provider_tokens": providerTokens,
	})
}
