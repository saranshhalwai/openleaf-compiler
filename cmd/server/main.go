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

	"openleaf-compiler/internal/handlers"
	"openleaf-compiler/internal/storage"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	env := os.Getenv("ENV")
	if env == "" {
		env = "development"
	}

	bucket := os.Getenv("GCS_BUCKET")
	authSecret := os.Getenv("COMPILER_AUTH_SECRET")
	useLocalStorage := os.Getenv("USE_LOCAL_STORAGE") == "true"

	ctx := context.Background()

	var storageProvider storage.Provider
	if useLocalStorage {
		log.Println("[Storage] Using Local Filesystem Provider (/tmp/openleaf-storage)")
		storageProvider = storage.NewLocalProvider("/tmp/openleaf-storage")
	} else {
		log.Println("[Storage] Initializing Google Cloud Storage Provider")
		gcs, err := storage.NewGCSProvider(ctx)
		if err != nil {
			log.Printf("[Storage Warning] GCS client initialization failed: %v", err)
			log.Println("[Storage] Falling back to Local Provider for development/testing")
			storageProvider = storage.NewLocalProvider("/tmp/openleaf-storage")
		} else {
			defer gcs.Close()
			storageProvider = gcs
		}
	}

	compileHandler := handlers.NewCompileHandler(storageProvider, bucket, authSecret)

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(180 * time.Second))

	// Health Check for Cloud Run probes
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"openleaf-compiler"}`))
	})

	// Main Compilation Endpoint
	r.Post("/compile", compileHandler.HandleCompile)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("Starting openleaf-compiler on port :%s (ENV=%s)", port, env)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down compiler service...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Compiler service exited gracefully")
}
