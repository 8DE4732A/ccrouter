package server

import (
	"net/http"
	"strings"
	"time"

	"ccrouter/internal/config"
	"ccrouter/internal/gateway"

	"github.com/gin-gonic/gin"
)

// listModels returns available models (combos + aliases) in OpenAI-compatible format.
func listModels(c *gin.Context, state *gateway.State) {
	svc := state.Service()
	models := []map[string]any{}
	for i := range svc.Config.Combos {
		combo := &svc.Config.Combos[i]
		ownedBy := combo.OwnedBy
		if ownedBy == "" {
			ownedBy = "default"
		}
		ids := append([]string{combo.FullName()}, combo.FullAliases()...)
		for _, id := range ids {
			models = append(models, map[string]any{
				"id":       id,
				"object":   "model",
				"created":  0,
				"owned_by": ownedBy,
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": models})
}

// listGeminiModels returns available models in Gemini-compatible format {"models": [...]}.
func listGeminiModels(c *gin.Context, state *gateway.State) {
	svc := state.Service()
	models := []map[string]any{}
	for i := range svc.Config.Combos {
		combo := &svc.Config.Combos[i]
		if !combo.SupportsFormat("gemini") {
			continue
		}
		ids := append([]string{combo.FullName()}, combo.FullAliases()...)
		for _, id := range ids {
			models = append(models, map[string]any{
				"name":                       "models/" + id,
				"displayName":                id,
				"supportedGenerationMethods": []string{"generateContent", "streamGenerateContent"},
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"models": models})
}

// getGeminiModel returns details for a single model in Gemini-compatible format.
func getGeminiModel(c *gin.Context, state *gateway.State) {
	name := c.Param("model_name")
	name = strings.TrimPrefix(name, "/")
	name = strings.TrimPrefix(name, "models/")
	name = strings.Trim(name, "/")
	svc := state.Service()
	combo := svc.Router.GetCombo(name)
	if combo == nil || !combo.SupportsFormat("gemini") {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"code":    http.StatusNotFound,
				"message": "models/" + name + " not found",
				"status":  "NOT_FOUND",
			},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"name":                       "models/" + name,
		"displayName":                name,
		"supportedGenerationMethods": []string{"generateContent", "streamGenerateContent"},
	})
}

func healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

func keysStatus(c *gin.Context, state *gateway.State) {
	svc := state.Service()
	providers := []map[string]any{}
	names := providerNames(svc.Config.Providers)
	for _, name := range names {
		if km, ok := svc.KeyManagers[name]; ok {
			providers = append(providers, km.Stats())
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"combos":    svc.Router.List(),
		"providers": providers,
	})
}

func providerNames(providers []config.ProviderConfig) []string {
	out := make([]string, 0, len(providers))
	for i := range providers {
		out = append(out, providers[i].Name)
	}
	return out
}
