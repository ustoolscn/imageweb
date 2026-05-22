package generator

import (
	"encoding/json"
	"testing"
)

func TestCompactImageResponseForStorageRedactsLargeInlineFields(t *testing.T) {
	source := []byte(`{
		"candidates": [{
			"content": {
				"parts": [{
					"inlineData": {"data": "abcdef"},
					"thoughtSignature": "signature-value"
				}]
			}
		}]
	}`)

	compact := compactImageResponseForStorage(source)
	var value map[string]any
	if err := json.Unmarshal([]byte(compact), &value); err != nil {
		t.Fatalf("compact response is not valid JSON: %v", err)
	}

	part := value["candidates"].([]any)[0].(map[string]any)["content"].(map[string]any)["parts"].([]any)[0].(map[string]any)
	inlineData := part["inlineData"].(map[string]any)
	if got, want := inlineData["data"], "[base64 image omitted, 6 chars]"; got != want {
		t.Fatalf("data = %v, want %v", got, want)
	}
	if got, want := part["thoughtSignature"], "[base64 image omitted, 15 chars]"; got != want {
		t.Fatalf("thoughtSignature = %v, want %v", got, want)
	}
}
