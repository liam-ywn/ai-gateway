package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/yewintnaing/ai-gateway/internal/api"
	"github.com/yewintnaing/ai-gateway/internal/config"
	"github.com/yewintnaing/ai-gateway/internal/observability"
	"github.com/yewintnaing/ai-gateway/internal/providers"
	"github.com/yewintnaing/ai-gateway/internal/providers/anthropic"
	"github.com/yewintnaing/ai-gateway/internal/providers/openai"
	"github.com/yewintnaing/ai-gateway/internal/ratelimit"
	"github.com/yewintnaing/ai-gateway/internal/router"
	"github.com/yewintnaing/ai-gateway/internal/usage"
)

func main() {
	// 1. Initial Context and OTEL
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdownOTEL, err := observability.InitOTEL(ctx, "ai-gateway")
	if err != nil {
		log.Fatalf("Failed to initialize OTEL: %v", err)
	}
	defer shutdownOTEL(context.Background())

	// 2. Load Config
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 3. Initialize Usage Store (Postgres)
	store, err := usage.NewStore(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer store.Close()

	// 4. Run Migrations
	if err := store.Migrate(ctx, "migrations/001_create_requests.sql"); err != nil {
		log.Printf("Warning: Migration 001 failed: %v", err)
	}
	if err := store.Migrate(ctx, "migrations/002_create_provider_attempts.sql"); err != nil {
		log.Printf("Warning: Migration 002 failed: %v", err)
	}
	if err := store.Migrate(ctx, "migrations/003_add_unique_to_requests.sql"); err != nil {
		log.Printf("Warning: Migration 003 failed: %v", err)
	}

	// 5. Initialize Rate Limiter
	limiter, err := ratelimit.NewLimiter(cfg.RedisURL, cfg.TPM)
	if err != nil {
		log.Printf("Warning: Redis not available, rate limiting disabled: %v", err)
	}

	// 6. Initialize Providers
	registry := providers.Registry{
		"openai":    openai.NewProvider(cfg.OpenAIKey, cfg.OpenAIURL, cfg.OpenAIVersion),
		"anthropic": anthropic.NewProvider(cfg.AnthropicKey, cfg.AnthropicURL, cfg.AnthropicVersion),
	}

	// 7. Initialize Components
	rt := router.NewRouter(cfg.Routes)
	h := api.NewHandler(rt, registry, store, limiter)

	// 8. Setup Router
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Post("/v1/chat/completions", h.HandleChat)
	r.Get("/health", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// 9. Start Server
	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	go func() {
		log.Printf("AI Gateway Phase 2 starting on port %s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("ListenAndServe failed: %v", err)
		}
	}()

	// Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down AI Gateway...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("AI Gateway exited correctly")
}
