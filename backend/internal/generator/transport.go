package generator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"time"
)

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
			if key == "b64_json" || key == "data" {
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
		if strings.EqualFold(key, "x-goog-api-key") {
			clean[key] = []string{"***"}
			continue
		}
		clean[key] = values
	}
	return clean
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
