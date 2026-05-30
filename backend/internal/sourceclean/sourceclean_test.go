package sourceclean

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompactStringForStorageRedactsInlineDataRequest(t *testing.T) {
	source := `{
		"contents": [{
			"parts": [{
				"inline_data": {
					"mime_type": "image/png",
					"data": "abcdef"
				}
			}]
		}]
	}`

	compact := CompactStringForStorage(source)
	var value map[string]any
	if err := json.Unmarshal([]byte(compact), &value); err != nil {
		t.Fatalf("compact source is not valid JSON: %v", err)
	}
	part := value["contents"].([]any)[0].(map[string]any)["parts"].([]any)[0].(map[string]any)
	inlineData := part["inline_data"].(map[string]any)
	if got, want := inlineData["data"], "[base64 image omitted, 6 chars]"; got != want {
		t.Fatalf("inline data = %v, want %v", got, want)
	}
}

func TestCompactStringForStorageRedactsB64JSON(t *testing.T) {
	source := `{"data":[{"b64_json":"abcdef"}]}`

	compact := CompactStringForStorage(source)
	var value map[string]any
	if err := json.Unmarshal([]byte(compact), &value); err != nil {
		t.Fatalf("compact source is not valid JSON: %v", err)
	}
	item := value["data"].([]any)[0].(map[string]any)
	if got, want := item["b64_json"], "[base64 image omitted, 6 chars]"; got != want {
		t.Fatalf("b64_json = %v, want %v", got, want)
	}
}

func TestCompactStringForStorageRedactsDataURLText(t *testing.T) {
	raw := "prefix data:image/png;base64," + strings.Repeat("a", 600) + " suffix"

	compact := CompactStringForStorage(raw)
	if strings.Contains(compact, strings.Repeat("a", 80)) {
		t.Fatalf("compact source still contains raw base64: %s", compact)
	}
	if !strings.Contains(compact, "[base64 data URL omitted, ") {
		t.Fatalf("compact source did not include data URL placeholder: %s", compact)
	}
}

func TestCompactStringForStorageIsIdempotent(t *testing.T) {
	source := `{"inlineData":{"data":"[base64 image omitted, 123 chars]"}}`

	if got := CompactStringForStorage(source); got != `{"inlineData":{"data":"[base64 image omitted, 123 chars]"}}` {
		t.Fatalf("compact source = %s", got)
	}
}
