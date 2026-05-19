package model

import "time"

type TaskStatus string
type TaskType string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskSucceeded TaskStatus = "succeeded"
	TaskFailed    TaskStatus = "failed"

	TaskTypeImageGeneration TaskType = "image_generation"
	TaskTypeVideoGeneration TaskType = "video_generation"
)

type UploadedImage struct {
	URL                string  `json:"url"`
	ThumbnailURL       string  `json:"thumbnail_url,omitempty"`
	Filename           string  `json:"filename,omitempty"`
	NodeID             string  `json:"node_id,omitempty"`
	ReferenceLabel     string  `json:"reference_label,omitempty"`
	MaskReferenceLabel string  `json:"mask_reference_label,omitempty"`
	OriginalSize       int64   `json:"original_size,omitempty"`
	CompressedSize     int64   `json:"compressed_size,omitempty"`
	CompressionRatio   float64 `json:"compression_ratio,omitempty"`
	MaskURL            string  `json:"mask_url,omitempty"`
}

type MediaAsset struct {
	Type           string `json:"type,omitempty"`
	URL            string `json:"url"`
	ThumbnailURL   string `json:"thumbnail_url,omitempty"`
	Filename       string `json:"filename,omitempty"`
	NodeID         string `json:"node_id,omitempty"`
	ReferenceLabel string `json:"reference_label,omitempty"`
	Duration       int    `json:"duration,omitempty"`
	ClipStart      int    `json:"clip_start,omitempty"`
	ClipEnd        int    `json:"clip_end,omitempty"`
	Width          int    `json:"width,omitempty"`
	Height         int    `json:"height,omitempty"`
}

