package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// McpRequestItem represents a single row returned when querying mcp_requests.
type McpRequestItem struct {
	ID         int64   `json:"id"`
	TS         float64 `json:"ts"`
	Combo      string  `json:"combo"`
	Transport  string  `json:"transport"`
	Method     string  `json:"method"`
	ToolName   *string `json:"tool_name"`
	Provider   *string `json:"provider"`
	UserID     *string `json:"user_id"`
	Arguments  *string `json:"arguments"`
	Result     *string `json:"result"`
	DurationMs *int    `json:"duration_ms"`
	StatusCode *int    `json:"status_code"`
	Success    int     `json:"success"`
	Error      *string `json:"error"`
}

// QueryMcpList returns a paginated list of MCP request records plus total count.
func QueryMcpList(dbPath string, limit, offset int, combo, provider, toolName, userID, method, transport, q *string, success *bool, since, until *float64) (map[string]any, error) {
	var filters []string
	var params []any

	if since != nil {
		filters = append(filters, "ts >= ?")
		params = append(params, *since)
	}
	if until != nil {
		filters = append(filters, "ts <= ?")
		params = append(params, *until)
	}
	if combo != nil && *combo != "" {
		filters = append(filters, "combo = ?")
		params = append(params, *combo)
	}
	if provider != nil && *provider != "" {
		filters = append(filters, "provider = ?")
		params = append(params, *provider)
	}
	if toolName != nil && *toolName != "" {
		filters = append(filters, "tool_name LIKE ?")
		params = append(params, "%"+*toolName+"%")
	}
	if userID != nil && *userID != "" {
		filters = append(filters, "user_id = ?")
		params = append(params, *userID)
	}
	if method != nil && *method != "" {
		filters = append(filters, "method = ?")
		params = append(params, *method)
	}
	if transport != nil && *transport != "" {
		filters = append(filters, "transport = ?")
		params = append(params, *transport)
	}
	if q != nil && *q != "" {
		filters = append(filters, "(tool_name LIKE ? OR combo LIKE ? OR provider LIKE ? OR user_id LIKE ? OR error LIKE ? OR arguments LIKE ?)")
		kw := "%" + *q + "%"
		params = append(params, kw, kw, kw, kw, kw, kw)
	}
	if success != nil {
		v := 0
		if *success {
			v = 1
		}
		filters = append(filters, "success = ?")
		params = append(params, v)
	}

	where := ""
	if len(filters) > 0 {
		where = "WHERE " + strings.Join(filters, " AND ")
	}

	db, err := sql.Open("sqlite", fmt.Sprintf("%s?mode=ro&_pragma=busy_timeout(5000)", dbPath))
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var total int
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM mcp_requests %s", where)
	if err := db.QueryRow(countSQL, params...).Scan(&total); err != nil {
		return nil, err
	}

	querySQL := fmt.Sprintf(`
		SELECT
			id, ts, combo, transport, method,
			tool_name, provider, user_id,
			arguments, result, duration_ms,
			status_code, success, error
		FROM mcp_requests
		%s
		ORDER BY ts DESC, id DESC
		LIMIT ? OFFSET ?`, where)

	queryParams := append(params, limit, offset)
	rows, err := db.Query(querySQL, queryParams...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []McpRequestItem{}
	for rows.Next() {
		var r McpRequestItem
		if err := rows.Scan(
			&r.ID, &r.TS, &r.Combo, &r.Transport, &r.Method,
			&r.ToolName, &r.Provider, &r.UserID,
			&r.Arguments, &r.Result, &r.DurationMs,
			&r.StatusCode, &r.Success, &r.Error,
		); err != nil {
			return nil, err
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return map[string]any{
		"total": total,
		"items": items,
	}, nil
}

// QueryMcpOverviewStats returns overall KPI metrics for the MCP overview.
func QueryMcpOverviewStats(dbPath string, since, until *float64) (map[string]any, error) {
	where, params := timeFilter(since, until)
	sqlStr := fmt.Sprintf(`
		SELECT
			COUNT(*)                                      AS total_calls,
			COALESCE(SUM(success), 0)                     AS success_calls,
			COUNT(*) - COALESCE(SUM(success), 0)          AS error_calls,
			COALESCE(AVG(NULLIF(duration_ms, 0)), 0)      AS avg_duration_ms,
			COUNT(DISTINCT NULLIF(tool_name, ''))         AS unique_tools,
			COUNT(DISTINCT NULLIF(provider, ''))          AS active_providers,
			COUNT(DISTINCT NULLIF(combo, ''))             AS active_combos,
			COUNT(DISTINCT NULLIF(user_id, ''))           AS active_users
		FROM mcp_requests
		%s`, where)
	rows, err := queryRows(dbPath, sqlStr, params...)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		return rows[0], nil
	}
	return map[string]any{
		"total_calls":      float64(0),
		"success_calls":    float64(0),
		"error_calls":      float64(0),
		"avg_duration_ms":  float64(0),
		"unique_tools":     float64(0),
		"active_providers": float64(0),
		"active_combos":    float64(0),
		"active_users":     float64(0),
	}, nil
}

// QueryMcpStats returns per-group aggregated stats from mcp_requests.
func QueryMcpStats(dbPath, groupBy string, since, until *float64) ([]map[string]any, error) {
	valid := map[string]string{
		"combo":     "combo",
		"provider":  "provider",
		"tool":      "tool_name",
		"tool_name": "tool_name",
		"method":    "method",
		"transport": "transport",
	}
	col := valid[groupBy]
	if col == "" {
		col = "combo"
		groupBy = "combo"
	}
	where, params := timeFilter(since, until)
	sqlStr := fmt.Sprintf(`
		SELECT
			COALESCE(NULLIF(%s, ''), '(none)')        AS group_key,
			COUNT(*)                                   AS total,
			COALESCE(SUM(success), 0)                  AS success_count,
			COUNT(*) - COALESCE(SUM(success), 0)       AS error_count,
			COALESCE(AVG(NULLIF(duration_ms, 0)), 0)   AS avg_duration_ms,
			COALESCE(MIN(NULLIF(duration_ms, 0)), 0)   AS min_duration_ms,
			COALESCE(MAX(NULLIF(duration_ms, 0)), 0)   AS max_duration_ms
		FROM mcp_requests
		%s
		GROUP BY %s
		ORDER BY total DESC`, col, where, col)
	return queryRows(dbPath, sqlStr, params...)
}

// QueryMcpTrend returns time-bucketed request counts from mcp_requests.
func QueryMcpTrend(dbPath, bucket string, since, until *float64) ([]map[string]any, error) {
	bucketSeconds := map[string]int64{"hour": 3600, "day": 86400, "minute": 60}[bucket]
	if bucketSeconds == 0 {
		bucketSeconds = 3600
	}
	where, params := timeFilter(since, until)
	sqlStr := fmt.Sprintf(`
		SELECT
			CAST(ts / %d AS INTEGER) * %d AS bucket_ts,
			COUNT(*)                      AS total,
			COALESCE(SUM(success), 0)     AS success_count
		FROM mcp_requests
		%s
		GROUP BY bucket_ts
		ORDER BY bucket_ts`, bucketSeconds, bucketSeconds, where)
	return queryRows(dbPath, sqlStr, params...)
}
