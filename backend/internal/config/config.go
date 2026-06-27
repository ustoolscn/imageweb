package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Port                 string
	DataDir              string
	DatabaseDSN          string
	AppCredentialKey     string
	OSSRegion            string
	OSSBucket            string
	OSSEndpoint          string
	OSSPublicBaseURL     string
	OSSPrefix            string
	OSSAccessKeyID       string
	OSSAccessKeySecret   string
	GalleryAdminUsername string
	GalleryAdminPassword string
	GallerySessionSecret string
	StaticDir            string
}

func Load() Config {
	loadDotEnv()
	dataDir := filepath.Join(os.TempDir(), "image-web")
	return Config{
		Port:                 getEnv("PORT", "8080"),
		DataDir:              dataDir,
		DatabaseDSN:          getEnv("DATABASE_DSN", "image_web:image_web@tcp(localhost:3306)/image_web?parseTime=true&charset=utf8mb4&loc=UTC"),
		AppCredentialKey:     getEnv("APP_CREDENTIAL_KEY", ""),
		OSSRegion:            normalizeOSSRegion(getEnv("OSS_REGION", "")),
		OSSBucket:            getEnv("OSS_BUCKET", ""),
		OSSEndpoint:          getEnv("OSS_ENDPOINT", ""),
		OSSPublicBaseURL:     strings.TrimRight(getEnv("OSS_PUBLIC_BASE_URL", ""), "/"),
		OSSPrefix:            strings.Trim(getEnv("OSS_PREFIX", "imageweb"), "/"),
		OSSAccessKeyID:       getEnv("OSS_ACCESS_KEY_ID", ""),
		OSSAccessKeySecret:   getEnv("OSS_ACCESS_KEY_SECRET", ""),
		GalleryAdminUsername: getEnv("GALLERY_ADMIN_USERNAME", "admin"),
		GalleryAdminPassword: getEnv("GALLERY_ADMIN_PASSWORD", ""),
		GallerySessionSecret: getEnv("GALLERY_SESSION_SECRET", getEnv("APP_CREDENTIAL_KEY", "")),
		StaticDir:            "./static",
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func normalizeOSSRegion(value string) string {
	value = strings.TrimSpace(value)
	return strings.TrimPrefix(value, "oss-")
}

func loadDotEnv() {
	for _, path := range dotenvCandidates() {
		if loadDotEnvFile(path) == nil {
			return
		}
	}
}

func dotenvCandidates() []string {
	candidates := []string{".env"}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, ".env"),
			filepath.Join(wd, "..", ".env"),
		)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, ".env"),
			filepath.Join(dir, "..", ".env"),
		)
	}
	return candidates
}

func loadDotEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key == "" {
			continue
		}
		_ = os.Setenv(key, value)
	}
	return scanner.Err()
}
