package imagehost

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"image-web/backend/internal/config"
	"image-web/backend/internal/model"

	_ "golang.org/x/image/webp"
)

type uploader interface {
	UploadReader(ctx context.Context, filename, contentType string, reader io.Reader) (model.UploadedImage, error)
}

type Client struct {
	uploader uploader
}

type httpUploader struct {
	UploadURL       string
	AuthHeader      string
	AuthValue       string
	FieldName       string
	ResponseURLPath string
	HTTPClient      *http.Client
}

type localUploader struct {
	Dir           string
	PublicBaseURL string
}

type uploadResponse struct {
	Success bool   `json:"success"`
	URL     string `json:"url"`
	Data    struct {
		URL              string  `json:"url"`
		Filename         string  `json:"filename"`
		OriginalSize     int64   `json:"original_size"`
		CompressedSize   int64   `json:"compressed_size"`
		CompressionRatio float64 `json:"compression_ratio"`
	} `json:"data"`
}

func New(cfg config.Config) *Client {
	provider := strings.ToLower(strings.TrimSpace(cfg.ImageHostProvider))
	if provider == "" {
		provider = "http-json"
	}
	if provider == "local" {
		return &Client{uploader: &localUploader{Dir: cfg.ImageHostLocalDir, PublicBaseURL: cfg.ImageHostPublicBaseURL}}
	}
	fieldName := cfg.ImageHostFieldName
	if fieldName == "" {
		fieldName = "file"
	}
	return &Client{uploader: &httpUploader{
		UploadURL:       cfg.ImageHostUploadURL,
		AuthHeader:      cfg.ImageHostAuthHeader,
		AuthValue:       cfg.ImageHostAuthValue,
		FieldName:       fieldName,
		ResponseURLPath: cfg.ImageHostResponseURLPath,
		HTTPClient:      &http.Client{Timeout: 120 * time.Second},
	}}
}

func (c *Client) UploadReader(ctx context.Context, filename, contentType string, reader io.Reader) (model.UploadedImage, error) {
	if c == nil || c.uploader == nil {
		return model.UploadedImage{}, fmt.Errorf("图床未配置")
	}
	data, err := io.ReadAll(io.LimitReader(reader, 512<<20))
	if err != nil {
		return model.UploadedImage{}, err
	}
	uploadContentType := normalizeUploadContentType(filename, contentType, data)
	image, err := c.uploader.UploadReader(ctx, filename, uploadContentType, bytes.NewReader(data))
	if err != nil {
		return model.UploadedImage{}, err
	}
	if isVideoUpload(filename, uploadContentType) {
		frames, err := c.extractVideoFramesFromBytes(ctx, filename, data)
		if err == nil {
			image.FirstFrameURL = frames.FirstFrameURL
			image.LastFrameURL = frames.LastFrameURL
			image.ThumbnailURL = frames.ThumbnailURL
		}
		return image, nil
	}
	thumb, err := createThumbnail(data, 480)
	if err != nil {
		return image, nil
	}
	thumbnail, err := c.uploader.UploadReader(ctx, thumbnailFilename(filename), "image/jpeg", bytes.NewReader(thumb))
	if err != nil {
		return image, nil
	}
	image.ThumbnailURL = thumbnail.URL
	return image, nil
}

func (c *Client) UploadFile(ctx context.Context, path string) (model.UploadedImage, error) {
	file, err := os.Open(path)
	if err != nil {
		return model.UploadedImage{}, err
	}
	defer file.Close()
	return c.UploadReader(ctx, filepath.Base(path), contentTypeFromFilename(path), file)
}

func (c *Client) ExtractVideoFramesFromFile(ctx context.Context, path, filename string) (model.MediaAsset, error) {
	if c == nil || c.uploader == nil {
		return model.MediaAsset{}, fmt.Errorf("图床未配置")
	}
	if strings.TrimSpace(filename) == "" {
		filename = filepath.Base(path)
	}
	return c.extractAndUploadVideoFrames(ctx, path, filename)
}

func (c *Client) ExtractVideoFramesFromURL(ctx context.Context, videoURL, filename string) (model.MediaAsset, error) {
	if c == nil || c.uploader == nil {
		return model.MediaAsset{}, fmt.Errorf("图床未配置")
	}
	videoURL = strings.TrimSpace(videoURL)
	if videoURL == "" {
		return model.MediaAsset{}, fmt.Errorf("缺少视频地址")
	}
	if strings.TrimSpace(filename) == "" {
		filename = filenameFromURL(videoURL, "video.mp4")
	}
	return c.extractAndUploadVideoFrames(ctx, videoURL, filename)
}

