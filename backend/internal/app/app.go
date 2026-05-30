package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

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

var requestSeq uint64

func New(ctx context.Context, cfg config.Config) (*App, error) {
	fmt.Println("app init: opening database")
	store, err := db.Open(cfg.DatabaseDSN, cfg.AppCredentialKey)
	if err != nil {
		return nil, err
	}
	fmt.Println("app init: database ready")
	fmt.Println("app init: cleaning stale image tasks")
	cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if err := store.FailStaleImageTasks(cleanupCtx, 10*time.Minute); err != nil {
		fmt.Println("startup stale image cleanup skipped:", err)
	}
	cancel()
	fmt.Println("app init: stale cleanup done")
	gen := generator.New(filepath.Join(cfg.DataDir, "tmp"))
	host := imagehost.New(cfg)
	h := &handler.Handler{Store: store, Generator: gen, ImageHost: host}
	mux := http.NewServeMux()
	h.Register(mux)
	mux.HandleFunc("/", staticHandler(cfg.StaticDir))

	w := &worker.Worker{Store: store, Generator: gen, ImageHost: host}
	w.Start(ctx)
	fmt.Println("app init: worker started")

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: requestLogger(securityHeaders(mux)),
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

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(data)
	r.bytes += n
	return n, err
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := fmt.Sprintf("%d-%d", time.Now().UnixNano(), atomic.AddUint64(&requestSeq, 1))
		ctx := db.WithTraceID(r.Context(), requestID)
		recorder := &statusRecorder{ResponseWriter: w}
		started := time.Now()
		log.Printf("[http] trace=%s begin method=%s path=%s remote=%s", requestID, r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(recorder, r.WithContext(ctx))
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		log.Printf("[http] trace=%s done method=%s path=%s status=%d bytes=%d elapsed=%s", requestID, r.Method, r.URL.Path, status, recorder.bytes, time.Since(started))
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
