package server

import (
	"encoding/json"
	"io"
	"net/http"
	"runtime"
	"strconv"

	"ccrouter/internal/config"
	"ccrouter/internal/db"
	"ccrouter/internal/gateway"

	"github.com/gin-gonic/gin"
)

func goRuntime() string { return runtime.Version() }

const version = "0.2.0"

func stateOf(c *gin.Context) *gateway.State {
	v, _ := c.Get("state")
	s, _ := v.(*gateway.State)
	return s
}

// ---- Config ----

func getConfig(c *gin.Context) {
	svc := stateOf(c).Service()
	c.JSON(http.StatusOK, config.Dump(svc.Config))
}

func putConfig(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request body must be a JSON object"})
		return
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request body must be a JSON object"})
		return
	}
	newConfig, err := config.Build(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	st := stateOf(c)
	if err := st.SaveAndReload(newConfig); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reload failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, config.Dump(newConfig))
}

// ---- Stats keys ----

func statsKeys(c *gin.Context) {
	keysStatus(c, stateOf(c))
}

// ---- Stats summary / trend ----

func statsSummary(c *gin.Context) {
	st := stateOf(c)
	groupBy := c.DefaultQuery("group_by", "combo")
	var since, until *float64
	since = floatQuery(c, "since")
	until = floatQuery(c, "until")
	rows, err := db.QueryStats(st.Recorder().DBPath(), groupBy, since, until)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "group_by": groupBy})
}

func statsTrend(c *gin.Context) {
	st := stateOf(c)
	bucket := c.DefaultQuery("bucket", "hour")
	since := floatQuery(c, "since")
	until := floatQuery(c, "until")
	rows, err := db.QueryTrend(st.Recorder().DBPath(), bucket, since, until)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "bucket": bucket})
}

func floatQuery(c *gin.Context, key string) *float64 {
	s := c.Query(key)
	if s == "" {
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &f
}

// ---- Requests ----

func listRequests(c *gin.Context) {
	st := stateOf(c)
	limit := intQuery(c, "limit", 50)
	offset := intQuery(c, "offset", 0)
	var combo, provider, model *string
	if s := c.Query("combo"); s != "" {
		combo = &s
	}
	if s := c.Query("provider"); s != "" {
		provider = &s
	}
	if s := c.Query("model"); s != "" {
		model = &s
	}
	var success *bool
	if s := c.Query("success"); s != "" {
		b, _ := strconv.ParseBool(s)
		success = &b
	}
	since := floatQuery(c, "since")
	until := floatQuery(c, "until")
	result, err := db.QueryList(st.Recorder().DBPath(), limit, offset, combo, provider, model, success, since, until)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func intQuery(c *gin.Context, key string, def int) int {
	s := c.Query(key)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// ---- Info ----

func adminInfo(c *gin.Context) {
	svc := stateOf(c).Service()
	combos := []map[string]any{}
	for i := range svc.Config.Combos {
		combo := &svc.Config.Combos[i]
		members := []map[string]any{}
		for _, m := range combo.Members {
			members = append(members, map[string]any{"provider": m.Provider, "model": m.Model})
		}
		aliases := combo.Aliases
		if aliases == nil {
			aliases = []string{}
		}
		combos = append(combos, map[string]any{
			"name":        combo.Name,
			"aliases":     aliases,
			"api_formats": combo.APIFormats(),
			"strategy":    combo.Strategy,
			"members":     members,
		})
	}
	providers := []map[string]any{}
	for i := range svc.Config.Providers {
		p := &svc.Config.Providers[i]
		formats := []string{}
		for _, ep := range p.APIs {
			formats = append(formats, ep.APIFormat)
		}
		providers = append(providers, map[string]any{
			"name":        p.Name,
			"api_formats": formats,
			"key_count":   len(p.Keys),
			"strategy":    p.KeyStrategy,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"version":   version,
		"runtime":   goRuntime(),
		"combos":    combos,
		"providers": providers,
	})
}

// ---- Health ----

func adminHealth(c *gin.Context) {
	st := stateOf(c)
	svc := st.Service()
	dbInfo := map[string]any{}
	if st.Recorder() != nil {
		dbInfo = map[string]any{
			"queue_size":    st.Recorder().QueueSize(),
			"dropped_count": st.Recorder().DroppedCount(),
			"db_path":       st.Recorder().DBPath(),
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"runtime": goRuntime(),
		"config": gin.H{
			"providers": len(svc.Config.Providers),
			"combos":    len(svc.Config.Combos),
		},
		"db": dbInfo,
	})
}
