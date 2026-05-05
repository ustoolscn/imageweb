package generator

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"image-web/backend/internal/model"
)

type Client struct {
	HTTPClient *http.Client
	TempDir    string
}

type GenerateResult struct {
	RequestHeaders  string
	RequestJSON     string
	ResponseHeaders string
	ResponseJSON    string
	Files           []string
}

type imagesResponse struct {
	Created int64 `json:"created"`
	Data    []struct {
		B64JSON       string `json:"b64_json"`
		URL           string `json:"url"`
		RevisedPrompt string `json:"revised_prompt"`
	} `json:"data"`
	Usage any `json:"usage"`
}

func New(tempDir string) *Client {
	return &Client{
		HTTPClient: &http.Client{Timeout: 10 * time.Minute},
		TempDir:    tempDir,
	}
}

func (c *Client) FetchModels(ctx context.Context, baseURL, apiKey string) (json.RawMessage, error) {
	endpoint, err := joinURL(baseURL, "/v1/models")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("模型接口请求失败：HTTP %d %s", resp.StatusCode, string(data))
	}
	return data, nil
}

func (c *Client) Generate(ctx context.Context, task *model.Task, finalPrompt string) (GenerateResult, error) {
	if hasReferenceImages(task) {
		return c.generateWithReferences(ctx, task, finalPrompt)
	}
	return c.generateTextOnly(ctx, task, finalPrompt)
}

func (c *Client) generateTextOnly(ctx context.Context, task *model.Task, finalPrompt string) (GenerateResult, error) {
	endpoint, err := joinURL(task.BaseURL, "/v1/images/generations")
	if err != nil {
		return GenerateResult{}, err
	}
	payload, err := buildPayload(task, finalPrompt)
	if err != nil {
		return GenerateResult{}, err
	}
	requestData, err := json.Marshal(payload)
	if err != nil {
		return GenerateResult{}, err
	}
	return c.doGenerateJSON(ctx, task, endpoint, requestData)
}

func (c *Client) doGenerateJSON(ctx context.Context, task *model.Task, endpoint string, requestData []byte) (GenerateResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestData))
	if err != nil {
		return GenerateResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+task.APIKey)
	req.Header.Set("Content-Type", "application/json")
	requestHeaders := requestInfoJSON(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return GenerateResult{RequestHeaders: requestHeaders, RequestJSON: string(requestData)}, err
	}
	defer resp.Body.Close()
	responseHeaders := responseInfoJSON(resp)
	responseData, err := io.ReadAll(resp.Body)
	if err != nil {
		return GenerateResult{RequestHeaders: requestHeaders, RequestJSON: string(requestData), ResponseHeaders: responseHeaders}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return GenerateResult{RequestHeaders: requestHeaders, RequestJSON: string(requestData), ResponseHeaders: responseHeaders, ResponseJSON: string(responseData)}, fmt.Errorf("生成接口请求失败：HTTP %d %s", resp.StatusCode, string(responseData))
	}

	files, err := c.materializeResponseImages(ctx, task, responseData)
	if err != nil {
		return GenerateResult{RequestHeaders: requestHeaders, RequestJSON: string(requestData), ResponseHeaders: responseHeaders, ResponseJSON: string(responseData), Files: files}, err
	}
	return GenerateResult{RequestHeaders: requestHeaders, RequestJSON: string(requestData), ResponseHeaders: responseHeaders, ResponseJSON: string(responseData), Files: files}, nil
}

func buildPayload(task *model.Task, finalPrompt string) (map[string]any, error) {
	size, err := normalizeGPTImage2Size(task.Size)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"model":         "gpt-image-2",
		"prompt":        finalPrompt,
		"n":             task.N,
		"size":          size,
		"quality":       normalizeGPTImage2Quality(task.Quality),
		"output_format": normalizeOutputFormat(task.OutputFormat),
		"background":    normalizeGPTImage2Background(task.Background),
		"moderation":    normalizeModeration(task.Moderation),
	}
	if payload["output_format"] == "webp" || payload["output_format"] == "jpeg" {
		payload["output_compression"] = clamp(task.OutputCompression, 0, 100)
	}
	return payload, nil
}

