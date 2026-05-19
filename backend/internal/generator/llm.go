package generator

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"image-web/backend/internal/model"
)

func (c *Client) GenerateText(ctx context.Context, llm model.LLMRequest) (string, error) {
	endpoint, err := joinURL(llm.BaseURL, "/v1/chat/completions")
	if err != nil {
		return "", err
	}
	prompt := buildReferenceGuidePrompt(strings.TrimSpace(llm.Prompt), llm.ReferenceImages, llm.ReferenceVideos, llm.ReferenceAudios)
	content := []map[string]any{}
	if prompt != "" {
		content = append(content, map[string]any{"type": "text", "text": prompt})
	}
	for _, image := range llm.ReferenceImages {
		if image.URL == "" {
			continue
		}
		content = append(content, map[string]any{"type": "image_url", "image_url": map[string]string{"url": image.URL}})
		if image.MaskURL != "" {
			content = append(content, map[string]any{"type": "text", "text": "Mask URL for the previous image: " + image.MaskURL})
		}
	}
	for _, video := range llm.ReferenceVideos {
		if video.URL != "" {
			content = append(content, map[string]any{"type": "text", "text": "Reference video URL: " + video.URL})
		}
	}
	for _, audio := range llm.ReferenceAudios {
		if audio.URL != "" {
			content = append(content, map[string]any{"type": "text", "text": "Reference audio URL: " + audio.URL})
		}
	}
	modelName := strings.TrimSpace(llm.Model)
	if modelName == "" {
		modelName = "gpt-4o-mini"
	}
	var messageContent any = prompt
	if len(content) > 1 || (len(content) == 1 && messageContent == "") {
		messageContent = content
	}
	payload := map[string]any{
		"model": modelName,
		"messages": []map[string]any{
			{"role": "user", "content": messageContent},
		},
		"stream": true,
	}
	if effort := cleanReasoningEffort(llm.ReasoningEffort); effort != "" {
		payload["reasoning_effort"] = effort
	}
	requestData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+llm.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("LLM 请求失败：HTTP %d %s", resp.StatusCode, string(data))
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") || bytes.HasPrefix(bytes.TrimSpace(data), []byte("data:")) {
		return parseChatCompletionStream(data), nil
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		OutputText string `json:"output_text"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", err
	}
	if result.OutputText != "" {
		return result.OutputText, nil
	}
	if len(result.Choices) == 0 {
		return "", nil
	}
	switch content := result.Choices[0].Message.Content.(type) {
	case string:
		return strings.TrimSpace(content), nil
	case []any:
		parts := []string{}
		for _, item := range content {
			if entry, ok := item.(map[string]any); ok {
				if text, ok := entry["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n")), nil
	default:
		return "", nil
	}
}

func cleanReasoningEffort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func parseChatCompletionStream(data []byte) string {
	parts := []string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var event struct {
			Choices []struct {
				Delta struct {
					Content any `json:"content"`
				} `json:"delta"`
				Message struct {
					Content any `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			OutputText string `json:"output_text"`
		}
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue
		}
		if event.OutputText != "" {
			parts = append(parts, event.OutputText)
		}
		for _, choice := range event.Choices {
			parts = appendLLMContent(parts, choice.Delta.Content)
			parts = appendLLMContent(parts, choice.Message.Content)
		}
	}
	return strings.TrimSpace(strings.Join(parts, ""))
}

func appendLLMContent(parts []string, content any) []string {
	switch value := content.(type) {
	case string:
		if value != "" {
			parts = append(parts, value)
		}
	case []any:
		for _, item := range value {
			if entry, ok := item.(map[string]any); ok {
				if text, ok := entry["text"].(string); ok && text != "" {
					parts = append(parts, text)
				}
			}
		}
	}
	return parts
}
