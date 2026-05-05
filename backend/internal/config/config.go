package config

import "os"

type Config struct {
	Port          string
	DataDir       string
	DatabasePath  string
	ScdnUploadURL string
	StaticDir     string
}

func Load() Config {
	dataDir := getEnv("DATA_DIR", "./data")
	return Config{
		Port:          getEnv("PORT", "8080"),
		DataDir:       dataDir,
		DatabasePath:  getEnv("DATABASE_PATH", dataDir+"/app.db"),
		ScdnUploadURL: getEnv("SCDN_UPLOAD_URL", "https://img.scdn.io/api/v1.php"),
		StaticDir:     getEnv("STATIC_DIR", "./static"),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
