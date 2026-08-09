package gateway

import (
	"net/http"
	"os"
	"sync"
	"time"

	"ccrouter/internal/combos"
	"ccrouter/internal/config"
	"ccrouter/internal/db"
	"ccrouter/internal/keys"
	"ccrouter/internal/proxy"
	"ccrouter/internal/report"
)

// State owns the shared HTTP client and the hot-swappable proxy service snapshot.
// All proxy requests take `s.service` once at the top of a request and keep that
// reference for the whole call (including streaming), so in-flight requests use
// the old snapshot while new requests pick up the new one.
type State struct {
	mu         sync.RWMutex
	configPath string
	service    *proxy.Service
	recorder   *db.Recorder
	report     *report.Logger
	client     *http.Client
}

// New creates a State from config, building key managers and the proxy service.
func New(cfg *config.AppConfig, configPath string, rec *db.Recorder, rl *report.Logger) (*State, error) {
	client := &http.Client{Timeout: 120 * time.Second}
	svc, err := buildService(cfg, nil, client, rec, rl)
	if err != nil {
		return nil, err
	}
	return &State{
		configPath: configPath,
		service:    svc,
		recorder:   rec,
		report:     rl,
		client:     client,
	}, nil
}

func buildService(cfg *config.AppConfig, prevKMs map[string]*keys.Manager,
	client *http.Client, rec *db.Recorder, rl *report.Logger) (*proxy.Service, error) {

	kms := make(map[string]*keys.Manager, len(cfg.Providers))
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		ks := make([]string, 0, len(p.Keys))
		for _, k := range p.Keys {
			ks = append(ks, k.Key)
		}
		km := keys.NewManager(p.Name, ks, p.KeyStrategy)
		if prev, ok := prevKMs[p.Name]; ok {
			km.MergeStats(prev)
		}
		kms[p.Name] = km
	}
	return proxy.New(cfg, kms, combos.NewRouter(cfg.Combos), client, rec, rl)
}

// Service returns the current proxy service snapshot.
func (s *State) Service() *proxy.Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.service
}

// Recorder returns the recorder.
func (s *State) Recorder() *db.Recorder { return s.recorder }

// Report returns the report logger.
func (s *State) Report() *report.Logger { return s.report }

// ConfigPath returns the config file path.
func (s *State) ConfigPath() string { return s.configPath }

// Reload atomically replaces the proxy service built from newConfig.
func (s *State) Reload(newConfig *config.AppConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.service.KeyManagers
	newSvc, err := buildService(newConfig, prev, s.client, s.recorder, s.report)
	if err != nil {
		return err
	}
	s.service = newSvc
	return nil
}

// SaveAndReload validates, atomically writes YAML, then hot-reloads.
func (s *State) SaveAndReload(newConfig *config.AppConfig) error {
	yamlText, err := config.ToYAML(newConfig)
	if err != nil {
		return err
	}
	tmpPath := s.configPath + ".tmp"
	if err := os.WriteFile(tmpPath, yamlText, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.configPath); err != nil {
		return err
	}
	return s.Reload(newConfig)
}

// Close shuts down the recorder and report logger.
func (s *State) Close() {
	if s.recorder != nil {
		s.recorder.Close()
	}
	if s.report != nil {
		s.report.Close()
	}
}
