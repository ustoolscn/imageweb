package imagehost

import (
	"strings"
	"testing"

	"image-web/backend/internal/config"
)

func TestOSSProcessURLsUsePublicObjectURL(t *testing.T) {
	client := New(config.Config{
		OSSBucket:        "bucket",
		OSSPrefix:        "base",
		OSSPublicBaseURL: "https://files.example.com",
	})

	raw := client.publicObjectURL(client.fullKey("docs/photo one.png"))
	if raw != "https://files.example.com/base/docs/photo%20one.png" {
		t.Fatalf("public url = %q", raw)
	}
	thumb := ossImageResizeURL(raw, 480)
	if !strings.Contains(thumb, "x-oss-process=image%2Fresize%2Cw_480") {
		t.Fatalf("thumbnail url = %q", thumb)
	}
	frame := ossVideoSnapshotURL("https://files.example.com/base/video.mp4?version=1", 0)
	if !strings.Contains(frame, "&x-oss-process=video%2Fsnapshot%2Ct_0%2Cf_jpg%2Cw_480") {
		t.Fatalf("frame url = %q", frame)
	}
}

func TestFullKeyKeepsObjectsInsidePrefix(t *testing.T) {
	client := New(config.Config{OSSPrefix: "gallery"})

	if got := client.fullKey("photo.jpg"); got != "gallery/photo.jpg" {
		t.Fatalf("full key = %q", got)
	}
	if got := client.fullKey("gallery/photo.jpg"); got != "gallery/photo.jpg" {
		t.Fatalf("prefixed key = %q", got)
	}
	if got := client.relativeKey("gallery/nested/photo.jpg"); got != "nested/photo.jpg" {
		t.Fatalf("relative key = %q", got)
	}
}
