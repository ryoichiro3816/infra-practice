package unit

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"media-api/internal/models"

	"github.com/gorilla/mux"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB() *gorm.DB {
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("Failed to connect to test database")
	}
	
	// Auto migrate tables
	testDB.AutoMigrate(&models.Media{})
	
	return testDB
}

// Test helper functions
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

func uploadMedia(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
}

func getMediaList(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var mediaList []models.Media
		db.Order("created_at DESC").Find(&mediaList)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"media": mediaList})
	}
}

func getMedia(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
}

func getMediaFile(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]

		var media models.Media
		if err := db.First(&media, id).Error; err != nil {
			http.Error(w, `{"error":"Not found"}`, 404)
			return
		}

		http.ServeFile(w, r, media.FilePath)
	}
}

// Unit Tests

func TestHealthCheck(t *testing.T) {
	req, err := http.NewRequest("GET", "/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	handler := http.HandlerFunc(healthCheck)

	handler.ServeHTTP(recorder, req)

	if status := recorder.Code; status != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, status)
	}

	expected := `{"status":"ok"}`
	if strings.TrimSpace(recorder.Body.String()) != expected {
		t.Errorf("Expected body %s, got %s", expected, recorder.Body.String())
	}
}

func TestCorsMiddleware(t *testing.T) {
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := corsMiddleware(testHandler)

	// Test OPTIONS request
	req, _ := http.NewRequest("OPTIONS", "/test", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != 204 {
		t.Errorf("Expected status 204 for OPTIONS, got %d", recorder.Code)
	}

	// Check CORS headers
	if recorder.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("Expected CORS origin header to be *")
	}

	if recorder.Header().Get("Access-Control-Allow-Methods") != "GET,POST,OPTIONS" {
		t.Error("Expected CORS methods header")
	}
}

func TestAuthMiddleware(t *testing.T) {
	// Set test environment variable
	os.Setenv("API_TOKEN", "test-token")
	defer os.Unsetenv("API_TOKEN")

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := authMiddleware(testHandler)

	tests := []struct {
		name           string
		authHeader     string
		expectedStatus int
	}{
		{
			name:           "Valid token",
			authHeader:     "Bearer test-token",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Invalid token",
			authHeader:     "Bearer wrong-token",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "No token",
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Wrong format",
			authHeader:     "test-token",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, recorder.Code)
			}
		})
	}
}

func TestUploadMedia(t *testing.T) {
	db := setupTestDB()

	// Set test upload directory
	os.Setenv("UPLOAD_DIR", "./test_uploads")
	defer func() {
		os.RemoveAll("./test_uploads")
		os.Unsetenv("UPLOAD_DIR")
	}()

	// Create multipart form data
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	
	fileWriter, err := writer.CreateFormFile("file", "test.jpg")
	if err != nil {
		t.Fatal(err)
	}
	fileWriter.Write([]byte("fake image data"))
	writer.Close()

	req, err := http.NewRequest("POST", "/api/v1/media", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	recorder := httptest.NewRecorder()
	handler := uploadMedia(db)

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, recorder.Code)
	}

	var response map[string]interface{}
	err = json.Unmarshal(recorder.Body.Bytes(), &response)
	if err != nil {
		t.Fatal("Failed to parse JSON response")
	}

	if response["message"] != "Uploaded" {
		t.Error("Expected message 'Uploaded'")
	}
}

func TestUploadMediaNoFile(t *testing.T) {
	db := setupTestDB()

	req, err := http.NewRequest("POST", "/api/v1/media", nil)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	handler := uploadMedia(db)

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestUploadMediaFileTooBig(t *testing.T) {
	db := setupTestDB()

	// Create a file larger than 10MB
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	
	fileWriter, err := writer.CreateFormFile("file", "big.jpg")
	if err != nil {
		t.Fatal(err)
	}
	
	// Write more than 10MB of data
	bigData := make([]byte, 11*1024*1024) // 11MB
	fileWriter.Write(bigData)
	writer.Close()

	req, err := http.NewRequest("POST", "/api/v1/media", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	recorder := httptest.NewRecorder()
	handler := uploadMedia(db)

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d for large file, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestGetMediaList(t *testing.T) {
	db := setupTestDB()

	// Create test data
	testMedia := models.Media{
		Filename: "test.jpg",
		FilePath: "/test/path/test.jpg",
		FileSize: 1024,
		MimeType: "image/jpeg",
	}
	db.Create(&testMedia)

	req, err := http.NewRequest("GET", "/api/v1/media", nil)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	handler := getMediaList(db)

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var response map[string]interface{}
	err = json.Unmarshal(recorder.Body.Bytes(), &response)
	if err != nil {
		t.Fatal("Failed to parse JSON response")
	}

	mediaList, ok := response["media"].([]interface{})
	if !ok || len(mediaList) == 0 {
		t.Error("Expected media list to contain at least one item")
	}
}

func TestGetMedia(t *testing.T) {
	db := setupTestDB()

	// Create test data
	testMedia := models.Media{
		Filename: "test.jpg",
		FilePath: "/test/path/test.jpg",
		FileSize: 1024,
		MimeType: "image/jpeg",
	}
	db.Create(&testMedia)

	// Use mux router for path variables
	router := mux.NewRouter()
	router.HandleFunc("/media/{id}", getMedia(db)).Methods("GET")

	req, err := http.NewRequest("GET", "/media/1", nil)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var response map[string]interface{}
	err = json.Unmarshal(recorder.Body.Bytes(), &response)
	if err != nil {
		t.Fatal("Failed to parse JSON response")
	}

	media, ok := response["media"].(map[string]interface{})
	if !ok {
		t.Error("Expected media object in response")
	}

	if media["filename"] != "test.jpg" {
		t.Errorf("Expected filename 'test.jpg', got %v", media["filename"])
	}
}

func TestGetMediaNotFound(t *testing.T) {
	db := setupTestDB()

	router := mux.NewRouter()
	router.HandleFunc("/media/{id}", getMedia(db)).Methods("GET")

	req, err := http.NewRequest("GET", "/media/999", nil)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
}

func TestGetMediaFile(t *testing.T) {
	db := setupTestDB()

	// Create test file
	testDir := "../testdata"
	os.MkdirAll(testDir, 0755)
	defer os.RemoveAll(testDir)

	testFilePath := testDir + "/test.jpg"
	testContent := []byte("fake image content")
	err := os.WriteFile(testFilePath, testContent, 0644)
	if err != nil {
		t.Fatal("Failed to create test file")
	}

	// Create test data
	testMedia := models.Media{
		Filename: "test.jpg",
		FilePath: testFilePath,
		FileSize: int64(len(testContent)),
		MimeType: "image/jpeg",
	}
	db.Create(&testMedia)

	router := mux.NewRouter()
	router.HandleFunc("/media/{id}/file", getMediaFile(db)).Methods("GET")

	req, err := http.NewRequest("GET", "/media/1/file", nil)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	// Check file content
	responseBody, _ := io.ReadAll(recorder.Body)
	if !bytes.Equal(responseBody, testContent) {
		t.Error("File content doesn't match")
	}
}