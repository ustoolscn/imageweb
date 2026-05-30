package worker

import (
	"context"
	"errors"
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
	Store            *db.Store
	Generator        *generator.Client
	ImageHost        *imagehost.Client
	running          int64
	claiming         int64
	lastNoPending    int64
	lastStaleCleanup int64
}

const imageTaskTimeout = 10 * time.Minute
const staleCleanupInterval = time.Minute
const staleCleanupDBTimeout = 5 * time.Second
const idleTaskPollInterval = 3 * time.Second

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
				w.pollVideos(ctx)
			}
		}
	}()
}

func (w *Worker) dispatch(ctx context.Context) {
	w.cleanupStaleImageTasks(ctx)
	config, err := w.Store.SiteConfig(ctx)
	if err != nil {
		fmt.Println("worker get config:", err)
		return
	}
	concurrency := config.WorkerConcurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	running := atomic.LoadInt64(&w.running)
	if running > 0 {
		fmt.Println("worker dispatch tick:", "running=", running, "concurrency=", concurrency)
	}
	if running >= int64(concurrency) {
		return
	}
	if lastNoPending := atomic.LoadInt64(&w.lastNoPending); lastNoPending > 0 && time.Since(time.Unix(lastNoPending, 0)) < idleTaskPollInterval {
		return
	}
	if !atomic.CompareAndSwapInt64(&w.claiming, 0, 1) {
		return
	}
	go w.claimAndProcessNext(ctx)
}

func (w *Worker) cleanupStaleImageTasks(ctx context.Context) {
	now := time.Now()
	last := time.Unix(atomic.LoadInt64(&w.lastStaleCleanup), 0)
	if !last.IsZero() && now.Sub(last) < staleCleanupInterval {
		return
	}
	if !atomic.CompareAndSwapInt64(&w.lastStaleCleanup, last.Unix(), now.Unix()) {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, staleCleanupDBTimeout)
	defer cancel()
	if err := w.Store.FailStaleImageTasks(cleanupCtx, imageTaskTimeout); err != nil {
		fmt.Println("worker fail stale image tasks skipped:", err)
	}
}

func (w *Worker) claimAndProcessNext(ctx context.Context) {
	defer atomic.StoreInt64(&w.claiming, 0)
	task, err := w.Store.NextPendingTask(ctx)
	if err != nil {
		if !db.IsNotFound(err) && err.Error() != "driver: bad connection" {
			fmt.Println("worker get task:", err)
		} else {
			atomic.StoreInt64(&w.lastNoPending, time.Now().Unix())
		}
		return
	}
	atomic.StoreInt64(&w.lastNoPending, 0)
	atomic.AddInt64(&w.running, 1)
	go func() {
		defer atomic.AddInt64(&w.running, -1)
		w.processTask(ctx, task)
	}()
}

func (w *Worker) processTask(ctx context.Context, task *model.Task) {
	started := time.Now()
	finalPrompt := task.Prompt
	requestHeaders := ""
	requestJSON := ""
	responseHeaders := ""
	responseJSON := ""

	fmt.Println("worker task dispatch:", "id=", task.ID, "task_type=", task.TaskType, "status=", task.Status, "model=", task.Model, "is_video=", isVideoTask(task), "image_size=", task.Size, "video_ratio=", task.VideoRatio, "video_width=", task.VideoWidth, "video_height=", task.VideoHeight, "video_duration=", task.VideoDuration, "ref_images=", len(task.ReferenceImages), "ref_videos=", len(task.ReferenceVideos), "ref_audios=", len(task.ReferenceAudios))
	if isVideoTask(task) {
		fmt.Println("worker route decision:", "id=", task.ID, "route=", "video", "reason_task_type=", task.TaskType)
		w.submitVideo(ctx, task, started)
		return
	}

	fmt.Println("worker route decision:", "id=", task.ID, "route=", "image", "reason_task_type=", task.TaskType)
	imageCtx, cancel := context.WithTimeout(ctx, imageTaskTimeout)
	defer cancel()
	result, err := w.Generator.Generate(imageCtx, task, finalPrompt)
	requestHeaders = result.RequestHeaders
	requestJSON = result.RequestJSON
	responseHeaders = result.ResponseHeaders
	responseJSON = result.ResponseJSON
	logUpstreamExchange(task.ID, result, requestHeaders, requestJSON, responseHeaders, responseJSON)
	for _, path := range result.Files {
		defer os.Remove(path)
	}
	if err != nil {
		fmt.Println("worker image generate failed:", "id=", task.ID, "task_type=", task.TaskType, "error=", err)
		_ = w.Store.FailTask(ctx, task.ID, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, imageTaskErrorMessage(err, imageCtx), time.Since(started).Milliseconds())
		return
	}

	fmt.Println("worker generated files task:", task.ID, len(result.Files))
	uploaded := []model.UploadedImage{}
	for _, path := range result.Files {
		image, err := w.ImageHost.UploadFile(imageCtx, path)
		if err != nil {
			fmt.Println("worker upload result image:", task.ID, err)
			_ = w.Store.FailTask(ctx, task.ID, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, imageTaskErrorMessage(err, imageCtx), time.Since(started).Milliseconds())
			return
		}
		fmt.Println("worker uploaded result image:", task.ID, image.URL)
		uploaded = append(uploaded, image)
	}
	if err := imageCtx.Err(); err != nil {
		_ = w.Store.FailTask(ctx, task.ID, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, imageTaskErrorMessage(err, imageCtx), time.Since(started).Milliseconds())
		return
	}
	if err := w.Store.CompleteTask(ctx, task.ID, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, uploaded, time.Since(started).Milliseconds()); err != nil {
		fmt.Println("worker complete task:", err)
		_ = w.Store.FailTask(ctx, task.ID, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, "保存生成结果失败："+err.Error(), time.Since(started).Milliseconds())
		return
	}
	fmt.Println("worker completed task:", task.ID, len(uploaded))
}

