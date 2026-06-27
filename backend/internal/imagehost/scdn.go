package imagehost

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"image-web/backend/internal/config"
	"image-web/backend/internal/model"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
)

type Client struct {
	oss           *oss.Client
	bucket        string
	prefix        string
	publicBaseURL string
}

type GalleryObject struct {
	Key          string     `json:"key"`
	FullKey      string     `json:"fullKey"`
	Name         string     `json:"name"`
	IsPrefix     bool       `json:"isPrefix"`
	Size         int64      `json:"size"`
	ETag         string     `json:"etag,omitempty"`
	LastModified *time.Time `json:"lastModified,omitempty"`
	StorageClass string     `json:"storageClass,omitempty"`
	ContentType  string     `json:"contentType,omitempty"`
	URL          string     `json:"url,omitempty"`
}

type GalleryList struct {
	Objects   []GalleryObject `json:"objects"`
	Prefix    string          `json:"prefix"`
	NextToken string          `json:"nextToken"`
}

func New(cfg config.Config) *Client {
	ossCfg := oss.LoadDefaultConfig().
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.OSSAccessKeyID, cfg.OSSAccessKeySecret)).
		WithRegion(cfg.OSSRegion)
	if strings.TrimSpace(cfg.OSSEndpoint) != "" {
		ossCfg = ossCfg.WithEndpoint(cfg.OSSEndpoint)
	}
	return &Client{
		oss:           oss.NewClient(ossCfg),
		bucket:        strings.TrimSpace(cfg.OSSBucket),
		prefix:        strings.Trim(strings.ReplaceAll(cfg.OSSPrefix, "\\", "/"), "/"),
		publicBaseURL: strings.TrimRight(cfg.OSSPublicBaseURL, "/"),
	}
}

func (c *Client) UploadReader(ctx context.Context, filename, contentType string, reader io.Reader) (model.UploadedImage, error) {
	if c == nil || c.oss == nil || c.bucket == "" {
		return model.UploadedImage{}, fmt.Errorf("阿里云 OSS 图库未配置")
	}
	data, err := io.ReadAll(io.LimitReader(reader, 512<<20))
	if err != nil {
		return model.UploadedImage{}, err
	}
	contentType = normalizeUploadContentType(filename, contentType)
	key := c.fullKey(uniqueFilename(filename))
	result, err := c.oss.PutObject(ctx, &oss.PutObjectRequest{
		Bucket:        oss.Ptr(c.bucket),
		Key:           oss.Ptr(key),
		Body:          bytes.NewReader(data),
		ContentType:   oss.Ptr(contentType),
		ContentLength: oss.Ptr(int64(len(data))),
	})
	if err != nil {
		return model.UploadedImage{}, fmt.Errorf("上传到阿里云 OSS 失败：%w", err)
	}
	url := c.publicObjectURL(key)
	image := model.UploadedImage{
		URL:            url,
		Filename:       path.Base(key),
		OriginalSize:   int64(len(data)),
		CompressedSize: int64(len(data)),
		ContentType:    contentType,
	}
	if etag := strings.Trim(ptrString(result.ETag), `"`); etag != "" {
		image.ETag = etag
	}
	if isImageUpload(filename, contentType) {
		image.ThumbnailURL = ossImageResizeURL(url, 480)
	}
	if isVideoUpload(filename, contentType) {
		frame := ossVideoSnapshotURL(url, 0)
		image.ThumbnailURL = frame
		image.FirstFrameURL = frame
		image.LastFrameURL = frame
	}
	return image, nil
}

func (c *Client) UploadFile(ctx context.Context, filePath string) (model.UploadedImage, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return model.UploadedImage{}, err
	}
	defer file.Close()
	return c.UploadReader(ctx, filepath.Base(filePath), contentTypeFromFilename(filePath), file)
}

