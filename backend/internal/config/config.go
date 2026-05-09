package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Port                     string
	DataDir                  string
	DatabaseDSN              string
	ImageHostProvider        string
	ImageHostUploadURL       string
	ImageHostAuthHeader      string
	ImageHostAuthValue       string
	ImageHostFieldName       string
	ImageHostResponseURLPath string
	ImageHostLocalDir        string
	ImageHostPublicBaseURL   string
	StaticDir                string
}

func Load() Config {
	loadDotEnv()
	dataDir := filepath.Join(os.TempDir(), "image-web")
	return Config{
		Port:                     getEnv("PORT", "8080"),
		DataDir:                  dataDir,
		DatabaseDSN:              getEnv("DATABASE_DSN", "postgres://image_web:image_web@localhost:5432/image_web?sslmode=disable"),
		ImageHostProvider:        getEnv("IMAGE_HOST_PROVIDER", "http-json"),
		ImageHostUploadURL:       getEnv("IMAGE_HOST_UPLOAD_URL", "https://2bad.lujilujilujilujiluji.com/"),
		ImageHostAuthHeader:      getEnv("IMAGE_HOST_AUTH_HEADER", "Authorization"),
		ImageHostAuthValue:       getEnv("IMAGE_HOST_AUTH_VALUE", "Bearer cooper"),
		ImageHostFieldName:       getEnv("IMAGE_HOST_FIELD_NAME", "file"),
		ImageHostResponseURLPath: getEnv("IMAGE_HOST_RESPONSE_URL_PATH", "url"),
		ImageHostLocalDir:        getEnv("IMAGE_HOST_LOCAL_DIR", filepath.Join(dataDir, "uploads")),
		ImageHostPublicBaseURL:   getEnv("IMAGE_HOST_PUBLIC_BASE_URL", ""),
		StaticDir:                "./static",
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
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
