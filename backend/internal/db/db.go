package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"image-web/backend/internal/model"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type Store struct {
	db *sql.DB
}

func Open(dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("database dsn is required")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	database.SetConnMaxIdleTime(30 * time.Second)
	database.SetConnMaxLifetime(5 * time.Minute)
	database.SetMaxIdleConns(0)
	database.SetMaxOpenConns(10)
	if err := database.Ping(); err != nil {
		database.Close()
		return nil, err
	}
	store := &Store{db: database}
	if err := store.migrate(); err != nil {
		database.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY,
  api_key TEXT NOT NULL,
  base_url TEXT NOT NULL,
  status TEXT NOT NULL,
  prompt TEXT NOT NULL,
  final_prompt TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL,
  size TEXT NOT NULL,
  quality TEXT NOT NULL,
  output_format TEXT NOT NULL,
  output_compression INT NOT NULL,
  background TEXT NOT NULL,
  moderation TEXT NOT NULL,
  n INT NOT NULL,
  stream BOOLEAN NOT NULL DEFAULT FALSE,
  style TEXT NOT NULL DEFAULT '',
  response_format TEXT NOT NULL DEFAULT '',
  reference_images_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  favorite BOOLEAN NOT NULL DEFAULT FALSE,
  request_headers TEXT NOT NULL DEFAULT '',
  request_json TEXT NOT NULL DEFAULT '',
  response_headers TEXT NOT NULL DEFAULT '',
  response_json TEXT NOT NULL DEFAULT '',
  result_images_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  error_message TEXT NOT NULL DEFAULT '',
  elapsed_ms BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_tasks_api_key_created ON tasks(api_key, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_status_created ON tasks(status, created_at ASC);
CREATE TABLE IF NOT EXISTS plaza_items (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL UNIQUE,
  prompt TEXT NOT NULL,
  model TEXT NOT NULL,
  size TEXT NOT NULL,
  quality TEXT NOT NULL,
  output_format TEXT NOT NULL,
  output_compression INT NOT NULL,
  background TEXT NOT NULL,
  moderation TEXT NOT NULL,
  n INT NOT NULL,
  stream BOOLEAN NOT NULL DEFAULT FALSE,
  style TEXT NOT NULL DEFAULT '',
  response_format TEXT NOT NULL DEFAULT '',
  reference_images_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  result_images_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  like_count INT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_plaza_created ON plaza_items(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_plaza_likes ON plaza_items(like_count DESC, created_at DESC, id DESC);
CREATE TABLE IF NOT EXISTS plaza_likes (
  plaza_id TEXT NOT NULL,
  client_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (plaza_id, client_id)
);
CREATE TABLE IF NOT EXISTS site_config (
  config_key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS request_headers TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS response_headers TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS favorite BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS stream BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE plaza_items ADD COLUMN IF NOT EXISTS stream BOOLEAN NOT NULL DEFAULT FALSE;
`)
	if err != nil {
		return err
	}
	if err := s.migrateSiteConfigKey(); err != nil {
		return err
	}
	return s.ensureSiteConfig()
}

func (s *Store) migrateSiteConfigKey() error {
	legacyExists := false
	if err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'site_config' AND column_name = 'key')`).Scan(&legacyExists); err != nil {
		return err
	}
	newExists := false
	if err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'site_config' AND column_name = 'config_key')`).Scan(&newExists); err != nil {
		return err
	}
	if legacyExists && !newExists {
		_, err := s.db.Exec(`ALTER TABLE site_config RENAME COLUMN key TO config_key`)
		return err
	}
	return nil
}

func (s *Store) ensureSiteConfig() error {
	for _, entry := range []struct {
		key   string
		value string
	}{
		{"baseurl_whitelist_enabled", "false"},
		{"baseurl_whitelist", "[]"},
		{"admin_contact_image", ""},
		{"site_title", "图片生成工作台"},
		{"site_icon", "AI"},
		{"worker_concurrency", "1"},
	} {
		if _, err := s.db.Exec(`INSERT INTO site_config (config_key, value) VALUES ($1, $2) ON CONFLICT (config_key) DO NOTHING`, entry.key, entry.value); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SiteConfig(ctx context.Context) (model.SiteConfig, error) {
	config := model.SiteConfig{}
	rows, err := s.db.QueryContext(ctx, `SELECT config_key, value FROM site_config WHERE config_key IN ('baseurl_whitelist_enabled', 'baseurl_whitelist', 'admin_contact_image', 'site_title', 'site_icon', 'worker_concurrency')`)
	if err != nil {
		return config, err
	}
	defer rows.Close()
	for rows.Next() {
		key := ""
		value := ""
		if err := rows.Scan(&key, &value); err != nil {
			return config, err
		}
		switch key {
		case "baseurl_whitelist_enabled":
			config.BaseURLWhitelistEnabled = value == "true" || value == "1"
		case "baseurl_whitelist":
			config.BaseURLWhitelist = parseBaseURLWhitelist(value)
		case "admin_contact_image":
			config.AdminContactImage = value
		case "site_title":
			config.SiteTitle = value
		case "site_icon":
			config.SiteIcon = value
		case "worker_concurrency":
			config.WorkerConcurrency, _ = strconv.Atoi(value)
		}
	}
	return config, rows.Err()
}

func parseBaseURLWhitelist(value string) []model.BaseURLAllowEntry {
	entries := []model.BaseURLAllowEntry{}
	if err := json.Unmarshal([]byte(value), &entries); err == nil {
		return entries
	}
	urls := []string{}
	if err := json.Unmarshal([]byte(value), &urls); err == nil {
		for _, url := range urls {
			entries = append(entries, model.BaseURLAllowEntry{URL: url})
		}
	}
	return entries
}

func (s *Store) CreateTask(ctx context.Context, task *model.Task) error {
	now := time.Now().UTC()
	task.CreatedAt = now
	task.UpdatedAt = now
	refs, err := json.Marshal(task.ReferenceImages)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO tasks (
	 id, api_key, base_url, status, prompt, final_prompt, model, size, quality, output_format,
	 output_compression, background, moderation, n, stream, style, response_format, reference_images_json,
	 favorite, request_headers, request_json, response_headers, response_json, result_images_json,
	 error_message, elapsed_ms, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18::jsonb, $19, $20, $21, $22, $23, $24::jsonb, $25, $26, $27, $28)`,
		task.ID, task.APIKey, task.BaseURL, task.Status, task.Prompt, task.FinalPrompt, task.Model,
		task.Size, task.Quality, task.OutputFormat, task.OutputCompression, task.Background,
		task.Moderation, task.N, task.Stream, task.Style, task.ResponseFormat, string(refs), task.Favorite,
		task.RequestHeaders, task.RequestJSON, task.ResponseHeaders, task.ResponseJSON, "[]", task.ErrorMessage,
		task.ElapsedMS, task.CreatedAt, task.UpdatedAt)
	return err
}

func (s *Store) ListTasks(ctx context.Context, apiKey, baseURL, status, query, beforeCreatedAt, beforeID string, favoriteOnly bool, limit int) ([]model.Task, int, error) {
	baseURLPattern := baseURLMatchPattern(baseURL)
	args := []any{apiKey, baseURLPattern}
	where := []string{"api_key = $1", "regexp_replace(base_url, '^https?://', '') = $2"}
	if status != "" && status != "all" {
		args = append(args, status)
		where = append(where, "status = "+placeholder(len(args)))
	}
	if query != "" {
		args = append(args, "%"+query+"%")
		where = append(where, "prompt LIKE "+placeholder(len(args)))
	}
	if favoriteOnly {
		where = append(where, "favorite = TRUE")
	}
	whereClause := strings.Join(where, " AND ")
	total := 0
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE `+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listArgs := append([]any{}, args...)
	listWhere := append([]string{}, where...)
	if beforeCreatedAt != "" && beforeID != "" {
		beforeTime, err := time.Parse(time.RFC3339Nano, beforeCreatedAt)
		if err != nil {
			return nil, 0, err
		}
		listArgs = append(listArgs, beforeTime, beforeTime, beforeID)
		listWhere = append(listWhere, fmt.Sprintf("(created_at < %s OR (created_at = %s AND id < %s))", placeholder(len(listArgs)-2), placeholder(len(listArgs)-1), placeholder(len(listArgs))))
	}
	if limit <= 0 || limit > 60 {
		limit = 30
	}
	listArgs = append(listArgs, limit+1)
	rows, err := s.db.QueryContext(ctx, `SELECT `+taskColumns()+` FROM tasks WHERE `+strings.Join(listWhere, " AND ")+` ORDER BY created_at DESC, id DESC LIMIT `+placeholder(len(listArgs)), listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	tasks, err := scanTasks(rows)
	return tasks, total, err
}

func (s *Store) GetTask(ctx context.Context, id, apiKey, baseURL string) (*model.Task, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+taskDetailColumns()+` FROM tasks WHERE id = $1 AND api_key = $2 AND regexp_replace(base_url, '^https?://', '') = $3`, id, apiKey, baseURLMatchPattern(baseURL))
	return scanTask(row)
}

func (s *Store) GetAnyTask(ctx context.Context, id string) (*model.Task, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+taskDetailColumns()+` FROM tasks WHERE id = $1`, id)
	return scanTask(row)
}

func (s *Store) ShareTaskToPlaza(ctx context.Context, id, apiKey, baseURL string) (*model.PlazaItem, error) {
	task, err := s.GetTask(ctx, id, apiKey, baseURL)
	if err != nil {
		return nil, err
	}
	if task.Status != model.TaskSucceeded || len(task.ResultImages) == 0 || task.ResultImages[0].URL == "" {
		return nil, fmt.Errorf("只有成功任务可以分享到广场")
	}
	now := time.Now().UTC()
	plazaID := ""
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM plaza_items WHERE task_id = $1`, id).Scan(&plazaID); err == nil {
		return s.PlazaItem(ctx, plazaID, "")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	refs, err := json.Marshal(task.ReferenceImages)
	if err != nil {
		return nil, err
	}
	results, err := json.Marshal(task.ResultImages)
	if err != nil {
		return nil, err
	}
	plazaID = uuid.NewString()
	_, err = s.db.ExecContext(ctx, `INSERT INTO plaza_items (
	 id, task_id, prompt, model, size, quality, output_format, output_compression,
	 background, moderation, n, stream, style, response_format, reference_images_json,
	 result_images_json, like_count, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb, $16::jsonb, 0, $17, $18)`,
		plazaID, task.ID, task.Prompt, task.Model, task.Size, task.Quality, task.OutputFormat,
		task.OutputCompression, task.Background, task.Moderation, task.N, task.Stream, task.Style,
		task.ResponseFormat, string(refs), string(results), now, now)
	if err != nil {
		return nil, err
	}
	return s.PlazaItem(ctx, plazaID, "")
}

func (s *Store) UnshareTaskFromPlaza(ctx context.Context, id, apiKey, baseURL string) error {
	if _, err := s.GetTask(ctx, id, apiKey, baseURL); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var plazaID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM plaza_items WHERE task_id = $1`, id).Scan(&plazaID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM plaza_likes WHERE plaza_id = $1`, plazaID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM plaza_items WHERE id = $1`, plazaID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) TaskUpdates(ctx context.Context, apiKey, baseURL string, ids []string) ([]model.TaskUpdate, error) {
	if len(ids) == 0 {
		return []model.TaskUpdate{}, nil
	}
	args := []any{apiKey, baseURLMatchPattern(baseURL)}
	placeholders := make([]string, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
		placeholders = append(placeholders, placeholder(len(args)))
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, status, result_images_json::text, error_message, elapsed_ms, updated_at, started_at, completed_at, CASE WHEN status = 'pending' THEN (SELECT COUNT(*) FROM tasks queued WHERE queued.status = 'pending' AND queued.created_at < tasks.created_at) ELSE 0 END FROM tasks WHERE api_key = $1 AND regexp_replace(base_url, '^https?://', '') = $2 AND id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	updates := []model.TaskUpdate{}
	for rows.Next() {
		var update model.TaskUpdate
		var startedAt, completedAt sql.NullTime
		if err := rows.Scan(&update.ID, &update.Status, &update.ResultImagesJSON, &update.ErrorMessage, &update.ElapsedMS, &update.UpdatedAt, &startedAt, &completedAt, &update.QueuePosition); err != nil {
			return nil, err
		}
		if startedAt.Valid {
			update.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			update.CompletedAt = &completedAt.Time
		}
		if update.ResultImagesJSON != "" {
			_ = json.Unmarshal([]byte(update.ResultImagesJSON), &update.ResultImages)
		}
		updates = append(updates, update)
	}
	return updates, rows.Err()
}

func (s *Store) ListPlazaItems(ctx context.Context, sort, beforeCreatedAt, beforeID string, beforeLikeCount int, clientID string, limit int) ([]model.PlazaItem, int, error) {
	total := 0
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM plaza_items`).Scan(&total); err != nil {
		return nil, 0, err
	}
	args := []any{clientID}
	where := []string{"1 = 1"}
	orderBy := "created_at DESC, id DESC"
	if sort == "likes" {
		orderBy = "like_count DESC, created_at DESC, id DESC"
		if beforeCreatedAt != "" && beforeID != "" {
			beforeTime, err := time.Parse(time.RFC3339Nano, beforeCreatedAt)
			if err != nil {
				return nil, 0, err
			}
			args = append(args, beforeLikeCount, beforeLikeCount, beforeTime, beforeTime, beforeID)
			where = append(where, fmt.Sprintf("(like_count < %s OR (like_count = %s AND (created_at < %s OR (created_at = %s AND id < %s))))", placeholder(len(args)-4), placeholder(len(args)-3), placeholder(len(args)-2), placeholder(len(args)-1), placeholder(len(args))))
		}
	} else if beforeCreatedAt != "" && beforeID != "" {
		beforeTime, err := time.Parse(time.RFC3339Nano, beforeCreatedAt)
		if err != nil {
			return nil, 0, err
		}
		args = append(args, beforeTime, beforeTime, beforeID)
		where = append(where, fmt.Sprintf("(created_at < %s OR (created_at = %s AND id < %s))", placeholder(len(args)-2), placeholder(len(args)-1), placeholder(len(args))))
	}
	if limit <= 0 || limit > 60 {
		limit = 30
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `SELECT `+plazaColumns()+` FROM plaza_items WHERE `+strings.Join(where, " AND ")+` ORDER BY `+orderBy+` LIMIT `+placeholder(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := scanPlazaItems(rows)
	return items, total, err
}

func (s *Store) PlazaItem(ctx context.Context, id, clientID string) (*model.PlazaItem, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+plazaColumns()+` FROM plaza_items WHERE id = $2`, clientID, id)
	return scanPlazaItem(row)
}

func (s *Store) SetPlazaLike(ctx context.Context, id, clientID string, liked bool) (*model.PlazaItem, error) {
	if strings.TrimSpace(clientID) == "" {
		return nil, fmt.Errorf("缺少 client_id")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if liked {
		result, err := tx.ExecContext(ctx, `INSERT INTO plaza_likes (plaza_id, client_id, created_at) VALUES ($1, $2, $3) ON CONFLICT (plaza_id, client_id) DO NOTHING`, id, clientID, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		if count, _ := result.RowsAffected(); count > 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE plaza_items SET like_count = like_count + 1, updated_at = $1 WHERE id = $2`, time.Now().UTC(), id); err != nil {
				return nil, err
			}
		}
	} else {
		result, err := tx.ExecContext(ctx, `DELETE FROM plaza_likes WHERE plaza_id = $1 AND client_id = $2`, id, clientID)
		if err != nil {
			return nil, err
		}
		if count, _ := result.RowsAffected(); count > 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE plaza_items SET like_count = GREATEST(like_count - 1, 0), updated_at = $1 WHERE id = $2`, time.Now().UTC(), id); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.PlazaItem(ctx, id, clientID)
}

func (s *Store) DeleteTask(ctx context.Context, id, apiKey, baseURL string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = $1 AND api_key = $2 AND regexp_replace(base_url, '^https?://', '') = $3`, id, apiKey, baseURLMatchPattern(baseURL))
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) SetFavorite(ctx context.Context, id, apiKey, baseURL string, favorite bool) error {
	result, err := s.db.ExecContext(ctx, `UPDATE tasks SET favorite = $1, updated_at = $2 WHERE id = $3 AND api_key = $4 AND regexp_replace(base_url, '^https?://', '') = $5`, favorite, time.Now().UTC(), id, apiKey, baseURLMatchPattern(baseURL))
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ResetStaleRunningTasks(ctx context.Context, maxAge time.Duration) error {
	cutoff := time.Now().UTC().Add(-maxAge)
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET status = $1, updated_at = $2, error_message = '' WHERE status = $3 AND started_at < $4`, model.TaskPending, time.Now().UTC(), model.TaskRunning, cutoff)
	return err
}

func (s *Store) NextPendingTask(ctx context.Context) (*model.Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `SELECT `+taskColumns()+` FROM tasks WHERE status = $1 ORDER BY created_at ASC LIMIT 1 FOR UPDATE SKIP LOCKED`, model.TaskPending)
	task, err := scanTask(row)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE tasks SET status = $1, started_at = $2, updated_at = $3 WHERE id = $4 AND status = $5`, model.TaskRunning, now, now, task.ID, model.TaskPending)
	if err != nil {
		return nil, err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return nil, sql.ErrNoRows
	}
	task.Status = model.TaskRunning
	task.StartedAt = &now
	task.UpdatedAt = now
	return task, tx.Commit()
}

func (s *Store) CompleteTask(ctx context.Context, id string, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON string, results []model.UploadedImage, elapsedMS int64) error {
	payload, err := json.Marshal(results)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE tasks SET status = $1, final_prompt = $2, request_headers = $3, request_json = $4, response_headers = $5, response_json = $6, result_images_json = $7::jsonb, elapsed_ms = $8, completed_at = $9, updated_at = $10, error_message = '' WHERE id = $11`, model.TaskSucceeded, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, string(payload), elapsedMS, now, now, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) FailTask(ctx context.Context, id string, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, message string, elapsedMS int64) error {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE tasks SET status = $1, final_prompt = $2, request_headers = $3, request_json = $4, response_headers = $5, response_json = $6, error_message = $7, elapsed_ms = $8, completed_at = $9, updated_at = $10 WHERE id = $11`, model.TaskFailed, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, message, elapsedMS, now, now, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func baseURLMatchPattern(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	baseURL = strings.TrimPrefix(baseURL, "http://")
	baseURL = strings.TrimPrefix(baseURL, "https://")
	return strings.TrimRight(baseURL, "/")
}

func taskColumns() string {
	return `id, api_key, base_url, status, prompt, final_prompt, model, size, quality, output_format, output_compression, background, moderation, n, stream, style, response_format, reference_images_json::text, favorite, '' AS request_headers, '' AS request_json, '' AS response_headers, '' AS response_json, result_images_json::text, error_message, elapsed_ms, created_at, updated_at, started_at, completed_at, CASE WHEN status = 'pending' THEN (SELECT COUNT(*) FROM tasks queued WHERE queued.status = 'pending' AND queued.created_at < tasks.created_at) ELSE 0 END, EXISTS(SELECT 1 FROM plaza_items WHERE plaza_items.task_id = tasks.id)`
}

func plazaColumns() string {
	return `id, task_id, prompt, model, size, quality, output_format, output_compression, background, moderation, n, stream, style, response_format, reference_images_json::text, result_images_json::text, like_count, EXISTS(SELECT 1 FROM plaza_likes WHERE plaza_likes.plaza_id = plaza_items.id AND plaza_likes.client_id = $1), created_at`
}

func taskDetailColumns() string {
	return `id, api_key, base_url, status, prompt, final_prompt, model, size, quality, output_format, output_compression, background, moderation, n, stream, style, response_format, reference_images_json::text, favorite, request_headers, request_json, response_headers, response_json, result_images_json::text, error_message, elapsed_ms, created_at, updated_at, started_at, completed_at, CASE WHEN status = 'pending' THEN (SELECT COUNT(*) FROM tasks queued WHERE queued.status = 'pending' AND queued.created_at < tasks.created_at) ELSE 0 END, EXISTS(SELECT 1 FROM plaza_items WHERE plaza_items.task_id = tasks.id)`
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(row scanner) (*model.Task, error) {
	var task model.Task
	var startedAt, completedAt sql.NullTime
	if err := row.Scan(&task.ID, &task.APIKey, &task.BaseURL, &task.Status, &task.Prompt, &task.FinalPrompt, &task.Model, &task.Size, &task.Quality, &task.OutputFormat, &task.OutputCompression, &task.Background, &task.Moderation, &task.N, &task.Stream, &task.Style, &task.ResponseFormat, &task.ReferenceImagesJSON, &task.Favorite, &task.RequestHeaders, &task.RequestJSON, &task.ResponseHeaders, &task.ResponseJSON, &task.ResultImagesJSON, &task.ErrorMessage, &task.ElapsedMS, &task.CreatedAt, &task.UpdatedAt, &startedAt, &completedAt, &task.QueuePosition, &task.SharedToPlaza); err != nil {
		return nil, err
	}
	if startedAt.Valid {
		task.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		task.CompletedAt = &completedAt.Time
	}
	decodeTaskJSON(&task)
	return &task, nil
}

func scanTasks(rows *sql.Rows) ([]model.Task, error) {
	tasks := []model.Task{}
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *task)
	}
	return tasks, rows.Err()
}

func scanPlazaItem(row scanner) (*model.PlazaItem, error) {
	var item model.PlazaItem
	if err := row.Scan(&item.ID, &item.TaskID, &item.Prompt, &item.Model, &item.Size, &item.Quality, &item.OutputFormat, &item.OutputCompression, &item.Background, &item.Moderation, &item.N, &item.Stream, &item.Style, &item.ResponseFormat, &item.ReferenceImagesJSON, &item.ResultImagesJSON, &item.LikeCount, &item.Liked, &item.CreatedAt); err != nil {
		return nil, err
	}
	decodePlazaJSON(&item)
	return &item, nil
}

func scanPlazaItems(rows *sql.Rows) ([]model.PlazaItem, error) {
	items := []model.PlazaItem{}
	for rows.Next() {
		item, err := scanPlazaItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func decodeTaskJSON(task *model.Task) {
	if task.ReferenceImagesJSON != "" {
		_ = json.Unmarshal([]byte(task.ReferenceImagesJSON), &task.ReferenceImages)
	}
	if task.ResultImagesJSON != "" {
		_ = json.Unmarshal([]byte(task.ResultImagesJSON), &task.ResultImages)
	}
}

func decodePlazaJSON(item *model.PlazaItem) {
	if item.ReferenceImagesJSON != "" {
		_ = json.Unmarshal([]byte(item.ReferenceImagesJSON), &item.ReferenceImages)
	}
	if item.ResultImagesJSON != "" {
		_ = json.Unmarshal([]byte(item.ResultImagesJSON), &item.ResultImages)
	}
}

func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func MustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(data)
}

func placeholder(index int) string {
	return fmt.Sprintf("$%d", index)
}
