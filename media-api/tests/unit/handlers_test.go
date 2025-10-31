package unit

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"media-api/internal/handlers"
	"media-api/internal/middleware"
	"media-api/internal/models"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type MediaHandlerTestSuite struct {
	suite.Suite
	db      *gorm.DB
	handler *handlers.MediaHandler
}

func (suite *MediaHandlerTestSuite) SetupTest() {
	// Setup in-memory database for each test
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	suite.Require().NoError(err)
	
	// Auto migrate
	err = db.AutoMigrate(&models.Media{})
	suite.Require().NoError(err)
	
	suite.db = db
	suite.handler = handlers.NewMediaHandler(db)
}

func (suite *MediaHandlerTestSuite) TearDownTest() {
	// Clean up test files
	os.RemoveAll("./test_uploads")
}

func (suite *MediaHandlerTestSuite) TestHealthCheck() {
	req, _ := http.NewRequest("GET", "/healthz", nil)
	recorder := httptest.NewRecorder()

	suite.handler.HealthCheck(recorder, req)

	assert.Equal(suite.T(), http.StatusOK, recorder.Code)
	
	var response map[string]string
	err := json.Unmarshal(recorder.Body.Bytes(), &response)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "ok", response["status"])
}

func (suite *MediaHandlerTestSuite) TestUploadMedia() {
	os.Setenv("UPLOAD_DIR", "./test_uploads")
	defer os.Unsetenv("UPLOAD_DIR")

	// Create multipart form data
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	
	fileWriter, err := writer.CreateFormFile("file", "test.jpg")
	assert.NoError(suite.T(), err)
	fileWriter.Write([]byte("fake image data"))
	writer.Close()

	req, _ := http.NewRequest("POST", "/api/v1/media", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()

	suite.handler.UploadMedia(recorder, req)

	assert.Equal(suite.T(), http.StatusCreated, recorder.Code)
	
	var response map[string]interface{}
	err = json.Unmarshal(recorder.Body.Bytes(), &response)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "Uploaded", response["message"])
}

func (suite *MediaHandlerTestSuite) TestGetMediaList() {
	// Create test data
	testMedia := models.Media{
		Filename: "test.jpg",
		FilePath: "/test/path/test.jpg",
		FileSize: 1024,
		MimeType: "image/jpeg",
	}
	suite.db.Create(&testMedia)

	req, _ := http.NewRequest("GET", "/api/v1/media", nil)
	recorder := httptest.NewRecorder()

	suite.handler.GetMediaList(recorder, req)

	assert.Equal(suite.T(), http.StatusOK, recorder.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(recorder.Body.Bytes(), &response)
	assert.NoError(suite.T(), err)
	
	mediaList := response["media"].([]interface{})
	assert.Len(suite.T(), mediaList, 1)
}

func (suite *MediaHandlerTestSuite) TestGetMedia() {
	// Create test data
	testMedia := models.Media{
		Filename: "test.jpg",
		FilePath: "/test/path/test.jpg", 
		FileSize: 1024,
		MimeType: "image/jpeg",
	}
	suite.db.Create(&testMedia)

	// Use mux router for path variables
	router := mux.NewRouter()
	router.HandleFunc("/media/{id}", suite.handler.GetMedia).Methods("GET")

	req, _ := http.NewRequest("GET", "/media/1", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	assert.Equal(suite.T(), http.StatusOK, recorder.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(recorder.Body.Bytes(), &response)
	assert.NoError(suite.T(), err)
	
	media := response["media"].(map[string]interface{})
	assert.Equal(suite.T(), "test.jpg", media["filename"])
}

func (suite *MediaHandlerTestSuite) TestGetMediaNotFound() {
	router := mux.NewRouter()
	router.HandleFunc("/media/{id}", suite.handler.GetMedia).Methods("GET")

	req, _ := http.NewRequest("GET", "/media/999", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	assert.Equal(suite.T(), http.StatusNotFound, recorder.Code)
}

// Test middleware separately
func TestMiddleware(t *testing.T) {
	t.Run("CORS Middleware", func(t *testing.T) {
		testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		handler := middleware.CORSMiddleware()(testHandler)

		// Test OPTIONS request
		req, _ := http.NewRequest("OPTIONS", "/test", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		assert.Equal(t, 204, recorder.Code)
		assert.Equal(t, "*", recorder.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, "GET,POST,OPTIONS", recorder.Header().Get("Access-Control-Allow-Methods"))
	})

	t.Run("Auth Middleware", func(t *testing.T) {
		os.Setenv("API_TOKEN", "test-token")
		defer os.Unsetenv("API_TOKEN")

		testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		handler := middleware.AuthMiddleware()(testHandler)

		tests := []struct {
			name           string
			authHeader     string
			expectedStatus int
		}{
			{"Valid token", "Bearer test-token", http.StatusOK},
			{"Invalid token", "Bearer wrong-token", http.StatusUnauthorized},
			{"No token", "", http.StatusUnauthorized},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req, _ := http.NewRequest("GET", "/test", nil)
				if tt.authHeader != "" {
					req.Header.Set("Authorization", tt.authHeader)
				}

				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, req)

				assert.Equal(t, tt.expectedStatus, recorder.Code)
			})
		}
	})
}

func TestMediaHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(MediaHandlerTestSuite))
}