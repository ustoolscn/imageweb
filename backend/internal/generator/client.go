package generator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
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
