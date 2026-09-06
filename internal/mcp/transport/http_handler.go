package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"ccrouter/internal/db"
	"ccrouter/internal/mcp/auth"
	"ccrouter/internal/mcp/gateway"
	"ccrouter/internal/mcp/protocol"

	"github.com/gin-gonic/gin"
)

// Handler handles downstream MCP HTTP and SSE requests.
type Handler struct {
	gw *gateway.Gateway
}

// NewHandler creates a new transport Handler.
func NewHandler(gw *gateway.Gateway) *Handler {
	return &Handler{gw: gw}
}

func (h *Handler) recordCall(comboName, transport, method string, toolName, provider, userID *string, args, result any, durMs int, statusCode int, success bool, errMsg *string) {
	if h.gw == nil || h.gw.Recorder() == nil {
		return
	}
	var argsStr *string
	if args != nil {
		if b, err := json.Marshal(args); err == nil {
			s := string(b)
			if len(s) > 16384 {
				s = s[:16384] + "... [truncated]"
			}
			argsStr = &s
		}
	}
	var resStr *string
	if result != nil {
		if b, err := json.Marshal(result); err == nil {
			s := string(b)
			if len(s) > 16384 {
				s = s[:16384] + "... [truncated]"
			}
			resStr = &s
		}
	}
	successInt := 0
	if success {
		successInt = 1
	}
	h.gw.RecordRequest(&db.McpRow{
		TS:         float64(time.Now().UnixNano()) / 1e9,
		Combo:      comboName,
		Transport:  transport,
		Method:     method,
		ToolName:   toolName,
		Provider:   provider,
		UserID:     userID,
		Arguments:  argsStr,
		Result:     resStr,
		DurationMs: &durMs,
		StatusCode: &statusCode,
		Success:    successInt,
		Error:      errMsg,
	})
}

