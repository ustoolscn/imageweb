package worker

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"image-web/backend/internal/db"
	"image-web/backend/internal/generator"
	"image-web/backend/internal/imagehost"
	"image-web/backend/internal/model"
)

type Worker struct {
	Store     *db.Store
	Generator *generator.Client
	ImageHost *imagehost.Client
	running   int64
}

func (w *Worker) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.dispatch(ctx)
			}
		}
	}()
}

func (w *Worker) dispatch(ctx context.Context) {
	if err := w.Store.ResetStaleRunningTasks(ctx, 30*time.Minute); err != nil {
		fmt.Println("worker reset stale tasks:", err)
		return
	}
	config, err := w.Store.SiteConfig(ctx)
	if err != nil {
		fmt.Println("worker get config:", err)
		return
	}
	concurrency := config.WorkerConcurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	for atomic.LoadInt64(&w.running) < int64(concurrency) {
		atomic.AddInt64(&w.running, 1)
		go func() {
			defer atomic.AddInt64(&w.running, -1)
			w.processNext(ctx)
		}()
	}
}

func (w *Worker) processNext(ctx context.Context) {
	task, err := w.Store.NextPendingTask(ctx)
	if err != nil {
		if !db.IsNotFound(err) && err.Error() != "driver: bad connection" {
			fmt.Println("worker get task:", err)
		}
		return
	}
	started := time.Now()
	finalPrompt := task.Prompt
	requestHeaders := ""
	requestJSON := ""
	responseHeaders := ""
	responseJSON := ""

	result, err := w.Generator.Generate(ctx, task, finalPrompt)
	requestHeaders = result.RequestHeaders
	requestJSON = result.RequestJSON
	responseHeaders = result.ResponseHeaders
	responseJSON = result.ResponseJSON
	logUpstreamExchange(task.ID, result, requestHeaders, requestJSON, responseHeaders, responseJSON)
	for _, path := range result.Files {
		defer os.Remove(path)
	}
	if err != nil {
		_ = w.Store.FailTask(ctx, task.ID, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, err.Error(), time.Since(started).Milliseconds())
		return
	}

	fmt.Println("worker generated files task:", task.ID, len(result.Files))
	uploaded := []model.UploadedImage{}
	for _, path := range result.Files {
		image, err := w.ImageHost.UploadFile(ctx, path)
		if err != nil {
			fmt.Println("worker upload result image:", task.ID, err)
			_ = w.Store.FailTask(ctx, task.ID, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, err.Error(), time.Since(started).Milliseconds())
			return
		}
		fmt.Println("worker uploaded result image:", task.ID, image.URL)
		uploaded = append(uploaded, image)
	}
	if err := w.Store.CompleteTask(ctx, task.ID, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, uploaded, time.Since(started).Milliseconds()); err != nil {
		fmt.Println("worker complete task:", err)
		_ = w.Store.FailTask(ctx, task.ID, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, "保存生成结果失败："+err.Error(), time.Since(started).Milliseconds())
		return
	}
	fmt.Println("worker completed task:", task.ID, len(uploaded))
}

func logUpstreamExchange(taskID string, result generator.GenerateResult, requestHeaders, requestJSON, responseHeaders, responseJSON string) {
	fmt.Println("upstream image exchange task:", taskID)
	fmt.Println("upstream image exchange logged_at:", logTimestamp(time.Now()))
	fmt.Println("upstream image request started_at:", logTimestamp(result.RequestStartedAt))
	fmt.Println("upstream image request wrote_at:", logTimestamp(result.RequestWroteAt))
	fmt.Println("upstream image response first_byte_at:", logTimestamp(result.ResponseFirstByteAt))
	fmt.Println("upstream image response headers_received_at:", logTimestamp(result.ResponseHeadersReceivedAt))
	fmt.Println("upstream image response body_read_at:", logTimestamp(result.ResponseBodyReadAt))
	fmt.Println("upstream image request write_latency_ms:", elapsedMillis(result.RequestStartedAt, result.RequestWroteAt))
	fmt.Println("upstream image response first_byte_latency_ms:", elapsedMillis(result.RequestWroteAt, result.ResponseFirstByteAt))
	fmt.Println("upstream image response headers_latency_ms:", elapsedMillis(result.RequestWroteAt, result.ResponseHeadersReceivedAt))
	fmt.Println("upstream image response body_read_latency_ms:", elapsedMillis(result.ResponseHeadersReceivedAt, result.ResponseBodyReadAt))
	fmt.Println("upstream image exchange total_latency_ms:", elapsedMillis(result.RequestStartedAt, result.ResponseBodyReadAt))
	fmt.Println("upstream image response status:", result.UpstreamStatus)
	fmt.Println("upstream image response status_code:", result.UpstreamStatusCode)
	fmt.Println("upstream image response server_date:", defaultLogValue(result.UpstreamServerDate))
	fmt.Println("upstream image request headers:", requestHeaders)
	fmt.Println("upstream image request body:", requestJSON)
	fmt.Println("upstream image request body size:", len(requestJSON))
	fmt.Println("upstream image response headers:", responseHeaders)
	fmt.Println("upstream image response body:", responseJSON)
	fmt.Println("upstream image response body size:", len(responseJSON))
}

func logTimestamp(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Format(time.RFC3339Nano)
}

func elapsedMillis(start, end time.Time) string {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return "-"
	}
	return fmt.Sprint(end.Sub(start).Milliseconds())
}

func defaultLogValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
