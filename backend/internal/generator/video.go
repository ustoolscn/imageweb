package generator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"image-web/backend/internal/model"
)

type VideoSubmitResult struct {
	GenerateResult
	UpstreamTaskID string
	Status         string
	Progress       int
}

type VideoPollResult struct {
	GenerateResult
	Status     string
	Progress   int
	FailReason string
	VideoURL   string
	Duration   int
	Width      int
	Height     int
}

func (c *Client) SubmitVideo(ctx context.Context, task *model.Task, finalPrompt string) (VideoSubmitResult, error) {
	endpoint, err := joinURL(task.BaseURL, "/v1/video/generations")
	if err != nil {
		return VideoSubmitResult{}, err
	}
	fmt.Println("generator video submit_enter:", "id=", task.ID, "task_type=", task.TaskType, "model=", task.Model, "endpoint=", endpoint)
	payload := buildVideoPayload(task, finalPrompt)
	requestData, err := json.Marshal(payload)
	if err != nil {
		return VideoSubmitResult{}, err
	}
	fmt.Println("video submit request:", "task_id=", task.ID, "endpoint=", endpoint, "task_type=", task.TaskType, "model=", task.Model, "payload=", string(requestData))
	base, responseData, err := c.doJSONExchange(ctx, http.MethodPost, endpoint, task.APIKey, requestData)
	result := VideoSubmitResult{GenerateResult: base}
	if err != nil {
		fmt.Println("generator video submit_http_failed:", "id=", task.ID, "endpoint=", endpoint, "error=", err)
		return result, err
	}
	fmt.Println("generator video submit_response:", "id=", task.ID, "endpoint=", endpoint, "status=", base.UpstreamStatus, "status_code=", base.UpstreamStatusCode, "response_bytes=", len(responseData))
	if base.UpstreamStatusCode < 200 || base.UpstreamStatusCode >= 300 {
		return result, fmt.Errorf("视频生成提交失败: HTTP %d %s", base.UpstreamStatusCode, string(responseData))
	}
	var parsed struct {
		ID       string `json:"id"`
		TaskID   string `json:"task_id"`
		Status   string `json:"status"`
		Progress int    `json:"progress"`
	}
	if err := json.Unmarshal(responseData, &parsed); err != nil {
		return result, err
	}
	result.UpstreamTaskID = firstNonEmpty(parsed.TaskID, parsed.ID)
	result.Status = parsed.Status
	result.Progress = parsed.Progress
	fmt.Println("generator video submit_parsed:", "id=", task.ID, "upstream_task_id=", result.UpstreamTaskID, "upstream_status=", result.Status, "progress=", result.Progress)
	if result.UpstreamTaskID == "" {
		return result, fmt.Errorf("视频接口未返回 task_id")
	}
	return result, nil
}

