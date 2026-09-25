// Command server runs the Go API gateway: an HTTP front door that enforces
// distributed rate limits via atomic Lua scripts in Redis.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/manan/distributed-rate-limiter/go/internal/buffer"
	"github.com/manan/distributed-rate-limiter/go/internal/config"
	"github.com/manan/distributed-rate-limiter/go/internal/middleware"
	"github.com/manan/distributed-rate-limiter/go/internal/redis"
	"github.com/manan/distributed-rate-limiter/go/internal/routes"
)

const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		log.Fatalf("[Server] %v", err)
	}
}

func run() error {
	cfg := config.Load()

	// A missing Lua script is a deployment error — fail loudly rather than
	// silently serving unlimited traffic.
	client, err := redis.New(cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("[Server] Closing Redis: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := client.Connect(ctx); err != nil {
		log.Printf("[Server] Redis unavailable at startup — running in fail-open mode: %v", err)
	}
	client.StartHealthProbe(ctx)

	buf := buffer.New(cfg, client)
	limiter, err := middleware.NewLimiter(cfg, client, buf)
	if err != nil {
		return err
	}
	buf.StartFlushLoop(ctx)

	mux := http.NewServeMux()
	routes.Register(mux, routes.Deps{
		Limiter: limiter,
		Client:  client,
		Buffer:  buf,
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("[Server] Listening on port %d", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		log.Println("[Server] Shutting down gracefully...")
	}

	// Order matters: stop accepting requests first, so no new work is handed
	// to the buffer while we drain its pending write-backs.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("[Server] Forced shutdown: %v", err)
	}
	buf.StopFlushLoop()

	return nil
}
