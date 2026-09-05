package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"ccrouter/internal/config"

	"github.com/gin-gonic/gin"
)

func listLogs(c *gin.Context) {
	st := stateOf(c)
	if st.Report() == nil {
		c.JSON(http.StatusOK, gin.H{"items": []any{}, "has_more": false})
		return
	}
	limit := intQuery(c, "limit", 20)
	offset := intQuery(c, "offset", 0)
	var success *bool
	if s := c.Query("success"); s != "" {
		b, err := strconv.ParseBool(s)
		if err == nil {
			success = &b
		}
	}
	result, err := st.Report().Read(limit, offset, success)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func getLogDetail(c *gin.Context) {
	st := stateOf(c)
	if st.Report() == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "record not found"})
		return
	}
	tsStr := c.Param("ts")
	ts, err := strconv.ParseFloat(tsStr, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ts: must be a float64 unix timestamp"})
		return
	}
	rec, err := st.Report().ReadOne(ts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if rec == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "record not found"})
		return
	}
	c.JSON(http.StatusOK, rec)
}

func getLogSettings(c *gin.Context) {
	cfg := stateOf(c).Service().Config
	c.JSON(http.StatusOK, gin.H{
		"verbose_logging":   cfg.VerboseLogging,
		"enabled":           cfg.Logging.Enabled,
		"dir":               cfg.Logging.Dir,
		"max_file_size_mb":  cfg.Logging.MaxFileSizeMB,
		"max_backups":       cfg.Logging.MaxBackups,
		"compression_level": cfg.Logging.CompressionLevel,
	})
}

type putLogSettingsPayload struct {
	Enabled          *bool   `json:"enabled"`
	VerboseLogging   *bool   `json:"verbose_logging"`
	Dir              *string `json:"dir"`
	MaxFileSizeMB    *int    `json:"max_file_size_mb"`
	MaxBackups       *int    `json:"max_backups"`
	CompressionLevel *string `json:"compression_level"`
}

func putLogSettings(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}
	var payload putLogSettingsPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}

	st := stateOf(c)
	newConfig := *st.Service().Config

	if payload.Dir != nil {
		dir := strings.TrimSpace(*payload.Dir)
		if dir == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "dir must not be empty"})
			return
		}
		targetDir := config.ResolveLogDir(st.ConfigPath(), dir)
		if err := config.TestDirWritable(targetDir); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("log directory %q is not writable: %v", dir, err)})
			return
		}
		newConfig.Logging.Dir = dir
	}

	if payload.MaxFileSizeMB != nil && (*payload.MaxFileSizeMB < 1 || *payload.MaxFileSizeMB > 2048) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "max_file_size_mb must be between 1 and 2048"})
		return
	}
	if payload.MaxBackups != nil && (*payload.MaxBackups < 1 || *payload.MaxBackups > 100) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "max_backups must be between 1 and 100"})
		return
	}
	if payload.CompressionLevel != nil {
		lvl := strings.ToLower(strings.TrimSpace(*payload.CompressionLevel))
		switch lvl {
		case "fastest", "default", "better", "best":
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid compression_level: must be fastest, default, better, or best"})
			return
		}
	}

	if payload.Enabled != nil {
		newConfig.Logging.Enabled = *payload.Enabled
		newConfig.VerboseLogging = *payload.Enabled
	} else if payload.VerboseLogging != nil {
		newConfig.Logging.Enabled = *payload.VerboseLogging
		newConfig.VerboseLogging = *payload.VerboseLogging
	}
	if payload.MaxFileSizeMB != nil {
		newConfig.Logging.MaxFileSizeMB = *payload.MaxFileSizeMB
	}
	if payload.MaxBackups != nil {
		newConfig.Logging.MaxBackups = *payload.MaxBackups
	}
	if payload.CompressionLevel != nil {
		newConfig.Logging.CompressionLevel = strings.ToLower(strings.TrimSpace(*payload.CompressionLevel))
	}

	if err := st.SaveAndReload(&newConfig); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reload failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"verbose_logging":   newConfig.VerboseLogging,
		"enabled":           newConfig.Logging.Enabled,
		"dir":               newConfig.Logging.Dir,
		"max_file_size_mb":  newConfig.Logging.MaxFileSizeMB,
		"max_backups":       newConfig.Logging.MaxBackups,
		"compression_level": newConfig.Logging.CompressionLevel,
	})
}