func (c *Client) generateWithReferences(ctx context.Context, task *model.Task, finalPrompt string) (GenerateResult, error) {
	endpoint, err := joinURL(task.BaseURL, "/v1/images/edits")
	if err != nil {
		return GenerateResult{}, err
	}
	if err := os.MkdirAll(c.TempDir, 0o755); err != nil {
		return GenerateResult{}, err
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	requestSummary, err := buildEditRequestSummary(task, finalPrompt)
	if err != nil {
		return GenerateResult{}, err
	}
	for key, value := range requestSummary {
		if value != "" {
			_ = writer.WriteField(key, value)
		}
	}

	cleanup := []string{}
	defer func() {
		for _, path := range cleanup {
			_ = os.Remove(path)
		}
	}()
	requestImages := []multipartFileSource{}
	for index, image := range referenceInputs(task) {
		path, err := c.downloadReferenceImage(ctx, task.ID, index, image.URL)
		if err != nil {
			_ = writer.Close()
			return GenerateResult{RequestJSON: buildMultipartRequestSource(requestSummary, requestImages)}, err
		}
		cleanup = append(cleanup, path)
		requestImages = append(requestImages, multipartFileSource{
			Field:    "image",
			Filename: filepath.Base(path),
			Source:   image.URL,
		})
		if err := addMultipartFile(writer, "image", path); err != nil {
			_ = writer.Close()
			return GenerateResult{RequestJSON: buildMultipartRequestSource(requestSummary, requestImages)}, err
		}
	}
	if err := writer.Close(); err != nil {
		return GenerateResult{RequestJSON: buildMultipartRequestSource(requestSummary, requestImages)}, err
	}

	requestData := buildMultipartRequestSource(requestSummary, requestImages)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return GenerateResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+task.APIKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	requestHeaders := requestInfoJSON(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return GenerateResult{RequestHeaders: requestHeaders, RequestJSON: string(requestData)}, err
	}
	defer resp.Body.Close()
	responseHeaders := responseInfoJSON(resp)
	responseData, err := io.ReadAll(resp.Body)
	if err != nil {
		return GenerateResult{RequestHeaders: requestHeaders, RequestJSON: string(requestData), ResponseHeaders: responseHeaders}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return GenerateResult{RequestHeaders: requestHeaders, RequestJSON: string(requestData), ResponseHeaders: responseHeaders, ResponseJSON: string(responseData)}, fmt.Errorf("编辑接口请求失败：HTTP %d %s", resp.StatusCode, string(responseData))
	}
	files, err := c.materializeResponseImages(ctx, task, responseData)
	if err != nil {
		return GenerateResult{RequestHeaders: requestHeaders, RequestJSON: string(requestData), ResponseHeaders: responseHeaders, ResponseJSON: string(responseData), Files: files}, err
	}
	return GenerateResult{RequestHeaders: requestHeaders, RequestJSON: string(requestData), ResponseHeaders: responseHeaders, ResponseJSON: string(responseData), Files: files}, nil
}

func (c *Client) materializeResponseImages(ctx context.Context, task *model.Task, responseData []byte) ([]string, error) {
	var parsed imagesResponse
	if err := json.Unmarshal(responseData, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Data) == 0 {
		return nil, fmt.Errorf("生成接口没有返回图片")
	}
	files := []string{}
	for index, image := range parsed.Data {
		file, err := c.materializeImage(ctx, task.ID, index, task.OutputFormat, image.B64JSON, image.URL)
		if err != nil {
			return files, err
		}
		files = append(files, file)
	}
	return files, nil
}

func (c *Client) materializeImage(ctx context.Context, taskID string, index int, outputFormat, b64, imageURL string) (string, error) {
	if err := os.MkdirAll(c.TempDir, 0o755); err != nil {
		return "", err
	}
	ext := normalizeExt(outputFormat)
	path := filepath.Join(c.TempDir, fmt.Sprintf("%s-%d.%s", taskID, index, ext))
	if b64 != "" {
		data, err := base64.StdEncoding.DecodeString(stripDataURLPrefix(b64))
		if err != nil {
			return "", err
		}
		return path, os.WriteFile(path, data, 0o644)
	}
	if imageURL == "" {
		return "", fmt.Errorf("图片结果既没有 b64_json 也没有 url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("下载生成图片失败：HTTP %d", resp.StatusCode)
	}
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	_, err = io.Copy(file, resp.Body)
	return path, err
}

func normalizeGPTImage2Size(size string) (string, error) {
	if size == "" || size == "auto" {
		return "auto", nil
	}
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return "", fmt.Errorf("尺寸格式错误：%s", size)
	}
	width, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", fmt.Errorf("尺寸格式错误：%s", size)
	}
	height, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", fmt.Errorf("尺寸格式错误：%s", size)
	}
	width = roundToMultiple(width, 16)
	height = roundToMultiple(height, 16)
	width, height = fitMaxEdge(width, height, 3840)
	width, height = fitAspectRatio(width, height, 3)
	width, height = fitPixelRange(width, height, 655360, 8294400)
	if err := validateGPTImage2Size(width, height); err != nil {
		return "", err
	}
	return fmt.Sprintf("%dx%d", width, height), nil
}

