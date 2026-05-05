package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"image-web/backend/internal/config"
	"image-web/backend/internal/db"
	"image-web/backend/internal/generator"
	"image-web/backend/internal/handler"
	"image-web/backend/internal/imagehost"
	"image-web/backend/internal/worker"
)

type App struct {
	Config config.Config
	Store  *db.Store
	Server *http.Server
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}
	store, err := db.Open(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	gen := generator.New(filepath.Join(cfg.DataDir, "tmp"))
	host := imagehost.New(cfg.ScdnUploadURL)
	h := &handler.Handler{Store: store, Generator: gen}
	mux := http.NewServeMux()
	h.Register(mux)
	mux.HandleFunc("/", staticHandler(cfg.StaticDir))

	w := &worker.Worker{Store: store, Generator: gen, ImageHost: host}
	w.Start(ctx)

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: securityHeaders(mux),
	}
	return &App{Config: cfg, Store: store, Server: server}, nil
}

func (a *App) Close() error {
	return a.Store.Close()
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "frame-ancestors *")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func staticHandler(staticDir string) http.HandlerFunc {
	files := http.FileServer(http.Dir(staticDir))
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		path := filepath.Join(staticDir, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
	}
}
