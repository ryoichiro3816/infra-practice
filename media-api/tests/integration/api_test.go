package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"media-api/internal/models"

	"github.com/gorilla/mux"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Test server setup
type TestServer struct {
	router *mux.Router
	db     *gorm.DB
}

func NewTestServer() *TestServer {
	// Setup test database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("Failed to connect to test database")
	}
	db.AutoMigrate(&models.Media{})

	// Setup router
	r := mux.NewRouter()

	// CORS middleware
	r.Use(corsMiddleware)

	// Health check (no auth)
	r.HandleFunc("/healthz", healthCheck).Methods("GET")

	// API routes (with auth)
	api := r.PathPrefix("/api/v1").Subrouter()
	api.Use(authMiddleware)
	api.HandleFunc("/media", uploadMedia(db)).Methods("POST")
	api.HandleFunc("/media", getMediaList(db)).Methods("GET")
	api.HandleFunc("/media/{id}", getMedia(db)).Methods("GET")
	api.HandleFunc("/media/{id}/file", getMediaFile(db)).Methods("GET")

	return &TestServer{
		router: r,
		db:     db,
	}
}

// Helper functions (copied from main package for integration tests)
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

		if header.Size > 10*1024*1024 {
			http.Error(w, `{"error":"File too large"}`, 400)
			return
		}

		uploadDir := getEnv("UPLOAD_DIR", "./storage/uploads")
		os.MkdirAll(uploadDir, 0755)

		filename := header.Filename
		filePath := uploadDir + "/" + filename
		out, err := os.Create(filePath)
		if err != nil {
			http.Error(w, `{"error":"Save failed"}`, 500)
			return
		}
		defer out.Close()
		io.Copy(out, file)

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

// Integration Tests

func TestAPIWorkflow(t *testing.T) {
	server := NewTestServer()
	
	// Set test environment
	os.Setenv("API_TOKEN", "test-token")
	os.Setenv("UPLOAD_DIR", "./test_integration_uploads")
	defer func() {
		os.RemoveAll("./test_integration_uploads")
		os.Unsetenv("API_TOKEN")
		os.Unsetenv("UPLOAD_DIR")
	}()

	// Test 1: Health check
	req := httptest.NewRequest("GET", "/healthz", nil)
	recorder := httptest.NewRecorder()
	server.router.ServeHTTP(recorder, req)
	
	if recorder.Code != http.StatusOK {
		t.Errorf("Health check failed: expected %d, got %d", http.StatusOK, recorder.Code)
	}

	// Test 2: Upload media
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fileWriter, err := writer.CreateFormFile("file", "test.jpg")
	if err != nil {
		t.Fatal(err)
	}
	fileWriter.Write([]byte("test image data"))
	writer.Close()

	req = httptest.NewRequest("POST", "/api/v1/media", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer test-token")
	recorder = httptest.NewRecorder()
	server.router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Errorf("Upload failed: expected %d, got %d", http.StatusCreated, recorder.Code)
	}

	var uploadResponse map[string]interface{}
	json.Unmarshal(recorder.Body.Bytes(), &uploadResponse)
	
	// Test 3: Get media list
	req = httptest.NewRequest("GET", "/api/v1/media", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	recorder = httptest.NewRecorder()
	server.router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("Get media list failed: expected %d, got %d", http.StatusOK, recorder.Code)
	}

	var listResponse map[string]interface{}
	json.Unmarshal(recorder.Body.Bytes(), &listResponse)
	mediaList := listResponse["media"].([]interface{})
	
	if len(mediaList) != 1 {
		t.Errorf("Expected 1 media item, got %d", len(mediaList))
	}

	// Test 4: Get specific media
	req = httptest.NewRequest("GET", "/api/v1/media/1", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	recorder = httptest.NewRecorder()
	server.router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("Get media failed: expected %d, got %d", http.StatusOK, recorder.Code)
	}

	// Test 5: Get media file
	req = httptest.NewRequest("GET", "/api/v1/media/1/file", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	recorder = httptest.NewRecorder()
	server.router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("Get media file failed: expected %d, got %d", http.StatusOK, recorder.Code)
	}

	fileContent, _ := io.ReadAll(recorder.Body)
	expectedContent := []byte("test image data")
	if !bytes.Equal(fileContent, expectedContent) {
		t.Error("File content doesn't match uploaded content")
	}
}

func TestAuthenticationFlow(t *testing.T) {
	server := NewTestServer()
	
	os.Setenv("API_TOKEN", "secret-token")
	defer os.Unsetenv("API_TOKEN")

	tests := []struct {
		name           string
		endpoint       string
		method         string
		authHeader     string
		expectedStatus int
	}{
		{
			name:           "No auth on health endpoint",
			endpoint:       "/healthz",
			method:         "GET",
			authHeader:     "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Valid auth on protected endpoint",
			endpoint:       "/api/v1/media",
			method:         "GET",
			authHeader:     "Bearer secret-token",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Invalid auth on protected endpoint",
			endpoint:       "/api/v1/media",
			method:         "GET",
			authHeader:     "Bearer wrong-token",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "No auth on protected endpoint",
			endpoint:       "/api/v1/media",
			method:         "GET",
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.endpoint, nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			recorder := httptest.NewRecorder()
			server.router.ServeHTTP(recorder, req)

			if recorder.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, recorder.Code)
			}
		})
	}
}

func TestCORSHandling(t *testing.T) {
	server := NewTestServer()

	// Test OPTIONS preflight request
	req := httptest.NewRequest("OPTIONS", "/api/v1/media", nil)
	req.Header.Set("Origin", "https://example.com")
	
	recorder := httptest.NewRecorder()
	server.router.ServeHTTP(recorder, req)

	if recorder.Code != 204 {
		t.Errorf("Expected status 204 for OPTIONS, got %d", recorder.Code)
	}

	// Check CORS headers
	expectedHeaders := map[string]string{
		"Access-Control-Allow-Origin":  "*",
		"Access-Control-Allow-Methods": "GET,POST,OPTIONS",
		"Access-Control-Allow-Headers": "Content-Type,Authorization",
	}

	for header, expected := range expectedHeaders {
		if recorder.Header().Get(header) != expected {
			t.Errorf("Expected header %s to be %s, got %s", header, expected, recorder.Header().Get(header))
		}
	}
}

func TestErrorHandling(t *testing.T) {
	server := NewTestServer()
	
	os.Setenv("API_TOKEN", "test-token")
	defer os.Unsetenv("API_TOKEN")

	// Test 404 for non-existent media
	req := httptest.NewRequest("GET", "/api/v1/media/999", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	recorder := httptest.NewRecorder()
	server.router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", recorder.Code)
	}

	// Test 404 for non-existent media file
	req = httptest.NewRequest("GET", "/api/v1/media/999/file", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	recorder = httptest.NewRecorder()
	server.router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", recorder.Code)
	}
}