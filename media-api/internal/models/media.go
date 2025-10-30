package models

import (
	"time"
)

type Media struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Filename  string    `json:"filename" gorm:"not null"`
	FilePath  string    `json:"file_path" gorm:"not null"`
	FileSize  int64     `json:"file_size"`
	MimeType  string    `json:"mime_type"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Media) TableName() string {
	return "media"
}