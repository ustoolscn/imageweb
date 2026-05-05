package model

import "time"

type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskSucceeded TaskStatus = "succeeded"
	TaskFailed    TaskStatus = "failed"
)

type UploadedImage struct {
	URL              string  `json:"url"`
	Filename         string  `json:"filename,omitempty"`
	OriginalSize     int64   `json:"original_size,omitempty"`
	CompressedSize   int64   `json:"compressed_size,omitempty"`
	CompressionRatio float64 `json:"compression_ratio,omitempty"`
}

type Task struct {
	ID                  string          `json:"id"`
	APIKey              string          `json:"-"`
	BaseURL             string          `json:"baseurl"`
	Status              TaskStatus      `json:"status"`
	Prompt              string          `json:"prompt"`
	FinalPrompt         string          `json:"final_prompt"`
	Model               string          `json:"model"`
	Size                string          `json:"size"`
	Quality             string          `json:"quality"`
	OutputFormat        string          `json:"output_format"`
	OutputCompression   int             `json:"output_compression"`
	Background          string          `json:"background"`
	Moderation          string          `json:"moderation"`
	N                   int             `json:"n"`
	Style               string          `json:"style,omitempty"`
	ResponseFormat      string          `json:"response_format,omitempty"`
	ReferenceImagesJSON string          `json:"-"`
	Favorite            bool            `json:"favorite"`
	RequestHeaders      string          `json:"request_headers"`
	RequestJSON         string          `json:"request_json"`
	ResponseHeaders     string          `json:"response_headers"`
	ResponseJSON        string          `json:"response_json"`
	ResultImagesJSON    string          `json:"-"`
	ErrorMessage        string          `json:"error_message"`
	ElapsedMS           int64           `json:"elapsed_ms"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
	StartedAt           *time.Time      `json:"started_at,omitempty"`
	CompletedAt         *time.Time      `json:"completed_at,omitempty"`
	ReferenceImages     []UploadedImage `json:"reference_images"`
	ResultImages        []UploadedImage `json:"result_images"`
}

type CreateTaskRequest struct {
	APIKey            string          `json:"apikey"`
	BaseURL           string          `json:"baseurl"`
	Prompt            string          `json:"prompt"`
	Model             string          `json:"model"`
	Size              string          `json:"size"`
	Quality           string          `json:"quality"`
	OutputFormat      string          `json:"output_format"`
	OutputCompression int             `json:"output_compression"`
	Background        string          `json:"background"`
	Moderation        string          `json:"moderation"`
	N                 int             `json:"n"`
	Style             string          `json:"style"`
	ResponseFormat    string          `json:"response_format"`
	ReferenceImages   []UploadedImage `json:"reference_images"`
}

type ModelsRequest struct {
	APIKey  string `json:"apikey"`
	BaseURL string `json:"baseurl"`
}
