// Command education-demo-server runs the classroom monitoring demo.
//
// Every model call goes through llm-bridge-server's oneshot endpoint, which
// runs on the Claude Code subscription. This process reads no API key.
package main

import (
	"context"
	"errors"
	"flag"
	"github.com/kayushkin/education-demo/internal/bridgeauth"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kayushkin/education-demo/internal/agent"
	"github.com/kayushkin/education-demo/internal/server"
	"github.com/kayushkin/education-demo/internal/store"
)

func main() {
	var (
		addr      = flag.String("addr", envOr("EDUCATION_DEMO_ADDR", "127.0.0.1:8316"), "listen address")
		dbPath    = flag.String("db", envOr("EDUCATION_DEMO_DB", defaultDBPath()), "sqlite database path")
		webDir    = flag.String("web", envOr("EDUCATION_DEMO_WEB", ""), "directory of the built front end")
		basePath  = flag.String("base-path", envOr("EDUCATION_DEMO_BASE_PATH", ""), "path prefix, e.g. /education-demo")
		bridgeURL = flag.String("bridge-url", envOr("LLM_BRIDGE_URL", "http://localhost:8160"), "llm-bridge-server base URL")
		instance  = flag.String("instance", envOr("EDUCATION_DEMO_INSTANCE", "inst-cc-local"), "llm-bridge instance id for model calls")
		modelID   = flag.String("model", envOr("EDUCATION_DEMO_MODEL", ""), "override the instance's default model")
		interval  = flag.Duration("monitor-interval", 25*time.Second, "gap between assessment rounds")
	)
	flag.Parse()

	// llm-bridge-server gates every route; stamp this process's calls to it
	// with the service token. Host-scoped, so nothing else sees the token.
	if err := bridgeauth.StampRequestsToBridge(*bridgeURL); err != nil {
		log.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		log.Fatalf("create database directory: %v", err)
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()
	log.Printf("database: %s", *dbPath)

	client := agent.New(*bridgeURL, *instance, *modelID)

	// Check the model path at boot and say plainly what we found. A service
	// that starts clean and only reveals at the first round that it cannot
	// reach the bridge wastes the one thing a live demo has least of.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	agentErr := client.Healthy(ctx)
	cancel()
	agentReady := agentErr == nil
	agentErrText := ""
	if agentReady {
		log.Printf("llm-bridge: instance %q at %s is ready", *instance, *bridgeURL)
	} else {
		agentErrText = agentErr.Error()
		// Not fatal: participation monitoring and the fallback transcripts
		// still work, and a demo that boots degraded beats one that will not
		// boot. It is logged as a warning so nobody mistakes it for healthy.
		log.Printf("WARNING: llm-bridge is not usable (%v)", agentErr)
		log.Printf("WARNING: sessions will run on fallback transcripts and participation-only monitoring")
	}

	srv := server.New(server.Config{
		Store: st, Agent: client, BasePath: *basePath, WebDir: *webDir,
		MonitorInterval: *interval, AgentReady: agentReady, AgentError: agentErrText,
	})

	httpServer := &http.Server{
		Addr:    *addr,
		Handler: srv.Handler(),
		// No WriteTimeout: the SSE stream is a long-lived response and any
		// finite write deadline cuts every dashboard off mid-session.
		ReadHeaderTimeout: 10 * time.Second,
	}

	// A restart must not leave a session marked running with nothing driving
	// it. Anything the database still calls live gets its playback and
	// monitoring back.
	srv.ResumeInterruptedSessions(context.Background())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("listening on %s (base path %q, web dir %q)", *addr, *basePath, *webDir)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-stop
	log.Printf("shutting down")
	srv.Shutdown()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "education-demo.db"
	}
	return filepath.Join(home, ".config", "education-demo", "education-demo.db")
}
