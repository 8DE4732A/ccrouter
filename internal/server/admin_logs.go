package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func listLogs(c *gin.Context) {
	st := stateOf(c)
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

func getLogSettings(c *gin.Context) {
	svc := stateOf(c).Service()
	c.JSON(http.StatusOK, gin.H{"verbose_logging": svc.Config.VerboseLogging})
}

func putLogSettings(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": `request body must be {"enabled": bool}`})
		return
	}
	var payload struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": `request body must be {"enabled": bool}`})
		return
	}

	st := stateOf(c)
	newConfig := *st.Service().Config
	newConfig.VerboseLogging = payload.Enabled
	if err := st.SaveAndReload(&newConfig); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reload failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"verbose_logging": payload.Enabled})
}
