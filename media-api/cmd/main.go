package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"

	"media-api/internal/models"

	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

var db *gorm.DB

func main() {
	// Load .env
	godotenv.Load()

	// Init DB
	var err error
	db, err = models.InitDB(getEnv("DB_PATH", "./storage/database.db"))
	if err != nil {
		log.Fatal("DB error:", err)
	}

	r := mux.NewRouter()

	// CORS middleware
	r.Use(corsMiddleware)

	// Health check (no auth)
	r.HandleFunc("/healthz", healthCheck).Methods("GET")

	// API routes (with auth)
	api := r.PathPrefix("/api/v1").Subrouter()
	api.Use(authMiddleware)
	api.HandleFunc("/media", uploadMedia).Methods("POST")
	api.HandleFunc("/media", getMediaList).Methods("GET")
	api.HandleFunc("/media/{id}", getMedia).Methods("GET")
	api.HandleFunc("/media/{id}/file", getMediaFile).Methods("GET")

	port := getEnv("PORT", "8080")
	log.Printf("Starting server on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		expected := "Bearer " + getEnv("API_TOKEN", "test-token")
		if token != expected {
			http.Error(w, `{"error":"Unauthorized"}`, 401)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func uploadMedia(w http.ResponseWriter, r *http.Request) {
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"No file"}`, 400)
		return
	}
	defer file.Close()

	// Check file size
	if header.Size > 10*1024*1024 {
		http.Error(w, `{"error":"File too large"}`, 400)
		return
	}

	// Create upload dir
	uploadDir := getEnv("UPLOAD_DIR", "./storage/uploads")
	os.MkdirAll(uploadDir, 0755)

	// Save file
	filename := header.Filename
	filePath := uploadDir + "/" + filename
	out, err := os.Create(filePath)
	if err != nil {
		http.Error(w, `{"error":"Save failed"}`, 500)
		return
	}
	defer out.Close()
	io.Copy(out, file)

	// Save to DB
	media := models.Media{
		Filename: filename,
		FilePath: filePath,
		FileSize: header.Size,
		MimeType: header.Header.Get("Content-Type"),
	}

	if err := db.Create(&media).Error; err != nil {
		os.Remove(filePath)
		http.Error(w, `{"error":"DB error"}`, 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Uploaded",
		"media":   media,
	})
}

func getMediaList(w http.ResponseWriter, r *http.Request) {
	var mediaList []models.Media
	db.Order("created_at DESC").Find(&mediaList)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"media": mediaList})
}

func getMedia(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var media models.Media
	if err := db.First(&media, id).Error; err != nil {
		http.Error(w, `{"error":"Not found"}`, 404)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"media": media})
}

func getMediaFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var media models.Media
	if err := db.First(&media, id).Error; err != nil {
		http.Error(w, `{"error":"Not found"}`, 404)
		return
	}

	http.ServeFile(w, r, media.FilePath)
}