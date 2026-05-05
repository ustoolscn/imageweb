package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

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
	mux.HandleFunc("/api/models", h.models)
	mux.HandleFunc("/api/tasks", h.tasks)
	mux.HandleFunc("/api/tasks/", h.taskByID)
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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

func (h *Handler) listTasks(w http.ResponseWriter, r *http.Request) {
	apiKey := r.URL.Query().Get("apikey")
	if apiKey == "" {
		writeError(w, http.StatusBadRequest, "缺少 apikey")
		return
	}
	tasks, err := h.Store.ListTasks(r.Context(), apiKey, r.URL.Query().Get("status"), r.URL.Query().Get("q"), r.URL.Query().Get("favorite") == "1")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": tasks})
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
	if apiKey == "" {
		writeError(w, http.StatusBadRequest, "缺少 apikey")
		return
	}
	task, err := h.Store.GetTask(r.Context(), id, apiKey)
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
	if apiKey == "" {
		writeError(w, http.StatusBadRequest, "缺少 apikey")
		return
	}
	if err := h.Store.DeleteTask(r.Context(), id, apiKey); err != nil {
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
	if apiKey == "" {
		writeError(w, http.StatusBadRequest, "缺少 apikey")
		return
	}
	var req struct {
		Favorite bool `json:"favorite"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.Store.SetFavorite(r.Context(), id, apiKey, req.Favorite); err != nil {
		if db.IsNotFound(err) {
			writeError(w, http.StatusNotFound, "任务不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	task, err := h.Store.GetTask(r.Context(), id, apiKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *Handler) retryTask(w http.ResponseWriter, r *http.Request, id string) {
	apiKey := r.URL.Query().Get("apikey")
	if apiKey == "" {
		writeError(w, http.StatusBadRequest, "缺少 apikey")
		return
	}
	oldTask, err := h.Store.GetTask(r.Context(), id, apiKey)
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
