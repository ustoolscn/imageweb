package handler

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
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
	"image-web/backend/internal/model"

	"github.com/google/uuid"
)

type Handler struct {
	Store     *db.Store
	Generator *generator.Client
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/health", h.health)
	mux.HandleFunc("/api/site-brand", h.siteBrand)
	mux.HandleFunc("/api/models", h.models)
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
	brand := model.SiteBrandResponse{Title: defaultString(config.SiteTitle, "图片生成工作台"), Icon: defaultString(config.SiteIcon, "AI")}
	if entry, ok := h.matchBaseURL(config, r.URL.Query().Get("baseurl")); ok {
		if entry.Title != "" {
			brand.Title = entry.Title
		}
		if entry.Icon != "" {
			brand.Icon = entry.Icon
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
	if req.APIKey == "" || req.BaseURL == "" || strings.TrimSpace(req.Prompt) == "" || req.Model == "" {
		writeError(w, http.StatusBadRequest, "缺少必要参数")
		return
	}
	if !h.allowBaseURL(w, r, req.BaseURL) {
		return
	}
	task := &model.Task{
		ID:                uuid.NewString(),
		APIKey:            req.APIKey,
		BaseURL:           req.BaseURL,
		Status:            model.TaskPending,
		Prompt:            strings.TrimSpace(req.Prompt),
		Model:             "gpt-image-2",
		Size:              defaultString(req.Size, "1024x1024"),
		Quality:           defaultString(req.Quality, "auto"),
		OutputFormat:      defaultString(req.OutputFormat, "png"),
		OutputCompression: req.OutputCompression,
		Background:        defaultString(req.Background, "auto"),
		Moderation:        defaultString(req.Moderation, "low"),
		N:                 req.N,
		ReferenceImages:   req.ReferenceImages,
	}
	if task.N <= 0 {
		task.N = 1
	}
	if task.OutputCompression < 0 || task.OutputCompression > 100 {
		task.OutputCompression = 100
	}
	if err := h.Store.CreateTask(r.Context(), task); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (h *Handler) plaza(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	beforeLikeCount, _ := strconv.Atoi(r.URL.Query().Get("before_like_count"))
	items, total, err := h.Store.ListPlazaItems(r.Context(), r.URL.Query().Get("sort"), r.URL.Query().Get("before_created_at"), r.URL.Query().Get("before_id"), beforeLikeCount, r.URL.Query().Get("client_id"), limit)
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
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
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
