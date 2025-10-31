package main

import (
	"log"
	"net/http"
	"os"

	"media-api/internal/handlers"
	"media-api/internal/middleware"
	"media-api/internal/models"

	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env
	godotenv.Load()

	// Init DB
	dbPath := getEnv("DB_PATH")
	if dbPath == "none"{
		log.Fatalf("DB error (path: %s): %v", dbPath)
	}

	db, err := models.InitDB(dbPath)
	if err != nil {
		log.Fatalf("DB error (path: %s): %v", dbPath, err)
	}

	// Init handlers
	mediaHandler := handlers.NewMediaHandler(db)

	r := mux.NewRouter()

	// CORS middleware
	r.Use(middleware.CORSMiddleware())

	// Health check (no auth)
	r.HandleFunc("/healthz", mediaHandler.HealthCheck).Methods("GET")

	// API routes (with auth)
	api := r.PathPrefix("/api/v1").Subrouter()
	api.Use(middleware.AuthMiddleware())
	api.HandleFunc("/media", mediaHandler.UploadMedia).Methods("POST")
	api.HandleFunc("/media", mediaHandler.GetMediaList).Methods("GET")
	api.HandleFunc("/media/{id}", mediaHandler.GetMedia).Methods("GET")
	api.HandleFunc("/media/{id}/file", mediaHandler.GetMediaFile).Methods("GET")

	port := getEnv("PORT")
	if port == "none"{
		log.Fatalf("port error (path: %s): %v", port)
	}
	log.Printf("Starting server on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}

func getEnv(key string) string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return "none"
	}
	return value
}