func imageTaskErrorMessage(err error, taskCtx context.Context) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(taskCtx.Err(), context.DeadlineExceeded) {
		return fmt.Sprintf("图片生成超过 %d 分钟，已自动断开并标记失败；上游可能仍在处理，请检查上游记录后手动重试", int(imageTaskTimeout.Minutes()))
	}
	return err.Error()
}

func (w *Worker) submitVideo(ctx context.Context, task *model.Task, started time.Time) {
	finalPrompt := task.Prompt
	fmt.Println("worker submit_video begin:", "id=", task.ID, "task_type=", task.TaskType, "model=", task.Model, "video_ratio=", task.VideoRatio, "video_width=", task.VideoWidth, "video_height=", task.VideoHeight, "video_duration=", task.VideoDuration)
	result, err := w.Generator.SubmitVideo(ctx, task, finalPrompt)
	if err != nil {
		fmt.Println("worker submit_video failed:", "id=", task.ID, "task_type=", task.TaskType, "error=", err)
		_ = w.Store.FailTask(ctx, task.ID, finalPrompt, result.RequestHeaders, result.RequestJSON, result.ResponseHeaders, result.ResponseJSON, err.Error(), time.Since(started).Milliseconds())
		return
	}
	fmt.Println("worker submit_video upstream_result:", "id=", task.ID, "upstream_task_id=", result.UpstreamTaskID, "upstream_status=", result.Status, "progress=", result.Progress)
	if err := w.Store.MarkVideoSubmitted(ctx, task.ID, result.UpstreamTaskID, result.Status, result.Progress, result.RequestHeaders, result.RequestJSON, result.ResponseHeaders, result.ResponseJSON); err != nil {
		fmt.Println("worker mark video submitted:", err)
		_ = w.Store.FailTask(ctx, task.ID, finalPrompt, result.RequestHeaders, result.RequestJSON, result.ResponseHeaders, result.ResponseJSON, "保存视频提交结果失败: "+err.Error(), time.Since(started).Milliseconds())
		return
	}
	fmt.Println("worker submitted video task:", task.ID, result.UpstreamTaskID)
}

func (w *Worker) pollVideos(ctx context.Context) {
	tasks, err := w.Store.VideoTasksToPoll(ctx, 8)
	if err != nil {
		fmt.Println("worker list video polls:", err)
		return
	}
	if len(tasks) == 0 {
		return
	}
	fmt.Println("worker poll_videos tasks:", "count=", len(tasks))
	for index := range tasks {
		task := tasks[index]
		w.pollVideo(ctx, &task)
	}
}

