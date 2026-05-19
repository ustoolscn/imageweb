package handler

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"image-web/backend/internal/db"
	"image-web/backend/internal/generator"
	"image-web/backend/internal/imagehost"
	"image-web/backend/internal/model"

	"github.com/google/uuid"
	_ "golang.org/x/image/webp"
)

type Handler struct {
	Store     *db.Store
	Generator *generator.Client
	ImageHost *imagehost.Client
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/health", h.health)
	mux.HandleFunc("/api/site-brand", h.siteBrand)
	mux.HandleFunc("/api/models", h.models)
	mux.HandleFunc("/api/llm", h.llm)
	mux.HandleFunc("/api/upload", h.upload)
	mux.HandleFunc("/api/mask-preview", h.maskPreview)
	mux.HandleFunc("/api/plaza", h.plaza)
	mux.HandleFunc("/api/plaza/", h.plazaByID)
	mux.HandleFunc("/api/tasks", h.tasks)
	mux.HandleFunc("/api/tasks/updates", h.taskUpdates)
	mux.HandleFunc("/api/tasks/", h.taskByID)
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) siteBrand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	config, err := h.Store.SiteConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	brand := model.SiteBrandResponse{Title: defaultString(config.SiteTitle, "图片生成工作台"), Icon: defaultString(config.SiteIcon, "AI"), Allow2K: true, Allow4K: true}
	if entry, ok := h.matchBaseURL(config, r.URL.Query().Get("baseurl")); ok {
		if entry.Title != "" {
			brand.Title = entry.Title
		}
		if entry.Icon != "" {
			brand.Icon = entry.Icon
		}
		if entry.Allow2K != nil {
			brand.Allow2K = *entry.Allow2K
		}
		if entry.Allow4K != nil {
			brand.Allow4K = *entry.Allow4K
		}
	}
	writeJSON(w, http.StatusOK, brand)
}

func (h *Handler) models(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req model.ModelsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.APIKey == "" || req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "缺少 baseurl 或 apikey")
		return
	}
	if !h.allowBaseURL(w, r, req.BaseURL) {
		return
	}
	data, err := h.Generator.FetchModels(r.Context(), req.BaseURL, req.APIKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *Handler) llm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req model.LLMRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.APIKey == "" || req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "缺少 baseurl 或 apikey")
		return
	}
	if strings.TrimSpace(req.Prompt) == "" && len(req.ReferenceImages) == 0 && len(req.ReferenceVideos) == 0 && len(req.ReferenceAudios) == 0 {
		writeError(w, http.StatusBadRequest, "LLM 请求缺少输入参数")
		return
	}
	if !h.allowBaseURL(w, r, req.BaseURL) {
		return
	}
	text, err := h.Generator.GenerateText(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}

func (h *Handler) tasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listTasks(w, r)
	case http.MethodPost:
		h.createTask(w, r)
	default:
		methodNotAllowed(w)
	}
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if h.ImageHost == nil {
		writeError(w, http.StatusInternalServerError, "图床未配置")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 512<<20)
	if err := r.ParseMultipartForm(512 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "图片上传请求无效")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "缺少图片文件")
		return
	}
	defer file.Close()
	image, err := h.ImageHost.UploadReader(r.Context(), header.Filename, file)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "url": image.URL, "data": image})
}

func (h *Handler) maskPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	maskURL := r.URL.Query().Get("url")
	if maskURL == "" {
		writeError(w, http.StatusBadRequest, "缺少蒙板地址")
		return
	}
	parsed, err := url.Parse(maskURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		writeError(w, http.StatusBadRequest, "蒙板地址无效")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, maskURL, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("蒙板下载失败：HTTP %d", resp.StatusCode))
		return
	}
	mask, _, err := image.Decode(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		writeError(w, http.StatusBadGateway, "蒙板图片解析失败")
		return
	}
	bounds := mask.Bounds()
	preview := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := mask.At(x, y).RGBA()
			if alpha < 0xffff {
				preview.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 184})
			}
		}
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_ = png.Encode(w, preview)
}