func validateGPTImage2Size(width, height int) error {
	longSide := max(width, height)
	shortSide := min(width, height)
	pixels := width * height
	if width%16 != 0 || height%16 != 0 {
		return fmt.Errorf("尺寸宽高必须都是 16 的倍数")
	}
	if longSide > 3840 {
		return fmt.Errorf("尺寸最大边不能超过 3840")
	}
	if float64(longSide)/float64(shortSide) > 3 {
		return fmt.Errorf("尺寸长短边比例不能超过 3:1")
	}
	if pixels < 655360 || pixels > 8294400 {
		return fmt.Errorf("尺寸总像素必须在 655360 到 8294400 之间")
	}
	return nil
}

func fitMaxEdge(width, height, maxEdge int) (int, int) {
	longSide := max(width, height)
	if longSide <= maxEdge {
		return width, height
	}
	scale := float64(maxEdge) / float64(longSide)
	return roundToMultiple(int(math.Floor(float64(width)*scale)), 16), roundToMultiple(int(math.Floor(float64(height)*scale)), 16)
}

func fitAspectRatio(width, height int, maxRatio float64) (int, int) {
	if width >= height && float64(width)/float64(height) > maxRatio {
		return roundToMultiple(int(float64(height)*maxRatio), 16), height
	}
	if height > width && float64(height)/float64(width) > maxRatio {
		return width, roundToMultiple(int(float64(width)*maxRatio), 16)
	}
	return width, height
}

func fitPixelRange(width, height, minPixels, maxPixels int) (int, int) {
	pixels := width * height
	if pixels > maxPixels {
		scale := math.Sqrt(float64(maxPixels) / float64(pixels))
		return roundToMultiple(int(math.Floor(float64(width)*scale)), 16), roundToMultiple(int(math.Floor(float64(height)*scale)), 16)
	}
	if pixels < minPixels {
		scale := math.Sqrt(float64(minPixels) / float64(pixels))
		return roundToMultiple(int(math.Ceil(float64(width)*scale)), 16), roundToMultiple(int(math.Ceil(float64(height)*scale)), 16)
	}
	return width, height
}

func roundToMultiple(value, multiple int) int {
	return max(multiple, int(math.Round(float64(value)/float64(multiple)))*multiple)
}

func normalizeGPTImage2Quality(value string) string {
	switch value {
	case "low", "medium", "high", "auto":
		return value
	default:
		return "auto"
	}
}

func normalizeOutputFormat(value string) string {
	switch value {
	case "jpeg", "webp", "png":
		return value
	default:
		return "png"
	}
}

func normalizeGPTImage2Background(value string) string {
	switch value {
	case "opaque", "auto":
		return value
	default:
		return "auto"
	}
}

