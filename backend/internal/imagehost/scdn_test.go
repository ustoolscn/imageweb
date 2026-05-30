package imagehost

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"image-web/backend/internal/model"
)

type recordingUploader struct {
	records []uploadRecord
}

type uploadRecord struct {
	filename    string
	contentType string
	data        []byte
}

func (u *recordingUploader) UploadReader(ctx context.Context, filename, contentType string, reader io.Reader) (model.UploadedImage, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return model.UploadedImage{}, err
	}
	u.records = append(u.records, uploadRecord{filename: filename, contentType: contentType, data: data})
	return model.UploadedImage{URL: "https://cdn.example/" + filename, Filename: filename}, nil
}

func TestClientInfersContentTypeAndUploadsThumbnailAsJPEG(t *testing.T) {
	var source bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&source, img); err != nil {
		t.Fatal(err)
	}

	recorder := &recordingUploader{}
	client := &Client{uploader: recorder}
	if _, err := client.UploadReader(context.Background(), "sample.png", "application/octet-stream", bytes.NewReader(source.Bytes())); err != nil {
		t.Fatal(err)
	}

	if len(recorder.records) != 2 {
		t.Fatalf("expected original and thumbnail uploads, got %d", len(recorder.records))
	}
	if recorder.records[0].contentType != "image/png" {
		t.Fatalf("original content type = %q, want image/png", recorder.records[0].contentType)
	}
	if recorder.records[1].filename != "sample-thumb.jpg" || recorder.records[1].contentType != "image/jpeg" {
		t.Fatalf("thumbnail upload = %#v, want sample-thumb.jpg image/jpeg", recorder.records[1])
	}
	if !bytes.HasPrefix(recorder.records[1].data, []byte{0xff, 0xd8}) {
		t.Fatal("thumbnail data is not JPEG")
	}
}

func TestHTTPUploaderSendsMultipartFileContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("multipart reader: %v", err)
		}
		part, err := reader.NextPart()
		if err != nil {
			t.Fatalf("next part: %v", err)
		}
		if part.FormName() != "file" {
			t.Fatalf("form name = %q, want file", part.FormName())
		}
		if part.FileName() != "photo.jpg" {
			t.Fatalf("filename = %q, want photo.jpg", part.FileName())
		}
		if got := part.Header.Get("Content-Type"); got != "image/jpeg" {
			t.Fatalf("part content type = %q, want image/jpeg", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"url":"https://cdn.example/photo.jpg","data":{"filename":"photo.jpg"}}`))
	}))
	defer server.Close()

	uploader := &httpUploader{
		UploadURL:  server.URL,
		FieldName:  "file",
		HTTPClient: server.Client(),
	}
	if _, err := uploader.UploadReader(context.Background(), "photo.jpg", "image/jpeg", strings.NewReader("jpeg data")); err != nil {
		t.Fatal(err)
	}
}
