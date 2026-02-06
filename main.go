package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"neomovies-tg-bot/api"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", handler.IndexHandler)
	mux.HandleFunc("/api/webhook", handler.Handler)
	mux.HandleFunc("/api/player", handler.PlayerHandler)
	mux.HandleFunc("/api/library", handler.LibraryHandler)
	mux.HandleFunc("/api/library/item", handler.LibraryItemHandler)

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("Server listening on :%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	log.Println("Server stopped")
}
