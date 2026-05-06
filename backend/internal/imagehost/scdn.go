package imagehost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"image-web/backend/internal/model"
)

type Client struct {
	UploadURL  string
	HTTPClient *http.Client
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

func New(uploadURL string) *Client {
	return &Client{
		UploadURL:  uploadURL,
		HTTPClient: &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *Client) UploadReader(ctx context.Context, filename string, reader io.Reader) (model.UploadedImage, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return model.UploadedImage{}, err
	}
	if _, err := io.Copy(part, reader); err != nil {
		return model.UploadedImage{}, err
	}
	if err := writer.Close(); err != nil {
		return model.UploadedImage{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.UploadURL, &body)
	if err != nil {
		return model.UploadedImage{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer cooper")

	resp, err := c.HTTPClient.Do(req)
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

	var parsed uploadResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return model.UploadedImage{}, err
	}
	if !parsed.Success || parsed.URL == "" {
		return model.UploadedImage{}, fmt.Errorf("图片上传失败：%s", string(data))
	}
	return model.UploadedImage{
		URL:              parsed.URL,
		Filename:         parsed.Data.Filename,
		OriginalSize:     parsed.Data.OriginalSize,
		CompressedSize:   parsed.Data.CompressedSize,
		CompressionRatio: parsed.Data.CompressionRatio,
	}, nil
}

func (c *Client) UploadFile(ctx context.Context, path string) (model.UploadedImage, error) {
	file, err := os.Open(path)
	if err != nil {
		return model.UploadedImage{}, err
	}
	defer file.Close()
	return c.UploadReader(ctx, filepath.Base(path), file)
}