// HandleHTTP handles Streamable HTTP requests (POST /mcp/:combo).
func (h *Handler) HandleHTTP(c *gin.Context) {
	comboName := c.Param("combo")
	combo, ok := h.gw.GetCombo(comboName)
	if !ok {
		errStr := "combo not found: " + comboName
		h.recordCall(comboName, "http", "unknown", nil, nil, nil, nil, nil, 0, http.StatusNotFound, false, &errStr)
		c.JSON(http.StatusNotFound, protocol.NewErrorResponse(nil, protocol.CodeInvalidRequest, errStr, nil))
		return
	}

	// Extract user_id exclusively from configured HTTP header
	headerKey := combo.UserIDHeader()
	userID := strings.TrimSpace(c.GetHeader(headerKey))
	var userPtr *string
	if userID != "" {
		userPtr = &userID
	}

	var req protocol.Request
	if err := c.ShouldBindJSON(&req); err != nil {
		errStr := "parse error: " + err.Error()
		h.recordCall(comboName, "http", "unknown", nil, nil, userPtr, nil, nil, 0, http.StatusBadRequest, false, &errStr)
		c.JSON(http.StatusBadRequest, protocol.NewErrorResponse(nil, protocol.CodeParseError, errStr, nil))
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

	resp := h.dispatchMethod(ctx, combo, &req, userID, c.Writer.Header(), "http")
	if resp == nil {
		// Notification - no content returned
		c.Status(http.StatusNoContent)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// HandleMethodNotAllowed returns 405 Method Not Allowed per Streamable HTTP specification.
func (h *Handler) HandleMethodNotAllowed(c *gin.Context) {
	c.Header("Allow", "POST")
	c.JSON(http.StatusMethodNotAllowed, protocol.NewErrorResponse(nil, protocol.CodeInvalidRequest, "only POST is supported for streamable http mcp endpoints", nil))
}

// dispatchMethod routes and executes a JSON-RPC method on the combo.
func (h *Handler) dispatchMethod(ctx context.Context, combo *gateway.Combo, req *protocol.Request, userID string, headers http.Header, transport string) *protocol.Response {
	// If it's a notification (no ID), method execution produces no response
	isNotification := req.ID == nil
	t0 := time.Now()
	var userPtr *string
	if userID != "" {
		userPtr = &userID
	}

	switch req.Method {
	case "initialize":
		res := protocol.InitializeResult{
			ProtocolVersion: protocol.LatestProtocolVersion,
			ServerInfo: protocol.Implementation{
				Name:    "ccrouter-mcp/" + combo.Name(),
				Version: "0.7.0",
			},
			Capabilities: protocol.ServerCapabilities{
				Tools:     &protocol.ToolsCapability{ListChanged: true},
				Resources: &protocol.ResourcesCapability{ListChanged: true},
				Prompts:   &protocol.PromptsCapability{ListChanged: true},
			},
			Instructions: combo.Description(),
		}
		h.recordCall(combo.Name(), transport, req.Method, nil, nil, userPtr, nil, res, int(time.Since(t0).Milliseconds()), 200, true, nil)
		return protocol.NewResponse(req.ID, res)

	case "notifications/initialized":
		return nil

	case "ping":
		res := map[string]any{}
		return protocol.NewResponse(req.ID, res)

	case "tools/list":
		tools, err := combo.ListTools(ctx)
		dur := int(time.Since(t0).Milliseconds())
		if err != nil {
			errStr := err.Error()
			h.recordCall(combo.Name(), transport, req.Method, nil, nil, userPtr, nil, nil, dur, protocol.CodeInternalError, false, &errStr)
			return protocol.NewErrorResponse(req.ID, protocol.CodeInternalError, err.Error(), nil)
		}
		if tools == nil {
			tools = []protocol.Tool{}
		}
		h.recordCall(combo.Name(), transport, req.Method, nil, nil, userPtr, nil, map[string]any{"count": len(tools)}, dur, 200, true, nil)
		return protocol.NewResponse(req.ID, protocol.ListToolsResult{Tools: tools})

	case "tools/call":
		var params protocol.CallToolParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				errStr := "invalid params: " + err.Error()
				h.recordCall(combo.Name(), transport, req.Method, nil, nil, userPtr, nil, nil, int(time.Since(t0).Milliseconds()), protocol.CodeInvalidParams, false, &errStr)
				return protocol.NewErrorResponse(req.ID, protocol.CodeInvalidParams, errStr, nil)
			}
		}
		toolName := params.Name
		provName, _, _ := combo.ResolveTool(toolName)
		var provPtr *string
		if provName != "" {
			provPtr = &provName
		}
		result, rpcErr := combo.CallTool(ctx, params.Name, params.Arguments, userID)
		dur := int(time.Since(t0).Milliseconds())
		if rpcErr != nil {
			if rpcErr.Code == protocol.CodeAuthRequired && headers != nil {
				headers.Set("X-MCP-Auth-Required", "true")
			}
			errStr := rpcErr.Message
			h.recordCall(combo.Name(), transport, req.Method, &toolName, provPtr, userPtr, params.Arguments, rpcErr.Data, dur, rpcErr.Code, false, &errStr)
			if isNotification {
				return nil
			}
			return protocol.NewErrorResponse(req.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
		}
		h.recordCall(combo.Name(), transport, req.Method, &toolName, provPtr, userPtr, params.Arguments, result, dur, 200, true, nil)
		if isNotification {
			return nil
		}
		return protocol.NewResponse(req.ID, result)

	case "resources/list":
		resources, err := combo.ListResources(ctx)
		dur := int(time.Since(t0).Milliseconds())
		if err != nil {
			errStr := err.Error()
			h.recordCall(combo.Name(), transport, req.Method, nil, nil, userPtr, nil, nil, dur, protocol.CodeInternalError, false, &errStr)
			return protocol.NewErrorResponse(req.ID, protocol.CodeInternalError, err.Error(), nil)
		}
		if resources == nil {
			resources = []protocol.Resource{}
		}
		h.recordCall(combo.Name(), transport, req.Method, nil, nil, userPtr, nil, map[string]any{"count": len(resources)}, dur, 200, true, nil)
		return protocol.NewResponse(req.ID, protocol.ListResourcesResult{Resources: resources})

	case "resources/read":
		var params protocol.ReadResourceParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				errStr := "invalid params: " + err.Error()
				h.recordCall(combo.Name(), transport, req.Method, nil, nil, userPtr, nil, nil, int(time.Since(t0).Milliseconds()), protocol.CodeInvalidParams, false, &errStr)
				return protocol.NewErrorResponse(req.ID, protocol.CodeInvalidParams, errStr, nil)
			}
		}
		targetURI := params.URI
		result, rpcErr := combo.ReadResource(ctx, params.URI, userID)
		dur := int(time.Since(t0).Milliseconds())
		if rpcErr != nil {
			if rpcErr.Code == protocol.CodeAuthRequired && headers != nil {
				headers.Set("X-MCP-Auth-Required", "true")
			}
			errStr := rpcErr.Message
			h.recordCall(combo.Name(), transport, req.Method, &targetURI, nil, userPtr, map[string]any{"uri": targetURI}, rpcErr.Data, dur, rpcErr.Code, false, &errStr)
			if isNotification {
				return nil
			}
			return protocol.NewErrorResponse(req.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
		}
		h.recordCall(combo.Name(), transport, req.Method, &targetURI, nil, userPtr, map[string]any{"uri": targetURI}, result, dur, 200, true, nil)
		if isNotification {
			return nil
		}
		return protocol.NewResponse(req.ID, result)

	case "prompts/list":
		prompts, err := combo.ListPrompts(ctx)
		dur := int(time.Since(t0).Milliseconds())
		if err != nil {
			errStr := err.Error()
			h.recordCall(combo.Name(), transport, req.Method, nil, nil, userPtr, nil, nil, dur, protocol.CodeInternalError, false, &errStr)
			return protocol.NewErrorResponse(req.ID, protocol.CodeInternalError, err.Error(), nil)
		}
		if prompts == nil {
			prompts = []protocol.Prompt{}
		}
		h.recordCall(combo.Name(), transport, req.Method, nil, nil, userPtr, nil, map[string]any{"count": len(prompts)}, dur, 200, true, nil)
		return protocol.NewResponse(req.ID, protocol.ListPromptsResult{Prompts: prompts})

	case "prompts/get":
		var params protocol.GetPromptParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				errStr := "invalid params: " + err.Error()
				h.recordCall(combo.Name(), transport, req.Method, nil, nil, userPtr, nil, nil, int(time.Since(t0).Milliseconds()), protocol.CodeInvalidParams, false, &errStr)
				return protocol.NewErrorResponse(req.ID, protocol.CodeInvalidParams, errStr, nil)
			}
		}
		promptName := params.Name
		result, rpcErr := combo.GetPrompt(ctx, params.Name, params.Arguments, userID)
		dur := int(time.Since(t0).Milliseconds())
		if rpcErr != nil {
			if rpcErr.Code == protocol.CodeAuthRequired && headers != nil {
				headers.Set("X-MCP-Auth-Required", "true")
			}
			errStr := rpcErr.Message
			h.recordCall(combo.Name(), transport, req.Method, &promptName, nil, userPtr, params.Arguments, rpcErr.Data, dur, rpcErr.Code, false, &errStr)
			if isNotification {
				return nil
			}
			return protocol.NewErrorResponse(req.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
		}
		h.recordCall(combo.Name(), transport, req.Method, &promptName, nil, userPtr, params.Arguments, result, dur, 200, true, nil)
		if isNotification {
			return nil
		}
		return protocol.NewResponse(req.ID, result)

	default:
		dur := int(time.Since(t0).Milliseconds())
		errStr := "method not found: " + req.Method
		h.recordCall(combo.Name(), transport, req.Method, nil, nil, userPtr, nil, nil, dur, protocol.CodeMethodNotFound, false, &errStr)
		if isNotification {
			return nil
		}
		return protocol.NewErrorResponse(req.ID, protocol.CodeMethodNotFound, errStr, nil)
	}
}
