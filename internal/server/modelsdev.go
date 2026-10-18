package server

import (
	"net/http"
	"strings"

	"ccrouter/internal/modelsdev"

	"github.com/gin-gonic/gin"
)

// modelsDevSearch handles GET /admin/api/modelsdev/search.
func modelsDevSearch(c *gin.Context) {
	client := modelsDevClient(c)
	if client == nil {
		c.JSON(http.StatusOK, gin.H{"items": []any{}, "status": modelsDevUnavailable()})
		return
	}
	limit := intQuery(c, "limit", 20)
	items := client.Search(strings.TrimSpace(c.Query("q")), strings.TrimSpace(c.Query("provider")), limit)
	c.JSON(http.StatusOK, gin.H{"items": items, "status": client.Status()})
}

// modelsDevSuggest handles GET /admin/api/modelsdev/suggest.
func modelsDevSuggest(c *gin.Context) {
	client := modelsDevClient(c)
	if client == nil {
		c.JSON(http.StatusOK, gin.H{"items": []any{}, "status": modelsDevUnavailable()})
		return
	}
	model := strings.TrimSpace(c.Query("model"))
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
		return
	}
	limit := intQuery(c, "limit", 20)
	items := client.Suggest(model, strings.TrimSpace(c.Query("provider")), limit)
	c.JSON(http.StatusOK, gin.H{"items": items, "status": client.Status()})
}

// modelsDevLookup handles GET /admin/api/modelsdev/lookup.
func modelsDevLookup(c *gin.Context) {
	client := modelsDevClient(c)
	source := strings.TrimSpace(c.Query("source"))
	if source == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "source is required"})
		return
	}
	if client == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "models.dev is unavailable"})
		return
	}
	entry, ok := client.Lookup(source)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "entry not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"item": entry, "status": client.Status()})
}

// modelsDevGetStatus handles GET /admin/api/modelsdev/status.
func modelsDevGetStatus(c *gin.Context) {
	client := modelsDevClient(c)
	if client == nil {
		c.JSON(http.StatusOK, modelsDevUnavailable())
		return
	}
	c.JSON(http.StatusOK, client.Status())
}

// modelsDevRefresh handles POST /admin/api/modelsdev/refresh.
func modelsDevRefresh(c *gin.Context) {
	client := modelsDevClient(c)
	if client == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "models.dev client unavailable"})
		return
	}
	if client.Status().Disabled {
		c.JSON(http.StatusConflict, gin.H{"error": "models.dev is disabled"})
		return
	}
	status, err := client.Refresh(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "status": status})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": status})
}

func modelsDevClient(c *gin.Context) *modelsdev.Client {
	st := stateOf(c)
	if st == nil {
		return nil
	}
	return st.ModelsDev()
}

func modelsDevUnavailable() modelsdev.Status {
	return modelsdev.Status{Disabled: true, Stale: true}
}
