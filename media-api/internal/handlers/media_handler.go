package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"os"

	"media-api/internal/models"

	"github.com/gorilla/mux"
	"gorm.io/gorm"
)

type MediaHandler struct {
	DB *gorm.DB
}

func NewMediaHandler(db *gorm.DB) *MediaHandler {
	return &MediaHandler{DB: db}
}

func (h *MediaHandler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *MediaHandler) UploadMedia(w http.ResponseWriter, r *http.Request) {
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

	if err := h.DB.Create(&media).Error; err != nil {
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

func (h *MediaHandler) GetMediaList(w http.ResponseWriter, r *http.Request) {
	var mediaList []models.Media
	h.DB.Order("created_at DESC").Find(&mediaList)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"media": mediaList})
}

func (h *MediaHandler) GetMedia(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var media models.Media
	if err := h.DB.First(&media, id).Error; err != nil {
		http.Error(w, `{"error":"Not found"}`, 404)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"media": media})
}

func (h *MediaHandler) GetMediaFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var media models.Media
	if err := h.DB.First(&media, id).Error; err != nil {
		http.Error(w, `{"error":"Not found"}`, 404)
		return
	}

	http.ServeFile(w, r, media.FilePath)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}