func (h *Handler) listTasks(w http.ResponseWriter, r *http.Request) {
	apiKey := r.URL.Query().Get("apikey")
	baseURL := r.URL.Query().Get("baseurl")
	if apiKey == "" || baseURL == "" {
		writeError(w, http.StatusBadRequest, "缺少 baseurl 或 apikey")
		return
	}
	if !h.allowBaseURL(w, r, baseURL) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	tasks, total, err := h.Store.ListTasks(r.Context(), apiKey, baseURL, r.URL.Query().Get("status"), r.URL.Query().Get("q"), r.URL.Query().Get("before_created_at"), r.URL.Query().Get("before_id"), r.URL.Query().Get("favorite") == "1", limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hasMore := false
	if limit <= 0 || limit > 60 {
		limit = 30
	}
	if len(tasks) > limit {
		hasMore = true
		tasks = tasks[:limit]
	}
	nextBeforeCreatedAt := ""
	nextBeforeID := ""
	if hasMore && len(tasks) > 0 {
		last := tasks[len(tasks)-1]
		nextBeforeCreatedAt = last.CreatedAt.Format(time.RFC3339Nano)
		nextBeforeID = last.ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": tasks, "has_more": hasMore, "next_before_created_at": nextBeforeCreatedAt, "next_before_id": nextBeforeID, "total": total})
}

func (h *Handler) createTask(w http.ResponseWriter, r *http.Request) {
	var req model.CreateTaskRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.APIKey == "" || req.BaseURL == "" || strings.TrimSpace(req.Prompt) == "" {
		writeError(w, http.StatusBadRequest, "缺少必要参数")
		return
	}
	if !h.allowBaseURL(w, r, req.BaseURL) {
		return
	}
	taskType := inferCreateTaskType(req)
	fmt.Println("handler create_task request:",
		"node_kind=", req.NodeKind,
		"req_task_type=", req.TaskType,
		"inferred_task_type=", taskType,
		"baseurl=", maskBaseURL(req.BaseURL),
		"model=", req.Model,
		"image_size=", req.Size,
		"image_quality=", req.Quality,
		"image_output_format=", req.OutputFormat,
		"video_ratio=", req.VideoRatio,
		"video_width=", req.VideoWidth,
		"video_height=", req.VideoHeight,
		"video_duration=", req.VideoDuration,
		"generate_audio=", req.GenerateAudio,
		"watermark=", req.Watermark,
		"ref_images=", len(req.ReferenceImages),
		"ref_videos=", len(req.ReferenceVideos),
		"ref_audios=", len(req.ReferenceAudios),
	)
	task := &model.Task{
		ID:                uuid.NewString(),
		APIKey:            req.APIKey,
		BaseURL:           req.BaseURL,
		TaskType:          taskType,
		Status:            model.TaskPending,
		Prompt:            strings.TrimSpace(req.Prompt),
		Model:             defaultString(req.Model, "gpt-image-2"),
		Size:              defaultString(req.Size, "1024x1024"),
		Quality:           defaultString(req.Quality, "auto"),
		OutputFormat:      defaultString(req.OutputFormat, "png"),
		OutputCompression: req.OutputCompression,
		Background:        defaultString(req.Background, "auto"),
		Moderation:        defaultString(req.Moderation, "low"),
		InputFidelity:     defaultString(req.InputFidelity, "high"),
		N:                 req.N,
		Stream:            req.Stream,
		ReferenceImages:   req.ReferenceImages,
		ReferenceVideos:   cleanMediaAssets(req.ReferenceVideos, "video"),
		ReferenceAudios:   cleanMediaAssets(req.ReferenceAudios, "audio"),
		VideoRatio:        defaultString(req.VideoRatio, "16:9"),
		VideoWidth:        req.VideoWidth,
		VideoHeight:       req.VideoHeight,
		VideoDuration:     req.VideoDuration,
		GenerateAudio:     req.GenerateAudio,
		Watermark:         req.Watermark,
	}
	if task.TaskType == model.TaskTypeVideoGeneration {
		if task.Model == "" || isImageModel(task.Model) {
			task.Model = "doubao-seedance-2.0"
		}
		normalizeVideoTaskOptions(task)
	}
	if task.N <= 0 {
		task.N = 1
	}
	if task.TaskType == model.TaskTypeImageGeneration && task.Model == "gpt-image-2" {
		task.N = 1
	}
	if task.OutputCompression < 0 || task.OutputCompression > 100 {
		task.OutputCompression = 100
	}
	fmt.Println("handler create_task normalized:",
		"id=", task.ID,
		"task_type=", task.TaskType,
		"status=", task.Status,
		"baseurl=", maskBaseURL(task.BaseURL),
		"model=", task.Model,
		"image_size=", task.Size,
		"image_quality=", task.Quality,
		"image_output_format=", task.OutputFormat,
		"image_n=", task.N,
		"video_ratio=", task.VideoRatio,
		"video_width=", task.VideoWidth,
		"video_height=", task.VideoHeight,
		"video_duration=", task.VideoDuration,
		"generate_audio=", task.GenerateAudio,
		"watermark=", task.Watermark,
		"ref_images=", len(task.ReferenceImages),
		"ref_videos=", len(task.ReferenceVideos),
		"ref_audios=", len(task.ReferenceAudios),
	)
	if err := h.Store.CreateTask(r.Context(), task); err != nil {
		fmt.Println("handler create_task store_failed:", "id=", task.ID, "task_type=", task.TaskType, "error=", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	fmt.Println("handler create_task response:", "id=", task.ID, "task_type=", task.TaskType, "status=", task.Status)
	writeJSON(w, http.StatusCreated, task)
}

func (h *Handler) plaza(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	beforeLikeCount, _ := strconv.Atoi(r.URL.Query().Get("before_like_count"))
	items, total, err := h.Store.ListPlazaItems(r.Context(), r.URL.Query().Get("sort"), r.URL.Query().Get("q"), r.URL.Query().Get("before_created_at"), r.URL.Query().Get("before_id"), beforeLikeCount, r.URL.Query().Get("client_id"), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if limit <= 0 || limit > 60 {
		limit = 30
	}
	hasMore := false
	if len(items) > limit {
		hasMore = true
		items = items[:limit]
	}
	nextBeforeCreatedAt := ""
	nextBeforeID := ""
	nextBeforeLikeCount := 0
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextBeforeCreatedAt = last.CreatedAt.Format(time.RFC3339Nano)
		nextBeforeID = last.ID
		nextBeforeLikeCount = last.LikeCount
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "has_more": hasMore, "next_before_created_at": nextBeforeCreatedAt, "next_before_id": nextBeforeID, "next_before_like_count": nextBeforeLikeCount, "total": total})
}

func (h *Handler) plazaByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/plaza/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "like" {
		writeError(w, http.StatusNotFound, "广场作品不存在")
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req model.LikePlazaRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := h.Store.SetPlazaLike(r.Context(), parts[0], strings.TrimSpace(req.ClientID), req.Liked)
	if err != nil {
		if db.IsNotFound(err) {
			writeError(w, http.StatusNotFound, "广场作品不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) taskUpdates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	apiKey := r.URL.Query().Get("apikey")
	baseURL := r.URL.Query().Get("baseurl")
	if apiKey == "" || baseURL == "" {
		writeError(w, http.StatusBadRequest, "缺少 baseurl 或 apikey")
		return
	}
	if !h.allowBaseURL(w, r, baseURL) {
		return
	}
	ids := strings.Split(r.URL.Query().Get("ids"), ",")
	cleanIDs := []string{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			cleanIDs = append(cleanIDs, id)
		}
	}
	updates, err := h.Store.TaskUpdates(r.Context(), apiKey, baseURL, cleanIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": updates})
}

func (h *Handler) taskByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	id := parts[0]
	if len(parts) == 2 && parts[1] == "retry" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		h.retryTask(w, r, id)
		return
	}
	if len(parts) == 2 && parts[1] == "favorite" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		h.setFavorite(w, r, id)
		return
	}
	if len(parts) == 2 && parts[1] == "share" {
		switch r.Method {
		case http.MethodPost:
			h.shareTask(w, r, id)
		case http.MethodDelete:
			h.unshareTask(w, r, id)
		default:
			methodNotAllowed(w)
		}
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.getTask(w, r, id)
	case http.MethodDelete:
		h.deleteTask(w, r, id)
	default:
		methodNotAllowed(w)
	}
}

func (h *Handler) getTask(w http.ResponseWriter, r *http.Request, id string) {
	apiKey := r.URL.Query().Get("apikey")
	baseURL := r.URL.Query().Get("baseurl")
	if apiKey == "" || baseURL == "" {
		writeError(w, http.StatusBadRequest, "缺少 baseurl 或 apikey")
		return
	}
	if !h.allowBaseURL(w, r, baseURL) {
		return
	}
	task, err := h.Store.GetTask(r.Context(), id, apiKey, baseURL)
	if err != nil {
		if db.IsNotFound(err) {
			writeError(w, http.StatusNotFound, "任务不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *Handler) deleteTask(w http.ResponseWriter, r *http.Request, id string) {
	apiKey := r.URL.Query().Get("apikey")
	baseURL := r.URL.Query().Get("baseurl")
	if apiKey == "" || baseURL == "" {
		writeError(w, http.StatusBadRequest, "缺少 baseurl 或 apikey")
		return
	}
	if !h.allowBaseURL(w, r, baseURL) {
		return
	}
	if err := h.Store.DeleteTask(r.Context(), id, apiKey, baseURL); err != nil {
		if db.IsNotFound(err) {
			writeError(w, http.StatusNotFound, "任务不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) setFavorite(w http.ResponseWriter, r *http.Request, id string) {
	apiKey := r.URL.Query().Get("apikey")
	baseURL := r.URL.Query().Get("baseurl")
	if apiKey == "" || baseURL == "" {
		writeError(w, http.StatusBadRequest, "缺少 baseurl 或 apikey")
		return
	}
	if !h.allowBaseURL(w, r, baseURL) {
		return
	}
	var req struct {
		Favorite bool `json:"favorite"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.Store.SetFavorite(r.Context(), id, apiKey, baseURL, req.Favorite); err != nil {
		if db.IsNotFound(err) {
			writeError(w, http.StatusNotFound, "任务不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	task, err := h.Store.GetTask(r.Context(), id, apiKey, baseURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *Handler) shareTask(w http.ResponseWriter, r *http.Request, id string) {
	var req model.ShareTaskRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.APIKey == "" || req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "缺少 baseurl 或 apikey")
		return
	}
	if !h.allowBaseURL(w, r, req.BaseURL) {
		return
	}
	item, err := h.Store.ShareTaskToPlaza(r.Context(), id, req.APIKey, req.BaseURL)
	if err != nil {
		if db.IsNotFound(err) {
			writeError(w, http.StatusNotFound, "任务不存在")
			return
		}
		status := http.StatusInternalServerError
		if err.Error() == "只有成功任务可以分享到广场" {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) unshareTask(w http.ResponseWriter, r *http.Request, id string) {
	var req model.ShareTaskRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.APIKey == "" || req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "缺少 baseurl 或 apikey")
		return
	}
	if !h.allowBaseURL(w, r, req.BaseURL) {
		return
	}
	if err := h.Store.UnshareTaskFromPlaza(r.Context(), id, req.APIKey, req.BaseURL); err != nil {
		if db.IsNotFound(err) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) retryTask(w http.ResponseWriter, r *http.Request, id string) {
	apiKey := r.URL.Query().Get("apikey")
	baseURL := r.URL.Query().Get("baseurl")
	if apiKey == "" || baseURL == "" {
		writeError(w, http.StatusBadRequest, "缺少 baseurl 或 apikey")
		return
	}
	if !h.allowBaseURL(w, r, baseURL) {
		return
	}
	oldTask, err := h.Store.GetTask(r.Context(), id, apiKey, baseURL)
	if err != nil {
		if db.IsNotFound(err) {
			writeError(w, http.StatusNotFound, "任务不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	newTask := *oldTask
	newTask.ID = uuid.NewString()
	newTask.BaseURL = strings.TrimSpace(baseURL)
	newTask.Status = model.TaskPending
	newTask.FinalPrompt = ""
	newTask.RequestHeaders = ""
	newTask.RequestJSON = ""
	newTask.ResponseHeaders = ""
	newTask.ResponseJSON = ""
	newTask.Favorite = false
	newTask.SharedToPlaza = false
	newTask.ResultImages = nil
	newTask.ResultImagesJSON = "[]"
	newTask.ResultVideos = nil
	newTask.ResultVideosJSON = "[]"
	newTask.UpstreamTaskID = ""
	newTask.UpstreamStatus = ""
	newTask.UpstreamProgress = 0
	newTask.NextPollAt = nil
	newTask.PollCount = 0
	newTask.ErrorMessage = ""
	newTask.ElapsedMS = 0
	newTask.StartedAt = nil
	newTask.CompletedAt = nil
	if err := h.Store.CreateTask(r.Context(), &newTask); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, newTask)
}

func (h *Handler) allowBaseURL(w http.ResponseWriter, r *http.Request, baseURL string) bool {
	config, err := h.Store.SiteConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if !config.BaseURLWhitelistEnabled {
		return true
	}
	normalized, err := normalizeBaseURL(baseURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "baseurl 无效")
		return false
	}
	for _, allowed := range config.BaseURLWhitelist {
		allowedURL, err := normalizeBaseURL(allowed.URL)
		if err == nil && normalized == allowedURL {
			return true
		}
	}
	writeJSON(w, http.StatusForbidden, map[string]any{
		"error":               "该 BASEURL 未授权，请联系管理员授权。",
		"code":                "baseurl_not_authorized",
		"admin_contact_image": config.AdminContactImage,
	})
	return false
}

func (h *Handler) matchBaseURL(config model.SiteConfig, baseURL string) (model.BaseURLAllowEntry, bool) {
	normalized, err := normalizeBaseURL(baseURL)
	if err != nil {
		return model.BaseURLAllowEntry{}, false
	}
	for _, entry := range config.BaseURLWhitelist {
		allowedURL, err := normalizeBaseURL(entry.URL)
		if err == nil && normalized == allowedURL {
			return entry, true
		}
	}
	return model.BaseURLAllowEntry{}, false
}

func normalizeBaseURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}
	if parsed.Host == "" || !slices.Contains([]string{"http", "https"}, parsed.Scheme) {
		return "", fmt.Errorf("invalid baseurl")
	}
	parsed.Scheme = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimPrefix(parsed.String(), "//"), nil
}

func maskBaseURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" {
		return strings.TrimSpace(value)
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "读取请求失败")
		return false
	}
	if err := json.Unmarshal(data, target); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 格式错误")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "不支持的请求方法")
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func cleanMediaAssets(items []model.MediaAsset, assetType string) []model.MediaAsset {
	clean := []model.MediaAsset{}
	for _, item := range items {
		item.URL = strings.TrimSpace(item.URL)
		if item.URL == "" {
			continue
		}
		if item.Type == "" {
			item.Type = assetType
		}
		clean = append(clean, item)
	}
	return clean
}

func isImageModel(value string) bool {
	modelName := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(modelName, "gpt-image") || strings.Contains(modelName, "image")
}

type videoCapability struct {
	ratios            []string
	resolutions       []string
	minDuration       int
	maxDuration       int
	defaultDuration   int
	defaultRatio      string
	defaultResolution string
}

func normalizeVideoTaskOptions(task *model.Task) {
	capability := videoModelCapability(task.Model)
	if !stringIn(task.VideoRatio, capability.ratios) {
		fmt.Println("handler video_options adjusted_ratio:", "id=", task.ID, "model=", task.Model, "from=", task.VideoRatio, "to=", capability.defaultRatio)
		task.VideoRatio = capability.defaultRatio
	}
	resolution := videoResolutionFromSize(task.VideoWidth, task.VideoHeight)
	if !stringIn(resolution, capability.resolutions) {
		fmt.Println("handler video_options adjusted_resolution:", "id=", task.ID, "model=", task.Model, "from=", resolution, "to=", capability.defaultResolution)
		resolution = capability.defaultResolution
	}
	if task.VideoDuration <= 0 {
		task.VideoDuration = capability.defaultDuration
	}
	if task.VideoDuration < capability.minDuration {
		fmt.Println("handler video_options adjusted_duration:", "id=", task.ID, "model=", task.Model, "from=", task.VideoDuration, "to=", capability.minDuration)
		task.VideoDuration = capability.minDuration
	}
	if task.VideoDuration > capability.maxDuration {
		fmt.Println("handler video_options adjusted_duration:", "id=", task.ID, "model=", task.Model, "from=", task.VideoDuration, "to=", capability.maxDuration)
		task.VideoDuration = capability.maxDuration
	}
	size := videoSizeFor(task.VideoRatio, resolution)
	task.VideoWidth = size.Width
	task.VideoHeight = size.Height
}

func videoModelCapability(modelName string) videoCapability {
	normalized := strings.ToLower(strings.TrimSpace(modelName))
	if normalized == "doubao-seedance-2.0" || normalized == "doubao-seedance-2-0" {
		return videoCapability{
			ratios:            []string{"21:9", "16:9", "4:3", "1:1", "3:4", "9:16", "adaptive"},
			resolutions:       []string{"480p", "720p", "1080p"},
			minDuration:       4,
			maxDuration:       15,
			defaultDuration:   5,
			defaultRatio:      "16:9",
			defaultResolution: "720p",
		}
	}
	return videoCapability{
		ratios:            []string{"16:9", "9:16", "1:1", "4:3", "3:4"},
		resolutions:       []string{"480p", "720p", "1080p"},
		minDuration:       1,
		maxDuration:       30,
		defaultDuration:   5,
		defaultRatio:      "16:9",
		defaultResolution: "720p",
	}
}

func videoResolutionFromSize(width, height int) string {
	shortSide := width
	if height < shortSide || shortSide <= 0 {
		shortSide = height
	}
	if shortSide >= 1000 {
		return "1080p"
	}
	if shortSide >= 700 {
		return "720p"
	}
	return "480p"
}

func videoSizeFor(ratio, resolution string) model.MediaAsset {
	shortSide := 720
	switch resolution {
	case "480p":
		shortSide = 480
	case "1080p":
		shortSide = 1080
	}
	switch ratio {
	case "21:9":
		return model.MediaAsset{Width: shortSide * 7 / 3, Height: shortSide}
	case "9:16":
		return model.MediaAsset{Width: shortSide, Height: shortSide * 16 / 9}
	case "1:1":
		return model.MediaAsset{Width: shortSide, Height: shortSide}
	case "4:3":
		return model.MediaAsset{Width: shortSide * 4 / 3, Height: shortSide}
	case "3:4":
		return model.MediaAsset{Width: shortSide, Height: shortSide * 4 / 3}
	default:
		return model.MediaAsset{Width: shortSide * 16 / 9, Height: shortSide}
	}
}

func stringIn(value string, items []string) bool {
	for _, item := range items {
		if value == item {
			return true
		}
	}
	return false
}

func inferCreateTaskType(req model.CreateTaskRequest) model.TaskType {
	if strings.EqualFold(strings.TrimSpace(req.NodeKind), "video") {
		return model.TaskTypeVideoGeneration
	}
	if strings.EqualFold(strings.TrimSpace(req.NodeKind), "image") {
		return model.TaskTypeImageGeneration
	}
	if req.TaskType == model.TaskTypeVideoGeneration {
		return model.TaskTypeVideoGeneration
	}
	if req.TaskType == model.TaskTypeImageGeneration {
		return model.TaskTypeImageGeneration
	}
	return model.TaskTypeImageGeneration
}