type Task struct {
	ID                  string          `json:"id"`
	APIKey              string          `json:"-"`
	BaseURL             string          `json:"baseurl"`
	TaskType            TaskType        `json:"task_type"`
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
	InputFidelity       string          `json:"input_fidelity"`
	N                   int             `json:"n"`
	Stream              bool            `json:"stream"`
	Style               string          `json:"style,omitempty"`
	ResponseFormat      string          `json:"response_format,omitempty"`
	ReferenceImagesJSON string          `json:"-"`
	ReferenceVideosJSON string          `json:"-"`
	ReferenceAudiosJSON string          `json:"-"`
	Favorite            bool            `json:"favorite"`
	RequestHeaders      string          `json:"request_headers"`
	RequestJSON         string          `json:"request_json"`
	ResponseHeaders     string          `json:"response_headers"`
	ResponseJSON        string          `json:"response_json"`
	ResultImagesJSON    string          `json:"-"`
	ResultVideosJSON    string          `json:"-"`
	UpstreamTaskID      string          `json:"upstream_task_id,omitempty"`
	UpstreamStatus      string          `json:"upstream_status,omitempty"`
	UpstreamProgress    int             `json:"upstream_progress"`
	NextPollAt          *time.Time      `json:"next_poll_at,omitempty"`
	PollCount           int             `json:"poll_count"`
	VideoRatio          string          `json:"video_ratio,omitempty"`
	VideoWidth          int             `json:"video_width,omitempty"`
	VideoHeight         int             `json:"video_height,omitempty"`
	VideoDuration       int             `json:"video_duration,omitempty"`
	GenerateAudio       bool            `json:"generate_audio"`
	Watermark           bool            `json:"watermark"`
	ErrorMessage        string          `json:"error_message"`
	ElapsedMS           int64           `json:"elapsed_ms"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
	StartedAt           *time.Time      `json:"started_at,omitempty"`
	CompletedAt         *time.Time      `json:"completed_at,omitempty"`
	QueuePosition       int             `json:"queue_position"`
	SharedToPlaza       bool            `json:"shared_to_plaza"`
	ReferenceImages     []UploadedImage `json:"reference_images"`
	ReferenceVideos     []MediaAsset    `json:"reference_videos"`
	ReferenceAudios     []MediaAsset    `json:"reference_audios"`
	ResultImages        []UploadedImage `json:"result_images"`
	ResultVideos        []MediaAsset    `json:"result_videos"`
}

type TaskUpdate struct {
	ID               string          `json:"id"`
	Status           TaskStatus      `json:"status"`
	ResultImagesJSON string          `json:"-"`
	ResultVideosJSON string          `json:"-"`
	ErrorMessage     string          `json:"error_message"`
	ElapsedMS        int64           `json:"elapsed_ms"`
	UpdatedAt        time.Time       `json:"updated_at"`
	StartedAt        *time.Time      `json:"started_at,omitempty"`
	CompletedAt      *time.Time      `json:"completed_at,omitempty"`
	QueuePosition    int             `json:"queue_position"`
	UpstreamStatus   string          `json:"upstream_status,omitempty"`
	UpstreamProgress int             `json:"upstream_progress"`
	ResultImages     []UploadedImage `json:"result_images"`
	ResultVideos     []MediaAsset    `json:"result_videos"`
}

type PlazaItem struct {
	ID                  string          `json:"id"`
	TaskID              string          `json:"task_id"`
	TaskType            TaskType        `json:"task_type"`
	Prompt              string          `json:"prompt"`
	Model               string          `json:"model"`
	Size                string          `json:"size"`
	Quality             string          `json:"quality"`
	OutputFormat        string          `json:"output_format"`
	OutputCompression   int             `json:"output_compression"`
	Background          string          `json:"background"`
	Moderation          string          `json:"moderation"`
	InputFidelity       string          `json:"input_fidelity"`
	N                   int             `json:"n"`
	Stream              bool            `json:"stream"`
	Style               string          `json:"style,omitempty"`
	ResponseFormat      string          `json:"response_format,omitempty"`
	ReferenceImagesJSON string          `json:"-"`
	ReferenceVideosJSON string          `json:"-"`
	ReferenceAudiosJSON string          `json:"-"`
	ResultImagesJSON    string          `json:"-"`
	ResultVideosJSON    string          `json:"-"`
	VideoRatio          string          `json:"video_ratio,omitempty"`
	VideoWidth          int             `json:"video_width,omitempty"`
	VideoHeight         int             `json:"video_height,omitempty"`
	VideoDuration       int             `json:"video_duration,omitempty"`
	GenerateAudio       bool            `json:"generate_audio"`
	Watermark           bool            `json:"watermark"`
	LikeCount           int             `json:"like_count"`
	Liked               bool            `json:"liked"`
	CreatedAt           time.Time       `json:"created_at"`
	ReferenceImages     []UploadedImage `json:"reference_images"`
	ReferenceVideos     []MediaAsset    `json:"reference_videos"`
	ReferenceAudios     []MediaAsset    `json:"reference_audios"`
	ResultImages        []UploadedImage `json:"result_images"`
	ResultVideos        []MediaAsset    `json:"result_videos"`
}

type CreateTaskRequest struct {
	APIKey            string          `json:"apikey"`
	BaseURL           string          `json:"baseurl"`
	NodeKind          string          `json:"node_kind"`
	TaskType          TaskType        `json:"task_type"`
	Prompt            string          `json:"prompt"`
	Model             string          `json:"model"`
	Size              string          `json:"size"`
	Quality           string          `json:"quality"`
	OutputFormat      string          `json:"output_format"`
	OutputCompression int             `json:"output_compression"`
	Background        string          `json:"background"`
	Moderation        string          `json:"moderation"`
	InputFidelity     string          `json:"input_fidelity"`
	N                 int             `json:"n"`
	Stream            bool            `json:"stream"`
	Style             string          `json:"style"`
	ResponseFormat    string          `json:"response_format"`
	ReferenceImages   []UploadedImage `json:"reference_images"`
	ReferenceVideos   []MediaAsset    `json:"reference_videos"`
	ReferenceAudios   []MediaAsset    `json:"reference_audios"`
	VideoRatio        string          `json:"video_ratio"`
	VideoWidth        int             `json:"video_width"`
	VideoHeight       int             `json:"video_height"`
	VideoDuration     int             `json:"video_duration"`
	GenerateAudio     bool            `json:"generate_audio"`
	Watermark         bool            `json:"watermark"`
}

type ModelsRequest struct {
	APIKey  string `json:"apikey"`
	BaseURL string `json:"baseurl"`
}

type LLMRequest struct {
	APIKey          string          `json:"apikey"`
	BaseURL         string          `json:"baseurl"`
	Model           string          `json:"model"`
	ReasoningEffort string          `json:"reasoning_effort"`
	Prompt          string          `json:"prompt"`
	ReferenceImages []UploadedImage `json:"reference_images"`
	ReferenceVideos []MediaAsset    `json:"reference_videos"`
	ReferenceAudios []MediaAsset    `json:"reference_audios"`
}

type ShareTaskRequest struct {
	APIKey  string `json:"apikey"`
	BaseURL string `json:"baseurl"`
}

type LikePlazaRequest struct {
	ClientID string `json:"client_id"`
	Liked    bool   `json:"liked"`
}

type SiteConfig struct {
	BaseURLWhitelistEnabled bool                `json:"baseurl_whitelist_enabled"`
	BaseURLWhitelist        []BaseURLAllowEntry `json:"baseurl_whitelist"`
	AdminContactImage       string              `json:"admin_contact_image"`
	SiteTitle               string              `json:"site_title"`
	SiteIcon                string              `json:"site_icon"`
	WorkerConcurrency       int                 `json:"worker_concurrency"`
}

type BaseURLAllowEntry struct {
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
	Icon    string `json:"icon,omitempty"`
	Allow2K *bool  `json:"allow_2k,omitempty"`
	Allow4K *bool  `json:"allow_4k,omitempty"`
}

type SiteBrandResponse struct {
	Title   string `json:"title"`
	Icon    string `json:"icon"`
	Allow2K bool   `json:"allow_2k"`
	Allow4K bool   `json:"allow_4k"`
}