func (c *Client) PollVideo(ctx context.Context, task *model.Task) (VideoPollResult, error) {
	if task.UpstreamTaskID == "" {
		return VideoPollResult{}, fmt.Errorf("缺少上游视频 task_id")
	}
	endpoint, err := joinURL(task.BaseURL, "/v1/video/generations/"+task.UpstreamTaskID)
	if err != nil {
		return VideoPollResult{}, err
	}
	fmt.Println("generator video poll_enter:", "id=", task.ID, "upstream_task_id=", task.UpstreamTaskID, "endpoint=", endpoint)
	base, responseData, err := c.doJSONExchange(ctx, http.MethodGet, endpoint, task.APIKey, nil)
	result := VideoPollResult{GenerateResult: base}
	if err != nil {
		fmt.Println("generator video poll_http_failed:", "id=", task.ID, "endpoint=", endpoint, "error=", err)
		return result, err
	}
	fmt.Println("generator video poll_response:", "id=", task.ID, "endpoint=", endpoint, "status=", base.UpstreamStatus, "status_code=", base.UpstreamStatusCode, "response_bytes=", len(responseData))
	if base.UpstreamStatusCode < 200 || base.UpstreamStatusCode >= 300 {
		return result, fmt.Errorf("视频任务查询失败: HTTP %d %s", base.UpstreamStatusCode, string(responseData))
	}
	var parsed struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Status     string `json:"status"`
			Progress   any    `json:"progress"`
			FailReason string `json:"fail_reason"`
			ResultURL  string `json:"result_url"`
			Data       struct {
				Content struct {
					VideoURL string `json:"video_url"`
				} `json:"content"`
				Duration int    `json:"duration"`
				Ratio    string `json:"ratio"`
				Status   string `json:"status"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(responseData, &parsed); err != nil {
		return result, err
	}
	result.Status = firstNonEmpty(parsed.Data.Status, parsed.Data.Data.Status)
	result.Progress = parseProgress(parsed.Data.Progress)
	result.FailReason = parsed.Data.FailReason
	if result.FailReason == "" && parsed.Message != "" && !strings.EqualFold(parsed.Code, "success") {
		result.FailReason = parsed.Message
	}
	result.VideoURL = firstNonEmpty(parsed.Data.Data.Content.VideoURL, parsed.Data.ResultURL)
	result.Duration = parsed.Data.Data.Duration
	fmt.Println("generator video poll_parsed:", "id=", task.ID, "upstream_status=", result.Status, "progress=", result.Progress, "fail_reason=", result.FailReason, "video_url_empty=", result.VideoURL == "", "duration=", result.Duration)
	return result, nil
}

func (c *Client) DownloadVideo(ctx context.Context, taskID, videoURL string) (string, error) {
	if strings.TrimSpace(videoURL) == "" {
		return "", fmt.Errorf("视频结果为空")
	}
	if err := os.MkdirAll(c.TempDir, 0o755); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, videoURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("下载视频失败: HTTP %d", resp.StatusCode)
	}
	path := filepath.Join(c.TempDir, taskID+"-video.mp4")
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	_, err = io.Copy(file, resp.Body)
	return path, err
}

func (c *Client) doJSONExchange(ctx context.Context, method, endpoint, apiKey string, requestData []byte) (GenerateResult, []byte, error) {
	var body io.Reader
	if requestData != nil {
		body = bytes.NewReader(requestData)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return GenerateResult{}, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if requestData != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	result := GenerateResult{RequestHeaders: requestInfoJSON(req), RequestJSON: string(requestData)}
	req = attachRequestTrace(req, &result)
	result.RequestStartedAt = time.Now()
	fmt.Println("generator json_exchange request_start:", "method=", method, "endpoint=", endpoint, "request_bytes=", len(requestData))
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		fmt.Println("generator json_exchange request_failed:", "method=", method, "endpoint=", endpoint, "error=", err)
		return result, nil, err
	}
	defer resp.Body.Close()
	result.ResponseHeadersReceivedAt = time.Now()
	result.UpstreamStatus = resp.Status
	result.UpstreamStatusCode = resp.StatusCode
	result.UpstreamServerDate = resp.Header.Get("Date")
	result.ResponseHeaders = responseInfoJSON(resp)
	responseData, err := io.ReadAll(resp.Body)
	result.ResponseBodyReadAt = time.Now()
	result.ResponseJSON = compactImageResponseForStorage(responseData)
	fmt.Println("generator json_exchange response:", "method=", method, "endpoint=", endpoint, "status=", resp.Status, "status_code=", resp.StatusCode, "response_bytes=", len(responseData))
	return result, responseData, err
}

func buildVideoPayload(task *model.Task, finalPrompt string) map[string]any {
	content := []map[string]any{}
	promptMappings := []string{}
	isSeedance15 := isSeedance15VideoModel(task.Model)
	imageIndex := 0
	for _, image := range task.ReferenceImages {
		if image.URL == "" {
			continue
		}
		imageIndex++
		item := map[string]any{
			"type": "image_url",
			"image_url": map[string]string{
				"url": image.URL,
			},
		}
		if isSeedance15 {
			if role := seedance15ImageRole(image.VideoFrameRole); role != "" && len(task.ReferenceImages) > 1 {
				item["role"] = role
			}
		} else {
			item["role"] = "reference_image"
		}
		content = append(content, item)
		if label := referenceLabel(image.ReferenceLabel, image.NodeID, ""); label != "" {
			promptMappings = append(promptMappings, fmt.Sprintf("@%s 对应图片%d", label, imageIndex))
		}
		if image.MaskURL != "" {
			if label := referenceLabel(image.MaskReferenceLabel, image.ReferenceLabel, ""); label != "" {
				promptMappings = append(promptMappings, fmt.Sprintf("@%s 对应图片%d的蒙版", label, imageIndex))
			}
		}
	}
	videoIndex := 0
	for _, video := range task.ReferenceVideos {
		if video.URL == "" {
			continue
		}
		videoIndex++
		videoURL := map[string]any{
			"url": video.URL,
		}
		if video.ClipStart > 0 {
			videoURL["clip_start"] = video.ClipStart
		}
		if video.ClipEnd > 0 && video.ClipEnd > video.ClipStart {
			videoURL["clip_end"] = video.ClipEnd
		}
		item := map[string]any{
			"type":      "video_url",
			"video_url": videoURL,
		}
		if !isSeedance15 {
			item["role"] = "reference_video"
		}
		content = append(content, item)
		if label := referenceLabel(video.ReferenceLabel, video.NodeID, ""); label != "" {
			promptMappings = append(promptMappings, fmt.Sprintf("@%s 对应视频%d", label, videoIndex))
		}
	}
	audioIndex := 0
	for _, audio := range task.ReferenceAudios {
		if audio.URL == "" {
			continue
		}
		audioIndex++
		item := map[string]any{
			"type": "audio_url",
			"audio_url": map[string]string{
				"url": audio.URL,
			},
		}
		if !isSeedance15 {
			item["role"] = "reference_audio"
		}
		content = append(content, item)
		if label := referenceLabel(audio.ReferenceLabel, audio.NodeID, ""); label != "" {
			promptMappings = append(promptMappings, fmt.Sprintf("@%s 对应音频%d", label, audioIndex))
		}
	}
	prompt := strings.TrimSpace(finalPrompt)
	if len(promptMappings) > 0 {
		prefix := strings.Join(promptMappings, "\n")
		if prompt != "" {
			prompt = prefix + "\n\n" + prompt
		} else {
			prompt = prefix
		}
	}
	metadata := map[string]any{
		"generate_audio": task.GenerateAudio,
		"ratio":          firstNonEmpty(task.VideoRatio, "16:9"),
		"resolution":     videoResolutionFromSize(defaultInt(task.VideoWidth, 1280), defaultInt(task.VideoHeight, 720)),
		"duration":       defaultInt(task.VideoDuration, 5),
		"draft":          task.Draft && isSeedance15VideoModel(task.Model),
		"watermark":      false,
	}
	if len(content) > 0 {
		metadata["content"] = content
	}
	payload := map[string]any{
		"model":    firstNonEmpty(task.Model, "doubao-seedance-2.0"),
		"prompt":   prompt,
		"metadata": metadata,
	}
	return payload
}

func videoResolutionFromSize(width, height int) string {
	shortSide := width
	if height < shortSide || shortSide <= 0 {
		shortSide = height
	}
	if shortSide >= 1000 {
		return "1080p"
	}
	if shortSide >= 700 {
		return "720p"
	}
	return "480p"
}

func isSeedance15VideoModel(modelName string) bool {
	normalized := strings.ToLower(strings.TrimSpace(modelName))
	return normalized == "doubao-seedance-1.5-pro" || normalized == "doubao-seedance-1-5-pro"
}

func seedance15ImageRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "first_frame":
		return "first_frame"
	case "last_frame":
		return "last_frame"
	default:
		return ""
	}
}

func IsVideoSucceeded(status string) bool {
	normalized := strings.ToLower(strings.TrimSpace(status))
	return normalized == "success" || normalized == "succeeded"
}

func IsVideoFailed(status string) bool {
	normalized := strings.ToLower(strings.TrimSpace(status))
	return normalized == "fail" || normalized == "failed" || normalized == "error" || normalized == "canceled" || normalized == "cancelled"
}

func parseProgress(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		typed = strings.TrimSpace(strings.TrimSuffix(typed, "%"))
		var parsed int
		_, _ = fmt.Sscan(typed, &parsed)
		return parsed
	default:
		return 0
	}
}

func defaultInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