func (w *Worker) pollVideo(ctx context.Context, task *model.Task) {
	fmt.Println("worker poll_video begin:", "id=", task.ID, "task_type=", task.TaskType, "upstream_task_id=", task.UpstreamTaskID, "upstream_status=", task.UpstreamStatus, "progress=", task.UpstreamProgress, "poll_count=", task.PollCount)
	result, err := w.Generator.PollVideo(ctx, task)
	if err != nil {
		nextPoll := time.Now().UTC().Add(videoPollDelay(task.PollCount + 1))
		_ = w.Store.UpdateVideoPoll(ctx, task.ID, task.UpstreamStatus, task.UpstreamProgress, result.ResponseHeaders, result.ResponseJSON, nextPoll)
		fmt.Println("worker poll video:", task.ID, err)
		return
	}
	status := result.Status
	progress := result.Progress
	if progress <= 0 {
		progress = task.UpstreamProgress
	}
	if generator.IsVideoFailed(status) {
		message := result.FailReason
		if message == "" {
			message = "视频生成失败"
		}
		_ = w.Store.FailTask(ctx, task.ID, task.Prompt, task.RequestHeaders, task.RequestJSON, result.ResponseHeaders, result.ResponseJSON, message, time.Since(defaultTime(task.StartedAt, task.CreatedAt)).Milliseconds())
		return
	}
	if !generator.IsVideoSucceeded(status) {
		nextPoll := time.Now().UTC().Add(videoPollDelay(task.PollCount + 1))
		_ = w.Store.UpdateVideoPoll(ctx, task.ID, status, progress, result.ResponseHeaders, result.ResponseJSON, nextPoll)
		return
	}
	videoURL := result.VideoURL
	fmt.Println("worker video succeeded:", "id=", task.ID, "upstream_video_url=", videoURL, "duration=", result.Duration, "status=", result.Status)
	if videoURL == "" {
		_ = w.Store.FailTask(ctx, task.ID, task.Prompt, task.RequestHeaders, task.RequestJSON, result.ResponseHeaders, result.ResponseJSON, "视频任务成功但没有返回 video_url", time.Since(defaultTime(task.StartedAt, task.CreatedAt)).Milliseconds())
		return
	}
	path, err := w.Generator.DownloadVideo(ctx, task.ID, videoURL)
	if err != nil {
		_ = w.Store.FailTask(ctx, task.ID, task.Prompt, task.RequestHeaders, task.RequestJSON, result.ResponseHeaders, result.ResponseJSON, err.Error(), time.Since(defaultTime(task.StartedAt, task.CreatedAt)).Milliseconds())
		return
	}
	defer os.Remove(path)
	uploaded, err := w.ImageHost.UploadFile(ctx, path)
	if err != nil {
		_ = w.Store.FailTask(ctx, task.ID, task.Prompt, task.RequestHeaders, task.RequestJSON, result.ResponseHeaders, result.ResponseJSON, err.Error(), time.Since(defaultTime(task.StartedAt, task.CreatedAt)).Milliseconds())
		return
	}
	video := model.MediaAsset{
		Type:          "video",
		URL:           uploaded.URL,
		ThumbnailURL:  uploaded.ThumbnailURL,
		FirstFrameURL: uploaded.FirstFrameURL,
		LastFrameURL:  uploaded.LastFrameURL,
		Filename:      uploaded.Filename,
		Duration:      result.Duration,
		Width:         task.VideoWidth,
		Height:        task.VideoHeight,
	}
	if video.FirstFrameURL == "" || video.LastFrameURL == "" {
		if frames, err := w.ImageHost.ExtractVideoFramesFromFile(ctx, path, uploaded.Filename); err == nil {
			video.ThumbnailURL = frames.ThumbnailURL
			video.FirstFrameURL = frames.FirstFrameURL
			video.LastFrameURL = frames.LastFrameURL
		} else {
			fmt.Println("worker video frame extract skipped:", "id=", task.ID, "error=", err)
		}
	}
	fmt.Println("worker video uploaded:", "id=", task.ID, "uploaded_url=", uploaded.URL, "filename=", uploaded.Filename)
	if err := w.Store.CompleteVideoTask(ctx, task.ID, task.Prompt, result.ResponseHeaders, result.ResponseJSON, []model.MediaAsset{video}, time.Since(defaultTime(task.StartedAt, task.CreatedAt)).Milliseconds()); err != nil {
		fmt.Println("worker complete video:", err)
	}
}

func videoPollDelay(pollCount int) time.Duration {
	if pollCount < 12 {
		return 5 * time.Second
	}
	if pollCount < 28 {
		return 15 * time.Second
	}
	return 45 * time.Second
}

func defaultTime(value *time.Time, fallback time.Time) time.Time {
	if value != nil {
		return *value
	}
	return fallback
}

func isVideoTask(task *model.Task) bool {
	return task.TaskType == model.TaskTypeVideoGeneration
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
