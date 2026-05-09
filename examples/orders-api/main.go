package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	port := flag.String("port", "", "port to listen on (default: PORT env var or 8080)")
	flag.Parse()

	p := *port
	if p == "" {
		p = os.Getenv("PORT")
	}
	if p == "" {
		p = "8080"
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	srv := &http.Server{
		Addr:    ":" + p,
		Handler: loggingMiddleware(NewRouter()),
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("orders-api listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-quit
	slog.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "err", err)
		os.Exit(1)
	}
}

// NewRouter builds and returns the orders API router.
// Exported so it can be used in tests via httptest.NewServer.
func NewRouter() http.Handler {
	mux := http.NewServeMux()
	store := newStore()

	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /orders", handleListOrders(store))
	mux.HandleFunc("POST /orders", handleCreateOrder(store))
	mux.HandleFunc("GET /orders/{id}", handleGetOrder(store))
	mux.HandleFunc("PATCH /orders/{id}", handleUpdateOrder(store))
	mux.HandleFunc("GET /orders/{id}/broken", handleBrokenOrder(store))

	return mux
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}
