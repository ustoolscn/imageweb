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
	"net/textproto"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"image-web/backend/internal/model"

	_ "golang.org/x/image/webp"
)

type imagesResponse struct {
	Created int64 `json:"created"`
	Data    []struct {
		B64JSON       string `json:"b64_json"`
		URL           string `json:"url"`
		RevisedPrompt string `json:"revised_prompt"`
	} `json:"data"`
	Usage any `json:"usage"`
}

type geminiGenerateContentResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text            string            `json:"text"`
				InlineData      *geminiInlineData `json:"inlineData"`
				InlineDataSnake *geminiInlineData `json:"inline_data"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

type geminiInlineData struct {
	MimeType      string `json:"mimeType"`
	MimeTypeSnake string `json:"mime_type"`
	Data          string `json:"data"`
}

func (c *Client) Generate(ctx context.Context, task *model.Task, finalPrompt string) (GenerateResult, error) {
	fmt.Println("generator image route_enter:", "id=", task.ID, "task_type=", task.TaskType, "model=", task.Model, "has_reference_images=", hasReferenceImages(task))
	if isNanoBananaModel(task) {
		return c.generateNanoBanana(ctx, task, finalPrompt)
	}
	if isSeedreamLiteModel(task) {
		return c.generateSeedreamLite(ctx, task, finalPrompt)
	}
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
	fmt.Println("generator image text_only endpoint:", "id=", task.ID, "task_type=", task.TaskType, "endpoint=", endpoint)
	payload, err := buildPayload(task, finalPrompt)
	if err != nil {
		return GenerateResult{}, err
	}
	fmt.Println("generator image text_only payload:", "id=", task.ID, "task_type=", task.TaskType, "model=", payload["model"], "size=", payload["size"], "quality=", payload["quality"], "output_format=", payload["output_format"], "n=", payload["n"])
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
	fmt.Println("generator image json request_start:", "id=", task.ID, "endpoint=", endpoint, "request_bytes=", len(requestData))
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		fmt.Println("generator image json request_failed:", "id=", task.ID, "endpoint=", endpoint, "error=", err)
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
	fmt.Println("generator image json response:", "id=", task.ID, "endpoint=", endpoint, "status=", resp.Status, "status_code=", resp.StatusCode, "response_bytes=", len(responseData))
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
		"model":          imageModel(task),
		"prompt":         finalPrompt,
		"n":              1,
		"size":           size,
		"quality":        normalizeGPTImage2Quality(task.Quality),
		"output_format":  normalizeOutputFormat(task.OutputFormat),
		"background":     normalizeGPTImage2Background(task.Background),
		"moderation":     normalizeModeration(task.Moderation),
		"input_fidelity": normalizeInputFidelity(task.InputFidelity),
	}
	if payload["output_format"] == "webp" || payload["output_format"] == "jpeg" {
		payload["output_compression"] = clamp(task.OutputCompression, 0, 100)
	}
	return payload, nil
}

func imageModel(task *model.Task) string {
	if task != nil && strings.TrimSpace(task.Model) != "" {
		return strings.TrimSpace(task.Model)
	}
	return "gpt-image-2"
}

func isNanoBananaModel(task *model.Task) bool {
	return strings.EqualFold(imageModel(task), "nano-banana-2")
}

func isSeedreamLiteModel(task *model.Task) bool {
	model := strings.TrimSpace(imageModel(task))
	return strings.EqualFold(model, "doubao-seedream-5.0-lite")
}

func (c *Client) generateSeedreamLite(ctx context.Context, task *model.Task, finalPrompt string) (GenerateResult, error) {
	endpoint, err := joinURL(task.BaseURL, "/v1/images/generations")
	if err != nil {
		return GenerateResult{}, err
	}
	payload, err := buildSeedreamLitePayload(task, finalPrompt)
	if err != nil {
		return GenerateResult{}, err
	}
	requestData, err := json.Marshal(payload)
	if err != nil {
		return GenerateResult{}, err
	}
	fmt.Println("generator image seedream_lite payload:", "id=", task.ID, "endpoint=", endpoint, "size=", payload["size"], "output_format=", payload["output_format"], "ref_images=", len(task.ReferenceImages))
	return c.doGenerateJSON(ctx, task, endpoint, requestData)
}

func buildSeedreamLitePayload(task *model.Task, finalPrompt string) (map[string]any, error) {
	size, err := normalizeSeedreamLiteSize(task.Size)
	if err != nil {
		return nil, err
	}
	outputFormat := normalizeSeedreamOutputFormat(task.OutputFormat)
	images, promptPrefix := seedreamLiteImages(task)
	payload := map[string]any{
		"model":         "doubao-seedream-5.0-lite",
		"prompt":        seedreamLitePrompt(promptPrefix, finalPrompt),
		"size":          size,
		"output_format": outputFormat,
		"watermark":     false,
	}
	if len(images) > 0 {
		payload["image"] = images
	}
	return payload, nil
}

func seedreamLiteImages(task *model.Task) ([]string, []string) {
	images := []string{}
	labels := []string{}
	imageIndex := 0
	for _, image := range referenceInputs(task) {
		if strings.TrimSpace(image.URL) != "" {
			imageIndex++
			label := referenceFileLabel(image, imageIndex-1)
			images = append(images, strings.TrimSpace(image.URL))
			labels = append(labels, fmt.Sprintf("第%d张图是：%s", len(images), label))
		}
		if strings.TrimSpace(image.MaskURL) != "" {
			images = append(images, strings.TrimSpace(image.MaskURL))
			labels = append(labels, fmt.Sprintf("第%d张图是MASK", len(images)))
		}
		if len(images) >= 16 {
			return images[:16], labels[:16]
		}
	}
	return images, labels
}

func seedreamLitePrompt(labels []string, finalPrompt string) string {
	finalPrompt = strings.TrimSpace(finalPrompt)
	if len(labels) == 0 {
		return finalPrompt
	}
	prefix := strings.Join(labels, "，")
	if finalPrompt == "" {
		return prefix
	}
	return prefix + "；这里是用户的提示词：" + finalPrompt
}

func (c *Client) generateNanoBanana(ctx context.Context, task *model.Task, finalPrompt string) (GenerateResult, error) {
	endpoint, err := joinURL(task.BaseURL, "/v1beta/models/nano-banana-2:generateContent")
	if err != nil {
		return GenerateResult{}, err
	}
	fmt.Println("generator image nano_banana endpoint:", "id=", task.ID, "task_type=", task.TaskType, "endpoint=", endpoint, "ref_images=", len(task.ReferenceImages))
	payload, cleanup, err := c.buildNanoBananaPayload(ctx, task, finalPrompt)
	if err != nil {
		for _, path := range cleanup {
			_ = os.Remove(path)
		}
		return GenerateResult{}, err
	}
	defer func() {
		for _, path := range cleanup {
			_ = os.Remove(path)
		}
	}()
	requestData, err := json.Marshal(payload)
	if err != nil {
		return GenerateResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestData))
	if err != nil {
		return GenerateResult{}, err
	}
	req.Header.Set("x-goog-api-key", task.APIKey)
	req.Header.Set("Content-Type", "application/json")
	requestHeaders := requestInfoJSON(req)
	result := GenerateResult{RequestHeaders: requestHeaders, RequestJSON: compactImageResponseForStorage(requestData)}
	req = attachRequestTrace(req, &result)

	result.RequestStartedAt = time.Now()
	fmt.Println("generator image nano_banana request_start:", "id=", task.ID, "endpoint=", endpoint, "request_bytes=", len(requestData))
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		fmt.Println("generator image nano_banana request_failed:", "id=", task.ID, "endpoint=", endpoint, "error=", err)
		return result, err
	}
	defer resp.Body.Close()
	result.ResponseHeadersReceivedAt = time.Now()
	result.UpstreamStatus = resp.Status
	result.UpstreamStatusCode = resp.StatusCode
	result.UpstreamServerDate = resp.Header.Get("Date")
	result.ResponseHeaders = responseInfoJSON(resp)
	responseData, err := readImageResponse(resp.Body)
	result.ResponseBodyReadAt = time.Now()
	result.ResponseJSON = compactImageResponseForStorage(responseData)
	fmt.Println("generator image nano_banana response:", "id=", task.ID, "endpoint=", endpoint, "status=", resp.Status, "status_code=", resp.StatusCode, "response_bytes=", len(responseData))
	if err != nil {
		return result, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("生成接口请求失败：HTTP %d %s", resp.StatusCode, string(responseData))
	}
	if upstreamErr := extractUpstreamError(responseData); upstreamErr != "" {
		return result, fmt.Errorf("上游图片接口返回错误：%s", upstreamErr)
	}
	files, err := c.materializeNanoBananaImages(task, responseData)
	if err != nil {
		result.Files = files
		return result, err
	}
	result.Files = files
	return result, nil
}

func (c *Client) buildNanoBananaPayload(ctx context.Context, task *model.Task, finalPrompt string) (map[string]any, []string, error) {
	if err := os.MkdirAll(c.TempDir, 0o755); err != nil {
		return nil, nil, err
	}
	contents := []map[string]any{}
	cleanup := []string{}
	for index, image := range referenceInputs(task) {
		imageURL := strings.TrimSpace(image.URL)
		if imageURL == "" {
			continue
		}
		path, err := c.downloadReferenceAsset(ctx, task.ID, "nano-ref", index, imageURL)
		if err != nil {
			return nil, cleanup, err
		}
		cleanup = append(cleanup, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, cleanup, err
		}
		contents = append(contents, nanoBananaImageContent(fmt.Sprintf("这是%s图片", referenceFileLabel(image, index)), path, data))
		maskURL := strings.TrimSpace(image.MaskURL)
		if maskURL == "" {
			continue
		}
		maskPath, err := c.downloadReferenceAsset(ctx, task.ID, "nano-mask", index, maskURL)
		if err != nil {
			return nil, cleanup, err
		}
		cleanup = append(cleanup, maskPath)
		maskData, err := os.ReadFile(maskPath)
		if err != nil {
			return nil, cleanup, err
		}
		contents = append(contents, nanoBananaImageContent("这是MASK图片", maskPath, maskData))
	}
	contents = append(contents, map[string]any{
		"role": "user",
		"parts": []map[string]any{
			{"text": finalPrompt},
		},
	})
	aspectRatio, imageSize, err := nanoBananaImageConfig(task.Size)
	if err != nil {
		return nil, cleanup, err
	}
	imageConfig := map[string]string{
		"imageSize": imageSize,
	}
	if aspectRatio != "" {
		imageConfig["aspectRatio"] = aspectRatio
	}
	return map[string]any{
		"contents": contents,
		"generationConfig": map[string]any{
			"responseModalities": []string{"IMAGE"},
			"imageConfig":        imageConfig,
		},
	}, cleanup, nil
}

func nanoBananaImageContent(label, path string, data []byte) map[string]any {
	return map[string]any{
		"role": "user",
		"parts": []map[string]any{
			{"text": label},
			{
				"inline_data": map[string]string{
					"mime_type": contentTypeFromPath(path),
					"data":      base64.StdEncoding.EncodeToString(data),
				},
			},
		},
	}
}

func nanoBananaImageConfig(size string) (string, string, error) {
	fields := strings.Fields(strings.TrimSpace(size))
	aspectRatio := "1:1"
	imageSize := "1K"
	if strings.TrimSpace(size) == "" {
		return aspectRatio, imageSize, nil
	}
	if len(fields) != 2 {
		return "", "", fmt.Errorf("nano-banana-2 尺寸格式必须是 \"1K 1:1\" 这种 imageSize + aspectRatio")
	}
	if len(fields) >= 1 {
		switch fields[0] {
		case "512", "1K", "2K", "4K":
			imageSize = fields[0]
		default:
			return "", "", fmt.Errorf("nano-banana-2 imageSize 只支持 512、1K、2K、4K，且 K 必须大写")
		}
	}
	if len(fields) >= 2 {
		switch fields[1] {
		case "auto":
			aspectRatio = ""
		case "1:1", "1:4", "1:8", "2:3", "3:2", "3:4", "4:1", "4:3", "4:5", "5:4", "8:1", "9:16", "16:9", "21:9":
			aspectRatio = fields[1]
		default:
			return "", "", fmt.Errorf("nano-banana-2 aspectRatio 不支持：%s", fields[1])
		}
	}
	return aspectRatio, imageSize, nil
}

func (c *Client) generateWithReferences(ctx context.Context, task *model.Task, finalPrompt string) (GenerateResult, error) {
	endpoint, err := joinURL(task.BaseURL, "/v1/images/edits")
	if err != nil {
		return GenerateResult{}, err
	}
	fmt.Println("generator image edit endpoint:", "id=", task.ID, "task_type=", task.TaskType, "endpoint=", endpoint, "ref_images=", len(task.ReferenceImages))
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
	references := referenceInputs(task)
	maskedIndex := -1
	for index, image := range references {
		if strings.TrimSpace(image.MaskURL) != "" {
			maskedIndex = index
			break
		}
	}
	if maskedIndex >= 0 {
		image := references[maskedIndex]
		baseLabel := referenceFileLabel(image, maskedIndex)
		maskLabel := maskReferenceFileLabel(image, maskedIndex)
		duplicateBaseIndex := -1
		for index, candidate := range references {
			if index == maskedIndex || strings.TrimSpace(candidate.MaskURL) != "" {
				continue
			}
			if sameReferenceURL(candidate.URL, image.URL) {
				baseLabel = referenceFileLabel(candidate, index)
				duplicateBaseIndex = index
				break
			}
		}
		if duplicateBaseIndex < 0 && baseLabel == maskLabel {
			if promptBaseLabel := inferMaskedBaseLabelFromPrompt(finalPrompt, maskLabel); promptBaseLabel != "" {
				baseLabel = promptBaseLabel
				fmt.Println("generator image edit infer_mask_base_from_prompt:", "id=", task.ID, "mask_ref=", maskLabel, "base_ref=", baseLabel)
			}
		}
		path, err := c.downloadReferenceAsset(ctx, task.ID, "ref", maskedIndex, image.URL)
		if err != nil {
			_ = writer.Close()
			return GenerateResult{RequestJSON: buildMultipartRequestSource(requestSummary, requestImages)}, err
		}
		cleanup = append(cleanup, path)
		maskPath, err := c.downloadReferenceAsset(ctx, task.ID, "mask", maskedIndex, image.MaskURL)
		if err != nil {
			_ = writer.Close()
			return GenerateResult{RequestJSON: buildMultipartRequestSource(requestSummary, requestImages)}, err
		}
		cleanup = append(cleanup, maskPath)
		editImagePath, editMaskPath, err := prepareMaskedEditPair(task.ID, maskedIndex, path, maskPath)
		if err != nil {
			_ = writer.Close()
			return GenerateResult{RequestJSON: buildMultipartRequestSource(requestSummary, requestImages)}, err
		}
		cleanup = append(cleanup, editImagePath, editMaskPath)
		editImageFilename := baseLabel + ".png"
		editMaskFilename := maskLabel + ".png"
		requestImages = append(requestImages, multipartFileSource{
			Field:    "image[]",
			Filename: editImageFilename,
			Source:   image.URL,
		})
		if err := addMultipartFile(writer, "image[]", editImagePath, editImageFilename); err != nil {
			_ = writer.Close()
			return GenerateResult{RequestJSON: buildMultipartRequestSource(requestSummary, requestImages)}, err
		}
		requestImages = append(requestImages, multipartFileSource{
			Field:    "mask",
			Filename: editMaskFilename,
			Source:   image.MaskURL,
		})
		if err := addMultipartFile(writer, "mask", editMaskPath, editMaskFilename); err != nil {
			_ = writer.Close()
			return GenerateResult{RequestJSON: buildMultipartRequestSource(requestSummary, requestImages)}, err
		}
		fmt.Println("generator image edit mask_pair:", "id=", task.ID, "image=", editImageFilename, "mask=", editMaskFilename)
		if duplicateBaseIndex >= 0 {
			fmt.Println("generator image edit skip_duplicate_mask_base:", "id=", task.ID, "masked_ref=", maskLabel, "base_ref=", baseLabel)
		}
	}
	for index, image := range references {
		if index == maskedIndex {
			continue
		}
		if maskedIndex >= 0 && strings.TrimSpace(image.MaskURL) == "" && sameReferenceURL(image.URL, references[maskedIndex].URL) {
			continue
		}
		label := referenceFileLabel(image, index)
		path, err := c.downloadReferenceAsset(ctx, task.ID, "ref", index, image.URL)
		if err != nil {
			_ = writer.Close()
			return GenerateResult{RequestJSON: buildMultipartRequestSource(requestSummary, requestImages)}, err
		}
		cleanup = append(cleanup, path)
		filename := label + filepath.Ext(path)
		requestImages = append(requestImages, multipartFileSource{
			Field:    "image[]",
			Filename: filename,
			Source:   image.URL,
		})
		if err := addMultipartFile(writer, "image[]", path, filename); err != nil {
			_ = writer.Close()
			return GenerateResult{RequestJSON: buildMultipartRequestSource(requestSummary, requestImages)}, err
		}
	}
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
	fmt.Println("generator image edit request_start:", "id=", task.ID, "endpoint=", endpoint, "request_summary_bytes=", len(requestData))
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		fmt.Println("generator image edit request_failed:", "id=", task.ID, "endpoint=", endpoint, "error=", err)
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
	fmt.Println("generator image edit response:", "id=", task.ID, "endpoint=", endpoint, "status=", resp.Status, "status_code=", resp.StatusCode, "response_bytes=", len(responseData))
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

func (c *Client) materializeNanoBananaImages(task *model.Task, responseData []byte) ([]string, error) {
	var parsed geminiGenerateContentResponse
	if err := json.Unmarshal(responseData, &parsed); err != nil {
		return nil, err
	}
	files := []string{}
	for _, candidate := range parsed.Candidates {
		for _, part := range candidate.Content.Parts {
			inlineData := part.InlineData
			if inlineData == nil {
				inlineData = part.InlineDataSnake
			}
			if inlineData == nil || strings.TrimSpace(inlineData.Data) == "" {
				continue
			}
			file, err := c.materializeInlineImage(task.ID, len(files), inlineData.mimeType(), inlineData.Data)
			if err != nil {
				return files, err
			}
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("生成接口没有返回图片")
	}
	return files, nil
}

func (data geminiInlineData) mimeType() string {
	if strings.TrimSpace(data.MimeType) != "" {
		return strings.TrimSpace(data.MimeType)
	}
	return strings.TrimSpace(data.MimeTypeSnake)
}

func (c *Client) materializeInlineImage(taskID string, index int, mimeType, b64 string) (string, error) {
	if err := os.MkdirAll(c.TempDir, 0o755); err != nil {
		return "", err
	}
	ext := extFromContentType(mimeType)
	path := filepath.Join(c.TempDir, fmt.Sprintf("%s-%d.%s", taskID, index, ext))
	data, err := base64.StdEncoding.DecodeString(stripDataURLPrefix(b64))
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
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
	case "transparent", "opaque", "auto":
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

func normalizeSeedreamOutputFormat(value string) string {
	switch value {
	case "png", "jpeg":
		return value
	default:
		return "jpeg"
	}
}

func normalizeSeedreamLiteSize(size string) (string, error) {
	switch strings.TrimSpace(size) {
	case "2K", "3K", "4K":
		return strings.TrimSpace(size), nil
	case "2048x2048", "2304x1728", "1728x2304", "2848x1600", "1600x2848", "2496x1664", "1664x2496", "3136x1344",
		"3072x3072", "3456x2592", "2592x3456", "4096x2304", "2304x4096", "2496x3744", "3744x2496", "4704x2016",
		"4096x4096", "3520x4704", "4704x3520", "5504x3040", "3040x5504", "3328x4992", "4992x3328", "6240x2656":
		return strings.TrimSpace(size), nil
	case "":
		return "2848x1600", nil
	default:
		return "", fmt.Errorf("doubao-seedream-5.0-lite 不支持尺寸：%s", size)
	}
}

func normalizeInputFidelity(value string) string {
	switch value {
	case "high", "low":
		return value
	default:
		return "high"
	}
}

func clamp(value, minValue, maxValue int) int {
	return min(max(value, minValue), maxValue)
}

func hasReferenceImages(task *model.Task) bool {
	return len(task.ReferenceImages) > 0
}

func buildReferenceGuidePrompt(userPrompt string, images []model.UploadedImage, videos []model.MediaAsset, audios []model.MediaAsset) string {
	prompt := strings.TrimSpace(userPrompt)
	lines := []string{}
	imageIndex := 0
	for _, image := range images {
		if strings.TrimSpace(image.URL) == "" {
			continue
		}
		imageIndex++
		label := referenceLabel(image.ReferenceLabel, image.NodeID, fmt.Sprintf("IMAGE_%d", imageIndex))
		extra := ""
		if image.MaskURL != "" {
			extra = "，带蒙版"
		}
		lines = append(lines, fmt.Sprintf("- [%s] 对应第 %d 张参考图%s。", label, imageIndex, extra))
	}
	videoIndex := 0
	for _, video := range videos {
		if strings.TrimSpace(video.URL) == "" {
			continue
		}
		videoIndex++
		label := referenceLabel(video.ReferenceLabel, video.NodeID, fmt.Sprintf("VIDEO_%d", videoIndex))
		clip := ""
		if video.ClipStart > 0 || video.ClipEnd > 0 {
			clip = fmt.Sprintf("，截取 %d 秒到 %d 秒", video.ClipStart, video.ClipEnd)
		}
		lines = append(lines, fmt.Sprintf("- [%s] 对应第 %d 个参考视频%s。", label, videoIndex, clip))
	}
	audioIndex := 0
	for _, audio := range audios {
		if strings.TrimSpace(audio.URL) == "" {
			continue
		}
		audioIndex++
		label := referenceLabel(audio.ReferenceLabel, audio.NodeID, fmt.Sprintf("AUDIO_%d", audioIndex))
		lines = append(lines, fmt.Sprintf("- [%s] 对应第 %d 个参考音频。", label, audioIndex))
	}
	if len(lines) == 0 {
		return prompt
	}
	guide := strings.Join(append([]string{
		"引用素材说明：",
		"用户提示词中的 [标签] 或 @标签 指向下面对应的参考素材。请按这些标签理解用户指定的图片、视频或音频。",
	}, lines...), "\n")
	if prompt == "" {
		return guide
	}
	return strings.TrimSpace(guide + "\n\n用户提示词：\n" + prompt)
}

func referenceLabel(label, nodeID, fallback string) string {
	if strings.TrimSpace(label) != "" {
		return strings.TrimSpace(label)
	}
	if strings.TrimSpace(nodeID) != "" {
		return strings.TrimSpace(nodeID)
	}
	return fallback
}

func referenceInputs(task *model.Task) []model.UploadedImage {
	return append([]model.UploadedImage{}, task.ReferenceImages...)
}

func sameReferenceURL(a, b string) bool {
	return strings.TrimSpace(a) != "" && strings.TrimSpace(a) == strings.TrimSpace(b)
}

func inferMaskedBaseLabelFromPrompt(prompt, maskLabel string) string {
	maskLabel = strings.TrimPrefix(strings.TrimSpace(maskLabel), "@")
	if maskLabel == "" {
		return ""
	}
	labels := regexp.MustCompile(`@([A-Z]+\d{2})`).FindAllStringSubmatch(prompt, -1)
	for index, match := range labels {
		if len(match) < 2 || match[1] != maskLabel {
			continue
		}
		for previous := index - 1; previous >= 0; previous-- {
			label := labels[previous][1]
			if strings.HasPrefix(label, "IMAGE") {
				return label
			}
		}
	}
	return ""
}

func referenceFileLabel(image model.UploadedImage, index int) string {
	label := referenceLabel(image.ReferenceLabel, image.NodeID, fmt.Sprintf("IMAGE%02d", index+1))
	return cleanReferenceFileLabel(label, fmt.Sprintf("IMAGE%02d", index+1))
}

func maskReferenceFileLabel(image model.UploadedImage, index int) string {
	label := referenceLabel(image.MaskReferenceLabel, image.ReferenceLabel, fmt.Sprintf("MASK%02d", index+1))
	return cleanReferenceFileLabel(label, fmt.Sprintf("MASK%02d", index+1))
}

func cleanReferenceFileLabel(label, fallback string) string {
	label = strings.TrimPrefix(strings.TrimSpace(label), "@")
	label = strings.TrimSuffix(label, filepath.Ext(label))
	clean := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, label)
	clean = strings.Trim(clean, "-")
	if clean == "" {
		return fallback
	}
	return clean
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
		"model":         imageModel(task),
		"prompt":        finalPrompt,
		"n":             "1",
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
	if _, err = io.Copy(file, resp.Body); err != nil {
		_ = file.Close()
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	if ext == "webp" {
		convertedPath, err := convertImageToPNG(path)
		if err != nil {
			return "", fmt.Errorf("webp reference image decode failed: %w", err)
		}
		_ = os.Remove(path)
		return convertedPath, nil
	}
	return path, nil
}

func convertImageToPNG(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	source, _, err := image.Decode(file)
	if err != nil {
		return "", err
	}
	outputPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".png"
	output, err := os.Create(outputPath)
	if err != nil {
		return "", err
	}
	defer output.Close()
	if err := png.Encode(output, source); err != nil {
		_ = os.Remove(outputPath)
		return "", err
	}
	return outputPath, nil
}

func prepareMaskedEditPair(taskID string, index int, imagePath, maskPath string) (string, string, error) {
	baseFile, err := os.Open(imagePath)
	if err != nil {
		return "", "", err
	}
	baseImage, _, err := image.Decode(baseFile)
	_ = baseFile.Close()
	if err != nil {
		return "", "", fmt.Errorf("decode base image: %w", err)
	}
	maskFile, err := os.Open(maskPath)
	if err != nil {
		return "", "", err
	}
	maskImage, _, err := image.Decode(maskFile)
	_ = maskFile.Close()
	if err != nil {
		return "", "", fmt.Errorf("decode mask image: %w", err)
	}
	bounds := baseImage.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return "", "", fmt.Errorf("invalid masked edit image size")
	}
	baseOutput := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(baseOutput, baseOutput.Bounds(), baseImage, bounds.Min, draw.Src)

	maskBounds := maskImage.Bounds()
	maskOutput := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			maskX := maskBounds.Min.X + x*maskBounds.Dx()/width
			maskY := maskBounds.Min.Y + y*maskBounds.Dy()/height
			_, _, _, alpha := maskImage.At(maskX, maskY).RGBA()
			outputAlpha := uint8(alpha >> 8)
			maskOutput.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: outputAlpha})
		}
	}

	basePath := filepath.Join(filepath.Dir(imagePath), fmt.Sprintf("%s-edit-image-%d.png", taskID, index))
	preparedMaskPath := filepath.Join(filepath.Dir(maskPath), fmt.Sprintf("%s-edit-mask-%d.png", taskID, index))
	if err := writePNGUnderLimit(basePath, baseOutput, 50<<20); err != nil {
		return "", "", err
	}
	if err := writePNGUnderLimit(preparedMaskPath, maskOutput, 50<<20); err != nil {
		_ = os.Remove(basePath)
		return "", "", err
	}
	return basePath, preparedMaskPath, nil
}

func writePNGUnderLimit(path string, img image.Image, limit int64) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	encodeErr := png.Encode(file, img)
	closeErr := file.Close()
	if encodeErr != nil {
		_ = os.Remove(path)
		return encodeErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return closeErr
	}
	info, err := os.Stat(path)
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	if info.Size() > limit {
		_ = os.Remove(path)
		return fmt.Errorf("masked edit image exceeds 50MB after normalization: %s", filepath.Base(path))
	}
	return nil
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

func addMultipartFile(writer *multipart.Writer, fieldName, path string, filename ...string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	uploadName := filepath.Base(path)
	if len(filename) > 0 && strings.TrimSpace(filename[0]) != "" {
		uploadName = filepath.Base(filename[0])
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, uploadName))
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
