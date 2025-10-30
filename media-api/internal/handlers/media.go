package handlers

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"media-api/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func UploadMedia(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get the file from form data
		file, header, err := c.Request.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
			return
		}
		defer file.Close()

		// Check file size
		maxSize := int64(10 * 1024 * 1024) // 10MB default
		if maxSizeStr := os.Getenv("MAX_UPLOAD_SIZE"); maxSizeStr != "" {
			// You could parse maxSizeStr here if needed
		}

		if header.Size > maxSize {
			c.JSON(http.StatusBadRequest, gin.H{"error": "File too large"})
			return
		}

		// Check file type (only images)
		if !isImageFile(header.Header.Get("Content-Type")) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Only image files are allowed"})
			return
		}

		// Create upload directory if it doesn't exist
		uploadDir := os.Getenv("UPLOAD_DIR")
		if uploadDir == "" {
			uploadDir = "./storage/uploads"
		}

		if err := os.MkdirAll(uploadDir, 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create upload directory"})
			return
		}

		// Generate unique filename
		filename := generateUniqueFilename(header.Filename)
		filePath := filepath.Join(uploadDir, filename)

		// Save file to disk
		if err := saveUploadedFile(file, filePath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
			return
		}

		// Save metadata to database
		media := models.Media{
			Filename: header.Filename,
			FilePath: filePath,
			FileSize: header.Size,
			MimeType: header.Header.Get("Content-Type"),
		}

		if err := db.Create(&media).Error; err != nil {
			// Clean up uploaded file if database save fails
			os.Remove(filePath)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save media metadata"})
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"message": "File uploaded successfully",
			"media":   media,
		})
	}
}

func isImageFile(contentType string) bool {
	allowedTypes := []string{
		"image/jpeg",
		"image/jpg", 
		"image/png",
		"image/gif",
		"image/webp",
		"image/heic",
		"image/heif",
	}

	for _, allowedType := range allowedTypes {
		if contentType == allowedType {
			return true
		}
	}
	return false
}

func generateUniqueFilename(originalFilename string) string {
	ext := filepath.Ext(originalFilename)
	name := strings.TrimSuffix(originalFilename, ext)
	timestamp := time.Now().Unix()
	return fmt.Sprintf("%s_%d%s", name, timestamp, ext)
}

func saveUploadedFile(file multipart.File, filepath string) error {
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, file)
	return err
}

func GetMediaList(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var mediaList []models.Media
		
		// Get pagination parameters
		page := c.DefaultQuery("page", "1")
		limit := c.DefaultQuery("limit", "20")
		
		// Convert to integers
		var pageNum, limitNum int
		fmt.Sscanf(page, "%d", &pageNum)
		fmt.Sscanf(limit, "%d", &limitNum)
		
		if pageNum < 1 {
			pageNum = 1
		}
		if limitNum < 1 || limitNum > 100 {
			limitNum = 20
		}
		
		offset := (pageNum - 1) * limitNum
		
		// Get total count
		var total int64
		db.Model(&models.Media{}).Count(&total)
		
		// Get media with pagination, ordered by created_at desc
		if err := db.Order("created_at DESC").Offset(offset).Limit(limitNum).Find(&mediaList).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch media list"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"media": mediaList,
			"pagination": gin.H{
				"page":  pageNum,
				"limit": limitNum,
				"total": total,
			},
		})
	}
}

func GetMedia(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		
		var media models.Media
		if err := db.First(&media, id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusNotFound, gin.H{"error": "Media not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch media"})
			}
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"media": media,
		})
	}
}

func GetMediaFile(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		
		var media models.Media
		if err := db.First(&media, id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusNotFound, gin.H{"error": "Media not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch media"})
			}
			return
		}

		// Check if file exists
		if _, err := os.Stat(media.FilePath); os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
			return
		}

		// Set appropriate headers
		c.Header("Content-Type", media.MimeType)
		c.Header("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", media.Filename))
		
		// Serve the file
		c.File(media.FilePath)
	}
}