func normalizeModeration(value string) string {
	switch value {
	case "low", "auto":
		return value
	default:
		return "low"
	}
}

func clamp(value, minValue, maxValue int) int {
	return min(max(value, minValue), maxValue)
}

func hasReferenceImages(task *model.Task) bool {
	return len(task.ReferenceImages) > 0
}

func referenceInputs(task *model.Task) []model.UploadedImage {
	return append([]model.UploadedImage{}, task.ReferenceImages...)
}

func buildEditRequestSummary(task *model.Task, finalPrompt string) (map[string]string, error) {
	size, err := normalizeGPTImage2Size(task.Size)
	if err != nil {
		return nil, err
	}
	format := normalizeOutputFormat(task.OutputFormat)
	payload := map[string]string{
		"model":         "gpt-image-2",
		"prompt":        finalPrompt,
		"n":             fmt.Sprint(task.N),
		"size":          size,
		"quality":       normalizeGPTImage2Quality(task.Quality),
		"output_format": format,
		"background":    normalizeGPTImage2Background(task.Background),
		"moderation":    normalizeModeration(task.Moderation),
	}
	if format == "webp" || format == "jpeg" {
		payload["output_compression"] = fmt.Sprint(clamp(task.OutputCompression, 0, 100))
	}
	return payload, nil
}

func (c *Client) downloadReferenceImage(ctx context.Context, taskID string, index int, imageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("下载参考图失败：HTTP %d", resp.StatusCode)
	}
	ext := extFromContentType(resp.Header.Get("Content-Type"))
	path := filepath.Join(c.TempDir, fmt.Sprintf("%s-ref-%d.%s", taskID, index, ext))
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	_, err = io.Copy(file, resp.Body)
	return path, err
}

func addMultipartFile(writer *multipart.Writer, fieldName, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	part, err := writer.CreateFormFile(fieldName, filepath.Base(path))
	if err != nil {
		return err
	}
	_, err = io.Copy(part, file)
	return err
}

type multipartFileSource struct {
	Field    string
	Filename string
	Source   string
}

func buildMultipartRequestSource(fields map[string]string, images []multipartFileSource) string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := []string{"Content-Type: multipart/form-data", ""}
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s: %s", key, fields[key]))
	}
	for _, image := range images {
		lines = append(lines, fmt.Sprintf("%s: @%s", image.Field, image.Filename))
		lines = append(lines, fmt.Sprintf("%s.source: %s", image.Field, image.Source))
	}
	return strings.Join(lines, "\n")
}

func requestInfoJSON(req *http.Request) string {
	data, err := json.MarshalIndent(map[string]any{
		"method":  req.Method,
		"url":     req.URL.String(),
		"headers": cleanHeaders(req.Header),
	}, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}

func responseInfoJSON(resp *http.Response) string {
	data, err := json.MarshalIndent(map[string]any{
		"status":      resp.Status,
		"status_code": resp.StatusCode,
		"headers":     cleanHeaders(resp.Header),
	}, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}

func cleanHeaders(headers http.Header) http.Header {
	clean := http.Header{}
	for key, values := range headers {
		if strings.EqualFold(key, "Authorization") {
			clean[key] = []string{"Bearer ***"}
			continue
		}
		clean[key] = values
	}
	return clean
}

func extFromContentType(contentType string) string {
	if strings.Contains(contentType, "jpeg") || strings.Contains(contentType, "jpg") {
		return "jpg"
	}
	if strings.Contains(contentType, "webp") {
		return "webp"
	}
	return "png"
}

func joinURL(baseURL, path string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	return parsed.String(), nil
}

func normalizeExt(format string) string {
	switch strings.ToLower(format) {
	case "jpeg", "jpg":
		return "jpg"
	case "webp":
		return "webp"
	default:
		return "png"
	}
}

func stripDataURLPrefix(value string) string {
	if idx := strings.Index(value, ","); idx >= 0 && strings.Contains(value[:idx], "base64") {
		return value[idx+1:]
	}
	return value
}