func (c *Client) extractVideoFramesFromBytes(ctx context.Context, filename string, data []byte) (model.MediaAsset, error) {
	temp, err := os.CreateTemp("", "image-web-upload-video-*"+filepath.Ext(filename))
	if err != nil {
		return model.MediaAsset{}, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return model.MediaAsset{}, err
	}
	if err := temp.Close(); err != nil {
		return model.MediaAsset{}, err
	}
	return c.extractAndUploadVideoFrames(ctx, tempPath, filename)
}

func (c *Client) extractAndUploadVideoFrames(ctx context.Context, source, filename string) (model.MediaAsset, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return model.MediaAsset{}, fmt.Errorf("ffmpeg 未安装，无法抽取视频帧")
	}
	dir, err := os.MkdirTemp("", "image-web-video-frames-*")
	if err != nil {
		return model.MediaAsset{}, err
	}
	defer os.RemoveAll(dir)

	firstPath := filepath.Join(dir, "first-frame.jpg")
	lastPath := filepath.Join(dir, "last-frame.jpg")
	if err := extractVideoFrame(ctx, source, firstPath, false); err != nil {
		return model.MediaAsset{}, err
	}
	first, err := c.uploadFrameFile(ctx, firstPath, frameFilename(filename, "first"))
	if err != nil {
		return model.MediaAsset{}, err
	}

	last := first
	if err := extractVideoFrame(ctx, source, lastPath, true); err == nil {
		if uploaded, err := c.uploadFrameFile(ctx, lastPath, frameFilename(filename, "last")); err == nil {
			last = uploaded
		}
	}
	return model.MediaAsset{
		Type:          "video",
		ThumbnailURL:  first.URL,
		FirstFrameURL: first.URL,
		LastFrameURL:  last.URL,
	}, nil
}

func extractVideoFrame(ctx context.Context, source, output string, tail bool) error {
	commandCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-threads", "1"}
	if tail {
		args = append(args, "-sseof", "-0.1")
	}
	args = append(args,
		"-i", source,
		"-an",
		"-frames:v", "1",
		"-vf", "scale=480:480:force_original_aspect_ratio=decrease",
		"-q:v", "4",
		output,
	)
	cmd := exec.CommandContext(commandCtx, "ffmpeg", args...)
	if outputData, err := cmd.CombinedOutput(); err != nil {
		if commandCtx.Err() != nil {
			return fmt.Errorf("视频抽帧超时")
		}
		return fmt.Errorf("视频抽帧失败：%s", strings.TrimSpace(string(outputData)))
	}
	return nil
}

func (c *Client) uploadFrameFile(ctx context.Context, path, filename string) (model.UploadedImage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.UploadedImage{}, err
	}
	return c.uploader.UploadReader(ctx, filename, "image/jpeg", bytes.NewReader(data))
}

func (u *httpUploader) UploadReader(ctx context.Context, filename, contentType string, reader io.Reader) (model.UploadedImage, error) {
	if strings.TrimSpace(u.UploadURL) == "" {
		return model.UploadedImage{}, fmt.Errorf("缺少图床上传地址")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", formDataContentDisposition(defaultString(u.FieldName, "file"), filename))
	header.Set("Content-Type", defaultString(contentType, "application/octet-stream"))
	part, err := writer.CreatePart(header)
	if err != nil {
		return model.UploadedImage{}, err
	}
	if _, err := io.Copy(part, reader); err != nil {
		return model.UploadedImage{}, err
	}
	if err := writer.Close(); err != nil {
		return model.UploadedImage{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.UploadURL, &body)
	if err != nil {
		return model.UploadedImage{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if u.AuthHeader != "" && u.AuthValue != "" {
		req.Header.Set(u.AuthHeader, u.AuthValue)
	}

	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return model.UploadedImage{}, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.UploadedImage{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return model.UploadedImage{}, fmt.Errorf("图片上传失败：HTTP %d %s", resp.StatusCode, string(data))
	}
	return parseUploadResponse(data, defaultString(u.ResponseURLPath, "url"))
}

func (u *localUploader) UploadReader(ctx context.Context, filename, contentType string, reader io.Reader) (model.UploadedImage, error) {
	if strings.TrimSpace(u.Dir) == "" {
		return model.UploadedImage{}, fmt.Errorf("缺少本地图床目录")
	}
	if strings.TrimSpace(u.PublicBaseURL) == "" {
		return model.UploadedImage{}, fmt.Errorf("缺少本地图床公开访问地址")
	}
	if err := os.MkdirAll(u.Dir, 0o755); err != nil {
		return model.UploadedImage{}, err
	}
	safeName := uniqueFilename(filename)
	path := filepath.Join(u.Dir, safeName)
	file, err := os.Create(path)
	if err != nil {
		return model.UploadedImage{}, err
	}
	written, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(path)
		return model.UploadedImage{}, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return model.UploadedImage{}, closeErr
	}
	return model.UploadedImage{URL: joinURL(u.PublicBaseURL, safeName), Filename: safeName, OriginalSize: written, CompressedSize: written}, nil
}

func parseUploadResponse(data []byte, urlPath string) (model.UploadedImage, error) {
	var parsed uploadResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return model.UploadedImage{}, err
	}
	url := valueAtPath(data, urlPath)
	if url == "" {
		url = parsed.URL
	}
	if url == "" {
		url = parsed.Data.URL
	}
	if !parsed.Success && url == "" {
		return model.UploadedImage{}, fmt.Errorf("图片上传失败：%s", string(data))
	}
	if url == "" {
		return model.UploadedImage{}, fmt.Errorf("图片上传失败：图床未返回链接")
	}
	return model.UploadedImage{
		URL:              url,
		Filename:         parsed.Data.Filename,
		OriginalSize:     parsed.Data.OriginalSize,
		CompressedSize:   parsed.Data.CompressedSize,
		CompressionRatio: parsed.Data.CompressionRatio,
	}, nil
}

func valueAtPath(data []byte, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return ""
	}
	current := value
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = object[part]
	}
	if text, ok := current.(string); ok {
		return text
	}
	return ""
}

