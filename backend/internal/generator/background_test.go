package generator

import (
	"testing"

	"image-web/backend/internal/model"
)

func TestGPTImage2PayloadOmitsBackgroundParameter(t *testing.T) {
	task := &model.Task{
		Model:        "gpt-image-2",
		Size:         "1024x1024",
		Quality:      "auto",
		OutputFormat: "png",
		Background:   "transparent",
		Moderation:   "low",
	}
	payload, err := buildPayload(task, "make an icon")
	if err != nil {
		t.Fatalf("buildPayload returned error: %v", err)
	}
	if _, ok := payload["background"]; ok {
		t.Fatalf("gpt-image-2 payload should not include background: %#v", payload)
	}

	summary, err := buildEditRequestSummary(task, "edit the icon")
	if err != nil {
		t.Fatalf("buildEditRequestSummary returned error: %v", err)
	}
	if _, ok := summary["background"]; ok {
		t.Fatalf("gpt-image-2 edit payload should not include background: %#v", summary)
	}
}

func TestCompatibleImagePayloadKeepsBackgroundParameter(t *testing.T) {
	task := &model.Task{
		Model:        "compatible-image-model",
		Size:         "1024x1024",
		Quality:      "auto",
		OutputFormat: "png",
		Background:   "transparent",
		Moderation:   "low",
	}
	payload, err := buildPayload(task, "make an icon")
	if err != nil {
		t.Fatalf("buildPayload returned error: %v", err)
	}
	if got := payload["background"]; got != "transparent" {
		t.Fatalf("background = %v, want transparent", got)
	}
}
