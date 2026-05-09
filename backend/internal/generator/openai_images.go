package generator

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
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
	RequestHeaders            string
	RequestJSON               string
	ResponseHeaders           string
	ResponseJSON              string
	Files                     []string
	RequestStartedAt          time.Time
	RequestWroteAt            time.Time
	ResponseFirstByteAt       time.Time
	ResponseHeadersReceivedAt time.Time
	ResponseBodyReadAt        time.Time
	UpstreamStatus            string
	UpstreamStatusCode        int
	UpstreamServerDate        string
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
		HTTPClient: &http.Client{Timeout: 20 * time.Minute},
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
	result := GenerateResult{RequestHeaders: requestHeaders, RequestJSON: string(requestData)}
	req = attachRequestTrace(req, &result)

	result.RequestStartedAt = time.Now()
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	result.ResponseHeadersReceivedAt = time.Now()
	result.UpstreamStatus = resp.Status
	result.UpstreamStatusCode = resp.StatusCode
	result.UpstreamServerDate = resp.Header.Get("Date")
	responseHeaders := responseInfoJSON(resp)
	result.ResponseHeaders = responseHeaders
	responseData, err := readImageResponse(resp.Body)
	result.ResponseBodyReadAt = time.Now()
	responseJSON := compactImageResponseForStorage(responseData)
	result.ResponseJSON = responseJSON
	if err != nil {
		return result, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("生成接口请求失败：HTTP %d %s", resp.StatusCode, string(responseData))
	}
	if upstreamErr := extractUpstreamError(responseData); upstreamErr != "" {
		return result, fmt.Errorf("上游图片接口返回错误：%s", upstreamErr)
	}

	files, err := c.materializeResponseImages(ctx, task, responseData)
	if err != nil {
		result.Files = files
		return result, err
	}
	result.Files = files
	return result, nil
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

	cleanup := []string{}
	defer func() {
		for _, path := range cleanup {
			_ = os.Remove(path)
		}
	}()
	requestImages := []multipartFileSource{}
	maskedReferences := []maskedReferencePromptInfo{}
	for index, image := range referenceInputs(task) {
		path, err := c.downloadReferenceAsset(ctx, task.ID, "ref", index, image.URL)
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
		if image.MaskURL != "" {
			maskPath, err := c.downloadReferenceAsset(ctx, task.ID, "mask", index, image.MaskURL)
			if err != nil {
				_ = writer.Close()
				return GenerateResult{RequestJSON: buildMultipartRequestSource(requestSummary, requestImages)}, err
			}
			cleanup = append(cleanup, maskPath)
			mergedPath, err := c.createMaskedReferencePreview(task.ID, index, path, maskPath)
			if err != nil {
				_ = writer.Close()
				return GenerateResult{RequestJSON: buildMultipartRequestSource(requestSummary, requestImages)}, err
			}
			cleanup = append(cleanup, mergedPath)
			maskedReferences = append(maskedReferences, maskedReferencePromptInfo{
				OriginalFilename: filepath.Base(path),
				MarkedFilename:   filepath.Base(mergedPath),
			})
			requestImages = append(requestImages, multipartFileSource{
				Field:    "image",
				Filename: filepath.Base(mergedPath),
				Source:   fmt.Sprintf("generated from %s with mask %s", image.URL, image.MaskURL),
			})
			if err := addMultipartFile(writer, "image", mergedPath); err != nil {
				_ = writer.Close()
				return GenerateResult{RequestJSON: buildMultipartRequestSource(requestSummary, requestImages)}, err
			}
		}
	}
	requestSummary["prompt"] = buildMaskedEditPrompt(finalPrompt, maskedReferences)
	for key, value := range requestSummary {
		if value != "" {
			_ = writer.WriteField(key, value)
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
	result := GenerateResult{RequestHeaders: requestHeaders, RequestJSON: string(requestData)}
	req = attachRequestTrace(req, &result)

	result.RequestStartedAt = time.Now()
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	result.ResponseHeadersReceivedAt = time.Now()
	result.UpstreamStatus = resp.Status
	result.UpstreamStatusCode = resp.StatusCode
	result.UpstreamServerDate = resp.Header.Get("Date")
	responseHeaders := responseInfoJSON(resp)
	result.ResponseHeaders = responseHeaders
	responseData, err := readImageResponse(resp.Body)
	result.ResponseBodyReadAt = time.Now()
	responseJSON := compactImageResponseForStorage(responseData)
	result.ResponseJSON = responseJSON
	if err != nil {
		return result, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("编辑接口请求失败：HTTP %d %s", resp.StatusCode, string(responseData))
	}
	if upstreamErr := extractUpstreamError(responseData); upstreamErr != "" {
		return result, fmt.Errorf("上游图片接口返回错误：%s", upstreamErr)
	}
	files, err := c.materializeResponseImages(ctx, task, responseData)
	if err != nil {
		result.Files = files
		return result, err
	}
	result.Files = files
	return result, nil
}

func extractUpstreamError(data []byte) string {
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return ""
	}
	errObj, ok := value["error"]
	if !ok {
		return ""
	}
	// 字符串错误
	if msg, ok := errObj.(string); ok {
		return msg
	}
	// 结构化错误 {"message": "...", "code": "..."}
	if obj, ok := errObj.(map[string]any); ok {
		if msg, ok := obj["message"].(string); ok && msg != "" {
			return msg
		}
	}
	return ""
}