func uniqueFilename(filename string) string {
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filepath.Base(filename), ext)
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, name)
	name = strings.Trim(name, "-")
	if name == "" {
		name = "image"
	}
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d-%s%s", time.Now().UnixNano(), name, ext)
	}
	return fmt.Sprintf("%s-%s%s", hex.EncodeToString(buf), name, ext)
}

func normalizeUploadContentType(filename, contentType string, data []byte) string {
	contentType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	if contentType != "" && contentType != "application/octet-stream" {
		return contentType
	}
	if inferred := contentTypeFromFilename(filename); inferred != "" {
		return inferred
	}
	if len(data) > 0 {
		return http.DetectContentType(data)
	}
	return "application/octet-stream"
}

func contentTypeFromFilename(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".m4a":
		return "audio/mp4"
	case ".aac":
		return "audio/aac"
	case ".ogg":
		return "audio/ogg"
	case ".flac":
		return "audio/flac"
	default:
		return ""
	}
}

func isVideoUpload(filename, contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if strings.HasPrefix(contentType, "video/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".mp4", ".m4v", ".webm", ".mov":
		return true
	default:
		return false
	}
}

func formDataContentDisposition(fieldName, filename string) string {
	return fmt.Sprintf(`form-data; name="%s"; filename="%s"`, escapeMultipartQuotes(fieldName), escapeMultipartQuotes(filepath.Base(filename)))
}

func escapeMultipartQuotes(value string) string {
	return strings.NewReplacer("\\", "\\\\", `"`, `\"`, "\r", " ", "\n", " ").Replace(value)
}

func createThumbnail(data []byte, maxSize int) ([]byte, error) {
	if maxSize <= 0 {
		maxSize = 480
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	bounds := source.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid image size")
	}
	targetWidth := width
	targetHeight := height
	if width >= height {
		if width > maxSize {
			targetWidth = maxSize
			targetHeight = height * maxSize / width
		}
	} else {
		if height > maxSize {
			targetHeight = maxSize
			targetWidth = width * maxSize / height
		}
	}
	if targetWidth <= 0 {
		targetWidth = 1
	}
	if targetHeight <= 0 {
		targetHeight = 1
	}
	resized := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	draw.Draw(resized, resized.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	for y := 0; y < targetHeight; y++ {
		for x := 0; x < targetWidth; x++ {
			sourceX := bounds.Min.X + x*width/targetWidth
			sourceY := bounds.Min.Y + y*height/targetHeight
			resized.Set(x, y, source.At(sourceX, sourceY))
		}
	}
	var output bytes.Buffer
	if err := jpeg.Encode(&output, resized, &jpeg.Options{Quality: 82}); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func thumbnailFilename(filename string) string {
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filepath.Base(filename), ext)
	if name == "" {
		name = "image"
	}
	return name + "-thumb.jpg"
}

func frameFilename(filename, role string) string {
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filepath.Base(filename), ext)
	if name == "" {
		name = "video"
	}
	return name + "-" + role + "-frame.jpg"
}

func filenameFromURL(value, fallback string) string {
	parts := strings.Split(strings.TrimSpace(value), "?")
	name := filepath.Base(parts[0])
	if name == "." || name == "/" || name == "" {
		return fallback
	}
	return name
}

func joinURL(baseURL, name string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(name, "/")
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
