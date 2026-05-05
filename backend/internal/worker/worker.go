package worker

import (
	"context"
	"fmt"
	"os"
	"strings"
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
}

func (w *Worker) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.processNext(ctx)
			}
		}
	}()
}

func (w *Worker) processNext(ctx context.Context) {
	task, err := w.Store.NextPendingTask(ctx)
	if err != nil {
		if !db.IsNotFound(err) {
			fmt.Println("worker get task:", err)
		}
		return
	}
	started := time.Now()
	finalPrompt := buildFinalPrompt(task)
	requestHeaders := ""
	requestJSON := ""
	responseHeaders := ""
	responseJSON := ""

	result, err := w.Generator.Generate(ctx, task, finalPrompt)
	requestHeaders = result.RequestHeaders
	requestJSON = result.RequestJSON
	responseHeaders = result.ResponseHeaders
	responseJSON = result.ResponseJSON
	for _, path := range result.Files {
		defer os.Remove(path)
	}
	if err != nil {
		_ = w.Store.FailTask(ctx, task.ID, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, err.Error(), time.Since(started).Milliseconds())
		return
	}

	uploaded := []model.UploadedImage{}
	for _, path := range result.Files {
		image, err := w.ImageHost.UploadFile(ctx, path)
		if err != nil {
			_ = w.Store.FailTask(ctx, task.ID, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, err.Error(), time.Since(started).Milliseconds())
			return
		}
		uploaded = append(uploaded, image)
	}
	if err := w.Store.CompleteTask(ctx, task.ID, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, uploaded, time.Since(started).Milliseconds()); err != nil {
		fmt.Println("worker complete task:", err)
	}
}

func buildFinalPrompt(task *model.Task) string {
	return strings.TrimSpace(task.Prompt)
}
