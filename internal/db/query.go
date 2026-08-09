package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// QueryStats returns per-group aggregated stats from the request log.
func QueryStats(dbPath, groupBy string, since, until *float64) ([]map[string]any, error) {
	valid := map[string]bool{"combo": true, "model": true, "provider": true, "key_prefix": true}
	if !valid[groupBy] {
		groupBy = "combo"
	}
	where, params := timeFilter(since, until)
	sql := fmt.Sprintf(`
		SELECT
			%s                       AS group_key,
			COUNT(*)                         AS total,
			SUM(success)                     AS success_count,
			COUNT(*) - SUM(success)          AS error_count,
			SUM(COALESCE(total_tokens, 0))   AS total_tokens,
			SUM(COALESCE(prompt_tokens, 0))  AS prompt_tokens,
			SUM(COALESCE(completion_tokens,0)) AS completion_tokens,
			SUM(COALESCE(cache_read_tokens, 0))  AS cache_read_tokens,
			SUM(COALESCE(cache_write_tokens, 0)) AS cache_write_tokens,
			AVG(NULLIF(duration_ms, 0))      AS avg_duration_ms
		FROM requests
		%s
		GROUP BY %s
		ORDER BY total DESC`, groupBy, where, groupBy)
	return queryRows(dbPath, sql, params...)
}

// QueryTrend returns time-bucketed request counts and token sums.
func QueryTrend(dbPath, bucket string, since, until *float64) ([]map[string]any, error) {
	bucketSeconds := map[string]int64{"hour": 3600, "day": 86400, "minute": 60}[bucket]
	if bucketSeconds == 0 {
		bucketSeconds = 3600
	}
	where, params := timeFilter(since, until)
	sql := fmt.Sprintf(`
		SELECT
			CAST(ts / %d AS INTEGER) * %d AS bucket_ts,
			COUNT(*)                          AS total,
			SUM(success)                      AS success_count,
			SUM(COALESCE(total_tokens, 0))    AS total_tokens
		FROM requests
		%s
		GROUP BY bucket_ts
		ORDER BY bucket_ts`, bucketSeconds, bucketSeconds, where)
	return queryRows(dbPath, sql, params...)
}

// QueryList returns a paginated list of raw request records plus total count.
func QueryList(dbPath string, limit, offset int, combo, provider, model *string, success *bool, since, until *float64) (map[string]any, error) {
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
	if combo != nil {
		filters = append(filters, "combo = ?")
		params = append(params, *combo)
	}
	if provider != nil {
		filters = append(filters, "provider = ?")
		params = append(params, *provider)
	}
	if model != nil {
		filters = append(filters, "model = ?")
		params = append(params, *model)
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

	conn, err := openReadOnly(dbPath)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var total int
	err = conn.QueryRow("SELECT COUNT(*) FROM requests "+where, params...).Scan(&total)
	if err != nil {
		return nil, err
	}

	listSQL := "SELECT * FROM requests " + where + " ORDER BY ts DESC LIMIT ? OFFSET ?"
	rowsQ := append(append([]any{}, params...), limit, offset)
	rows, err := conn.Query(listSQL, rowsQ...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, c := range cols {
			row[c] = normalizeValue(vals[i])
		}
		items = append(items, row)
	}
	return map[string]any{"total": float64(total), "items": items}, nil
}

func queryRows(dbPath, sql string, params ...any) ([]map[string]any, error) {
	conn, err := openReadOnly(dbPath)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	rows, err := conn.Query(sql, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, c := range cols {
			row[c] = normalizeValue(vals[i])
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func openReadOnly(dbPath string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=journal_mode(WAL)")
}

func normalizeValue(v any) any {
	switch t := v.(type) {
	case int64:
		return float64(t)
	case []byte:
		return string(t)
	default:
		return v
	}
}

func timeFilter(since, until *float64) (string, []any) {
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
	if len(filters) == 0 {
		return "", params
	}
	return "WHERE " + strings.Join(filters, " AND "), params
}
