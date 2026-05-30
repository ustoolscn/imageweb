package sourceclean

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var (
	dataURLPattern     = regexp.MustCompile(`(?i)data:[^,\s]+;base64,[A-Za-z0-9+/=_-]+`)
	longBase64Pattern  = regexp.MustCompile(`[A-Za-z0-9+/_-]{512,}={0,2}`)
	base64KeyFragments = []string{"base64", "b64"}
)

func CompactBytesForStorage(data []byte) string {
	return CompactStringForStorage(string(data))
}

func CompactStringForStorage(source string) string {
	if source == "" {
		return ""
	}
	var value any
	if err := json.Unmarshal([]byte(source), &value); err == nil {
		redactValue(value, "")
		compact, err := json.Marshal(value)
		if err == nil {
			return string(compact)
		}
	}
	return redactInlineBase64(source)
}

func redactValue(value any, parentKey string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if text, ok := child.(string); ok {
				if shouldRedactField(key, parentKey, text) {
					typed[key] = omittedText(text)
				} else {
					typed[key] = redactInlineBase64(text)
				}
				continue
			}
			redactValue(child, key)
		}
	case []any:
		for _, child := range typed {
			redactValue(child, parentKey)
		}
	}
}

func shouldRedactField(key, parentKey, text string) bool {
	if text == "" || isOmittedText(text) {
		return false
	}
	normalizedKey := strings.ToLower(strings.TrimSpace(key))
	normalizedParent := strings.ToLower(strings.TrimSpace(parentKey))
	if normalizedKey == "b64_json" || normalizedKey == "thoughtsignature" {
		return true
	}
	for _, fragment := range base64KeyFragments {
		if strings.Contains(normalizedKey, fragment) {
			return true
		}
	}
	if normalizedKey == "data" && (normalizedParent == "inlinedata" || normalizedParent == "inline_data") {
		return true
	}
	return looksLikeInlineBase64(text)
}

func redactInlineBase64(text string) string {
	if text == "" || isOmittedText(text) {
		return text
	}
	text = dataURLPattern.ReplaceAllStringFunc(text, func(match string) string {
		return fmt.Sprintf("[base64 data URL omitted, %d chars]", len(match))
	})
	text = longBase64Pattern.ReplaceAllStringFunc(text, func(match string) string {
		if isOmittedText(match) {
			return match
		}
		return omittedText(match)
	})
	return text
}

func looksLikeInlineBase64(text string) bool {
	return dataURLPattern.MatchString(text) || longBase64Pattern.MatchString(text)
}

func omittedText(text string) string {
	return fmt.Sprintf("[base64 image omitted, %d chars]", len(text))
}

func isOmittedText(text string) bool {
	return strings.HasPrefix(text, "[base64 ") && strings.Contains(text, " omitted")
}