func (c *Client) ListObjects(ctx context.Context, prefix, token string, limit int) (GalleryList, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	fullPrefix := c.fullKey(strings.Trim(strings.ReplaceAll(prefix, "\\", "/"), "/"))
	if prefix == "" && c.prefix != "" {
		fullPrefix = c.prefix + "/"
	} else if fullPrefix != "" && !strings.HasSuffix(fullPrefix, "/") {
		fullPrefix += "/"
	}
	req := &oss.ListObjectsV2Request{
		Bucket:    oss.Ptr(c.bucket),
		Prefix:    oss.Ptr(fullPrefix),
		Delimiter: oss.Ptr("/"),
		MaxKeys:   int32(limit),
	}
	if strings.TrimSpace(token) != "" {
		req.ContinuationToken = oss.Ptr(token)
	}
	result, err := c.oss.ListObjectsV2(ctx, req)
	if err != nil {
		return GalleryList{}, err
	}
	items := []GalleryObject{}
	for _, commonPrefix := range result.CommonPrefixes {
		fullKey := ptrString(commonPrefix.Prefix)
		items = append(items, GalleryObject{Key: c.relativeKey(fullKey), FullKey: fullKey, Name: displayName(c.relativeKey(fullKey), true), IsPrefix: true})
	}
	for _, object := range result.Contents {
		fullKey := ptrString(object.Key)
		if fullKey == "" || strings.HasSuffix(fullKey, "/") {
			continue
		}
		item := GalleryObject{
			Key:          c.relativeKey(fullKey),
			FullKey:      fullKey,
			Name:         displayName(c.relativeKey(fullKey), false),
			Size:         object.Size,
			ETag:         strings.Trim(ptrString(object.ETag), `"`),
			LastModified: object.LastModified,
			StorageClass: ptrString(object.StorageClass),
			URL:          c.publicObjectURL(fullKey),
		}
		items = append(items, item)
	}
	return GalleryList{Objects: items, Prefix: prefix, NextToken: ptrString(result.NextContinuationToken)}, nil
}

func (c *Client) DeleteObject(ctx context.Context, key string) error {
	fullKey := c.fullKey(key)
	_, err := c.oss.DeleteObject(ctx, &oss.DeleteObjectRequest{Bucket: oss.Ptr(c.bucket), Key: oss.Ptr(fullKey)})
	return err
}

func (c *Client) ObjectURL(key string) string {
	return c.publicObjectURL(c.fullKey(key))
}

func (c *Client) fullKey(key string) string {
	key = strings.Trim(strings.ReplaceAll(key, "\\", "/"), "/")
	if c.prefix == "" {
		return key
	}
	if key == "" {
		return c.prefix
	}
	if key == c.prefix || strings.HasPrefix(key, c.prefix+"/") {
		return key
	}
	return c.prefix + "/" + key
}

func (c *Client) relativeKey(fullKey string) string {
	fullKey = strings.TrimLeft(strings.ReplaceAll(fullKey, "\\", "/"), "/")
	if c.prefix == "" {
		return fullKey
	}
	return strings.TrimPrefix(strings.TrimPrefix(fullKey, c.prefix), "/")
}

func (c *Client) publicObjectURL(fullKey string) string {
	if c.publicBaseURL != "" {
		return strings.TrimRight(c.publicBaseURL, "/") + "/" + escapeURLPath(fullKey)
	}
	return ""
}

func uniqueFilename(filename string) string {
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filepath.Base(filename), ext)
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, name)
	name = strings.Trim(name, "-")
	if name == "" {
		name = "asset"
	}
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d-%s%s", time.Now().UnixNano(), name, ext)
	}
	return fmt.Sprintf("%s-%s%s", hex.EncodeToString(buf), name, ext)
}

func normalizeUploadContentType(filename, contentType string) string {
	contentType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	if contentType != "" && contentType != "application/octet-stream" {
		return contentType
	}
	if inferred := contentTypeFromFilename(filename); inferred != "" {
		return inferred
	}
	return "application/octet-stream"
}

func contentTypeFromFilename(filename string) string {
	if value := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))); value != "" {
		return strings.Split(value, ";")[0]
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".m4v":
		return "video/mp4"
	case ".m4a":
		return "audio/mp4"
	default:
		return ""
	}
}

func isImageUpload(filename, contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if strings.HasPrefix(contentType, "image/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return true
	default:
		return false
	}
}

func isVideoUpload(filename, contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if strings.HasPrefix(contentType, "video/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".mp4", ".m4v", ".webm", ".mov":
		return true
	default:
		return false
	}
}

func ossImageResizeURL(raw string, maxWidth int) string {
	if raw == "" {
		return ""
	}
	return appendOSSProcess(raw, fmt.Sprintf("image/resize,w_%d", maxWidth))
}

func ossVideoSnapshotURL(raw string, ms int) string {
	if raw == "" {
		return ""
	}
	return appendOSSProcess(raw, fmt.Sprintf("video/snapshot,t_%d,f_jpg,w_480", ms))
}

func appendOSSProcess(raw, process string) string {
	separator := "?"
	if strings.Contains(raw, "?") {
		separator = "&"
	}
	return raw + separator + "x-oss-process=" + url.QueryEscape(process)
}

func escapeURLPath(value string) string {
	parts := strings.Split(value, "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func displayName(key string, prefix bool) string {
	key = strings.Trim(key, "/")
	if key == "" {
		return ""
	}
	if prefix {
		return path.Base(key)
	}
	return path.Base(strings.TrimSuffix(key, "/"))
}

func ptrString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
