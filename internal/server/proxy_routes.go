package server

import (
	"net/http"
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
		for _, id := range append([]string{combo.Name}, combo.Aliases...) {
			models = append(models, map[string]any{
				"id":       id,
				"object":   "model",
				"created":  0,
				"owned_by": "sense-roll",
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": models})
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
