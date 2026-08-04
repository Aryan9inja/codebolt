package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Aryan9inja/codebolt/internal/embeddings"
	"github.com/Aryan9inja/codebolt/internal/github"
	"github.com/Aryan9inja/codebolt/internal/llm"
	"github.com/Aryan9inja/codebolt/internal/processor"
	"github.com/Aryan9inja/codebolt/internal/webhook"
	"github.com/Aryan9inja/gotaskq/taskq"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
)

const (
	defaultPort     = "8080"
	prReviewQueue   = "pr-review"
	numQueueWorkers = 3
)

func main() {
	godotenv.Load()

	secret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	if secret == "" {
		log.Fatal("GITHUB_WEBHOOK_SECRET not set")
	}

	appID := os.Getenv("GITHUB_APP_ID")
	if appID == "" {
		log.Fatal("GITHUB_APP_ID not set")
	}

	privateKeyPath := os.Getenv("GITHUB_PRIVATE_KEY_PATH")
	if privateKeyPath == "" {
		log.Fatal("GITHUB_PRIVATE_KEY_PATH not set")
	}

	openRouterKey := os.Getenv("OPENROUTER_API_KEY")
	if openRouterKey == "" {
		log.Println("OPENROUTER_API_KEY not set")
	}

	// Embeddings are optional — CodeBolt runs without pgvector,
	// cross-PR context is simply skipped when these are unset.
	var embProvider embeddings.EmbeddingProvider
	var embStore *embeddings.Store

	geminiAPIKey := os.Getenv("GEMINI_API_KEY")
	databaseURL := os.Getenv("DATABASE_URL")

	if geminiAPIKey != "" && databaseURL != "" {
		embProvider = embeddings.NewGeminiEmbeddingProvider(geminiAPIKey)
		store, err := embeddings.NewStore(context.Background(), databaseURL)
		if err != nil {
			log.Fatalf("Failed to init embeddings store: %v", err)
		}
		embStore = store
		log.Println("Embeddings enabled (Gemini + pgvector)")
	} else {
		log.Println("Embeddings disabled — set GEMINI_API_KEY and DATABASE_URL to enable cross-PR pattern detection")
	}

	if embStore != nil {
		defer embStore.Close()
	}

	ghClient := github.NewClient(appID, privateKeyPath)
	llmProvider := llm.NewOpenRouterProvider(openRouterKey)
	llmPipeline := llm.NewPipeline(llmProvider, llm.DefaultModel, embProvider, embStore)
	proc := processor.NewProcessor(ghClient, llmPipeline)

	queue, err := taskq.New(taskq.Options{
		NumWorkers:       numQueueWorkers,
		DefaultQueueName: prReviewQueue,
	})
	if err != nil {
		log.Fatalf("Failed to init task queue: %v", err)
	}

	if err := queue.RegisterFunc(prReviewQueue, proc.HandlePRReview); err != nil {
		log.Fatalf("Failed to register handler: %v", err)
	}

	if err := queue.StartWorkers(); err != nil {
		log.Fatalf("Failed to start workers: %v", err)
	}
	defer func() {
		if err := queue.StopWorkers(); err != nil {
			log.Printf("Failed to stop workers: %v", err)
		}
	}()

	wh := webhook.NewHandler(secret, queue)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Post("/webhook", wh.ServeHTTP)

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	srv := &http.Server{Addr: ":" + port, Handler: r}

	go func() {
		log.Printf("Codebolt listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down...")
	if err := srv.Shutdown(context.Background()); err != nil {
		log.Fatalf("Server shutdown error: %v", err)
	}
}
