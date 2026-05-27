package main

import (
	"log"
	"net/http"
	"os"

	"github.com/Aryan9inja/codebolt/internal/webhook"
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

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	wh := webhook.NewHandler(secret)
	r.Post("/webhook", wh.ServeHTTP)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("CodeBolt listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
