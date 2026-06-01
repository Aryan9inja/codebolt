package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Aryan9inja/codebolt/internal/webhook"
	"github.com/Aryan9inja/gotaskq/taskq"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()
	secret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	if secret == "" {
		log.Fatal("GITHUB_WEBHOOK_SECRET not set")
	}

	queue, err := taskq.New(taskq.Options{
		NumWorkers:       3,
		DefaultQueueName: "pr-review",
	})
	if err != nil {
		log.Fatalf("Failed to init task queue: %v", err)
	}

	// register PR review job handler
	if err := queue.RegisterFunc("pr-review", func(ctx context.Context, job *taskq.Job) error {
		var payload webhook.PullRequestEvent
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return err
		}
		log.Printf("Processing PR review job for #%d in %s/%s", payload.PullRequest.Number, payload.Repository.Owner.Login, payload.Repository.Name)
		// TODO: add actual review logic here
		return nil
	}); err != nil {
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
		port = "8080"
	}

	// graceful shutdown
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
