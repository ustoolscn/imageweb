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

func TestGPTImage2PromptInjectsSelectedPixelSize(t *testing.T) {
	task := &model.Task{
		Model:        "gpt-image-2",
		Size:         "1024x1536",
		Quality:      "auto",
		OutputFormat: "png",
		Moderation:   "low",
	}
	payload, err := buildPayload(task, "画一只玻璃杯")
	if err != nil {
		t.Fatalf("buildPayload returned error: %v", err)
	}
	want := "画一只玻璃杯\n\n请生成尺寸为 1024像素x1536像素 的图片。"
	if got := payload["prompt"]; got != want {
		t.Fatalf("prompt = %#v, want %#v", got, want)
	}

	summary, err := buildEditRequestSummary(task, "把玻璃杯改成蓝色")
	if err != nil {
		t.Fatalf("buildEditRequestSummary returned error: %v", err)
	}
	wantEdit := "把玻璃杯改成蓝色\n\n请生成尺寸为 1024像素x1536像素 的图片。"
	if got := summary["prompt"]; got != wantEdit {
		t.Fatalf("edit prompt = %#v, want %#v", got, wantEdit)
	}
}

func TestGPTImage2PromptDoesNotInjectAutoSize(t *testing.T) {
	task := &model.Task{
		Model:        "gpt-image-2",
		Size:         "auto",
		Quality:      "auto",
		OutputFormat: "png",
		Moderation:   "low",
	}
	payload, err := buildPayload(task, "画一只玻璃杯")
	if err != nil {
		t.Fatalf("buildPayload returned error: %v", err)
	}
	if got := payload["prompt"]; got != "画一只玻璃杯" {
		t.Fatalf("prompt = %#v, want original prompt", got)
	}
}

func TestCompatibleImagePromptDoesNotInjectPixelSize(t *testing.T) {
	task := &model.Task{
		Model:        "compatible-image-model",
		Size:         "1024x1536",
		Quality:      "auto",
		OutputFormat: "png",
		Moderation:   "low",
	}
	payload, err := buildPayload(task, "画一只玻璃杯")
	if err != nil {
		t.Fatalf("buildPayload returned error: %v", err)
	}
	if got := payload["prompt"]; got != "画一只玻璃杯" {
		t.Fatalf("prompt = %#v, want original prompt", got)
	}
}
