package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ccrouter/internal/config"
	"ccrouter/internal/db"
	"ccrouter/internal/gateway"
	"ccrouter/internal/report"
	"ccrouter/internal/script"
	"ccrouter/internal/server"
)

func main() {
	var (
		host           = flag.String("host", "127.0.0.1", "Bind host")
		port           = flag.Int("port", 8000, "Bind port")
		configPathFlag = flag.String("c", "", "Path to config yaml (default: $SENSE_ROLL_CONFIG or config.yaml)")
	)
	flag.Parse()

	// Wire script compiler into config validation so PUT /config catches syntax errors.
	config.ScriptCompileFunc = func(src string) error {
		_, err := script.Compile(src)
		return err
	}

	configPath := *configPathFlag
	if configPath == "" {
		configPath = os.Getenv("SENSE_ROLL_CONFIG")
	}
	if configPath == "" {
		configPath = "config.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}
	log.Printf("Loaded config: providers=%d, combos=%d", len(cfg.Providers), len(cfg.Combos))
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		fmts := make([]string, 0, len(p.APIs))
		for _, ep := range p.APIs {
			fmts = append(fmts, ep.APIFormat)
		}
		log.Printf("  Provider %q: formats=%v keys=%d rules=%d",
			p.Name, fmts, len(p.Keys), len(p.HealthCheckRules))
	}

	dbPath := db.PathForConfig(configPath)
	recorder, err := db.NewRecorder(dbPath)
	if err != nil {
		log.Fatalf("SQLite recorder init failed: %v", err)
	}
	log.Printf("SQLite recorder initialised at %s", dbPath)

	logDir := "logs"
	if cwd, err := os.Getwd(); err == nil {
		logDir = cwd + "/logs"
	}
	reportLogger, err := report.New(logDir)
	if err != nil {
		log.Fatalf("Report logger init failed: %v", err)
	}
	log.Printf("Report logger initialised at %s (verbose_logging=%v)", logDir, cfg.VerboseLogging)

	st, err := gateway.New(cfg, configPath, recorder, reportLogger)
	if err != nil {
		log.Fatalf("Gateway init failed: %v", err)
	}

	router := server.Router(st)
	addr := fmt.Sprintf("%s:%d", *host, *port)
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	// Graceful shutdown on SIGTERM / SIGINT.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		log.Printf("ccrouter listening on http://%s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("ListenAndServe: %v", err)
		}
	}()

	<-quit
	log.Println("Shutting down — draining in-flight requests (up to 15s)…")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	// Flush recorder queue and report logger before exit.
	recorder.Flush()
	st.Close()
	log.Println("ccrouter stopped")
}