func readImageResponse(reader io.Reader) ([]byte, error) {
	return io.ReadAll(reader)
}

func attachRequestTrace(req *http.Request, result *GenerateResult) *http.Request {
	trace := &httptrace.ClientTrace{
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			result.RequestWroteAt = time.Now()
		},
		GotFirstResponseByte: func() {
			result.ResponseFirstByteAt = time.Now()
		},
	}
	return req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
}

func compactImageResponseForStorage(data []byte) string {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return string(data)
	}
	redactBase64Fields(value)
	compact, err := json.Marshal(value)
	if err != nil {
		return string(data)
	}
	return string(compact)
}

func redactBase64Fields(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "b64_json" {
				if text, ok := child.(string); ok && text != "" {
					typed[key] = fmt.Sprintf("[base64 image omitted, %d chars]", len(text))
				}
				continue
			}
			redactBase64Fields(child)
		}
	case []any:
		for _, child := range typed {
			redactBase64Fields(child)
		}
	}
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

type maskedReferencePromptInfo struct {
	OriginalFilename string
	MarkedFilename   string
}

func buildMaskedEditPrompt(userPrompt string, references []maskedReferencePromptInfo) string {
	prompt := strings.TrimSpace(userPrompt)
	if len(references) == 0 {
		return prompt
	}
	lines := []string{"参考图说明："}
	for _, reference := range references {
		lines = append(lines, fmt.Sprintf("- %s 是需要编辑的原图；%s 是由 %s 和蒙板合并生成的标记图。", reference.OriginalFilename, reference.MarkedFilename, reference.OriginalFilename))
	}
	lines = append(lines,
		"标记图中的白色涂抹区域表示允许修改的区域；其余区域表示需要尽量保持不变。",
		"请根据上面列出的文件名匹配每组原图和标记图，只修改标记图中白色涂抹区域对应的位置，其他区域保持原图一致。",
		"",
		"用户真实输入：",
		prompt,
	)
	return strings.TrimSpace(strings.Join(lines, "\n"))
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
	if task.Stream {
		payload["stream"] = "true"
	}
	if format == "webp" || format == "jpeg" {
		payload["output_compression"] = fmt.Sprint(clamp(task.OutputCompression, 0, 100))
	}
	return payload, nil
}

func (c *Client) downloadReferenceAsset(ctx context.Context, taskID, kind string, index int, imageURL string) (string, error) {
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
	path := filepath.Join(c.TempDir, fmt.Sprintf("%s-%s-%d.%s", taskID, kind, index, ext))
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	_, err = io.Copy(file, resp.Body)
	return path, err
}

func (c *Client) createMaskedReferencePreview(taskID string, index int, imagePath, maskPath string) (string, error) {
	baseFile, err := os.Open(imagePath)
	if err != nil {
		return "", err
	}
	defer baseFile.Close()
	baseImage, _, err := image.Decode(baseFile)
	if err != nil {
		return "", err
	}
	maskFile, err := os.Open(maskPath)
	if err != nil {
		return "", err
	}
	defer maskFile.Close()
	maskImage, _, err := image.Decode(maskFile)
	if err != nil {
		return "", err
	}
	bounds := baseImage.Bounds()
	output := image.NewNRGBA(bounds)
	draw.Draw(output, bounds, baseImage, bounds.Min, draw.Src)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := maskImage.At(x, y).RGBA()
			if alpha < 0xffff {
				base := output.NRGBAAt(x, y)
				output.SetNRGBA(x, y, color.NRGBA{
					R: uint8((uint16(base.R)*71 + 255*184) / 255),
					G: uint8((uint16(base.G)*71 + 255*184) / 255),
					B: uint8((uint16(base.B)*71 + 255*184) / 255),
					A: base.A,
				})
			}
		}
	}
	path := filepath.Join(c.TempDir, fmt.Sprintf("%s-mask-preview-%d.png", taskID, index))
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return path, png.Encode(file, output)
}

func addMultipartFile(writer *multipart.Writer, fieldName, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, filepath.Base(path)))
	header.Set("Content-Type", contentTypeFromPath(path))
	part, err := writer.CreatePart(header)
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

func contentTypeFromPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return "image/png"
	}
}

func joinURL(baseURL, path string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("invalid baseurl")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	parsed.RawQuery = ""
	parsed.Fragment = ""
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
