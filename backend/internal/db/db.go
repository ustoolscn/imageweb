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
  task_type TEXT NOT NULL DEFAULT 'image_generation',
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
  input_fidelity TEXT NOT NULL DEFAULT 'high',
  n INT NOT NULL,
  stream BOOLEAN NOT NULL DEFAULT FALSE,
  style TEXT NOT NULL DEFAULT '',
  response_format TEXT NOT NULL DEFAULT '',
  reference_images_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  reference_videos_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  reference_audios_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  favorite BOOLEAN NOT NULL DEFAULT FALSE,
  request_headers TEXT NOT NULL DEFAULT '',
  request_json TEXT NOT NULL DEFAULT '',
  response_headers TEXT NOT NULL DEFAULT '',
  response_json TEXT NOT NULL DEFAULT '',
  result_images_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  result_videos_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  upstream_task_id TEXT NOT NULL DEFAULT '',
  upstream_status TEXT NOT NULL DEFAULT '',
  upstream_progress INT NOT NULL DEFAULT 0,
  next_poll_at TIMESTAMPTZ,
  poll_count INT NOT NULL DEFAULT 0,
  video_ratio TEXT NOT NULL DEFAULT '',
  video_width INT NOT NULL DEFAULT 0,
  video_height INT NOT NULL DEFAULT 0,
  video_duration INT NOT NULL DEFAULT 0,
  generate_audio BOOLEAN NOT NULL DEFAULT FALSE,
  watermark BOOLEAN NOT NULL DEFAULT FALSE,
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
  task_type TEXT NOT NULL DEFAULT 'image_generation',
  prompt TEXT NOT NULL,
  model TEXT NOT NULL,
  size TEXT NOT NULL,
  quality TEXT NOT NULL,
  output_format TEXT NOT NULL,
  output_compression INT NOT NULL,
  background TEXT NOT NULL,
  moderation TEXT NOT NULL,
  input_fidelity TEXT NOT NULL DEFAULT 'high',
  n INT NOT NULL,
  stream BOOLEAN NOT NULL DEFAULT FALSE,
  style TEXT NOT NULL DEFAULT '',
  response_format TEXT NOT NULL DEFAULT '',
  reference_images_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  reference_videos_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  reference_audios_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  result_images_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  result_videos_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  video_ratio TEXT NOT NULL DEFAULT '',
  video_width INT NOT NULL DEFAULT 0,
  video_height INT NOT NULL DEFAULT 0,
  video_duration INT NOT NULL DEFAULT 0,
  generate_audio BOOLEAN NOT NULL DEFAULT FALSE,
  watermark BOOLEAN NOT NULL DEFAULT FALSE,
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
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS task_type TEXT NOT NULL DEFAULT 'image_generation';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS reference_videos_json JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS reference_audios_json JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS result_videos_json JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS upstream_task_id TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS upstream_status TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS upstream_progress INT NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS next_poll_at TIMESTAMPTZ;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS poll_count INT NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS video_ratio TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS video_width INT NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS video_height INT NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS video_duration INT NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS generate_audio BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS watermark BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS input_fidelity TEXT NOT NULL DEFAULT 'high';
ALTER TABLE plaza_items ADD COLUMN IF NOT EXISTS stream BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE plaza_items ADD COLUMN IF NOT EXISTS task_type TEXT NOT NULL DEFAULT 'image_generation';
ALTER TABLE plaza_items ADD COLUMN IF NOT EXISTS reference_videos_json JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE plaza_items ADD COLUMN IF NOT EXISTS reference_audios_json JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE plaza_items ADD COLUMN IF NOT EXISTS result_videos_json JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE plaza_items ADD COLUMN IF NOT EXISTS video_ratio TEXT NOT NULL DEFAULT '';
ALTER TABLE plaza_items ADD COLUMN IF NOT EXISTS video_width INT NOT NULL DEFAULT 0;
ALTER TABLE plaza_items ADD COLUMN IF NOT EXISTS video_height INT NOT NULL DEFAULT 0;
ALTER TABLE plaza_items ADD COLUMN IF NOT EXISTS video_duration INT NOT NULL DEFAULT 0;
ALTER TABLE plaza_items ADD COLUMN IF NOT EXISTS generate_audio BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE plaza_items ADD COLUMN IF NOT EXISTS watermark BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE plaza_items ADD COLUMN IF NOT EXISTS input_fidelity TEXT NOT NULL DEFAULT 'high';
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
	refVideos, err := json.Marshal(task.ReferenceVideos)
	if err != nil {
		return err
	}
	refAudios, err := json.Marshal(task.ReferenceAudios)
	if err != nil {
		return err
	}
	if task.TaskType == "" {
		task.TaskType = model.TaskTypeImageGeneration
	}
	if task.InputFidelity == "" {
		task.InputFidelity = "high"
	}
	fmt.Println("db create_task insert_begin:",
		"id=", task.ID,
		"task_type=", task.TaskType,
		"status=", task.Status,
		"model=", task.Model,
		"base_url_match=", baseURLMatchPattern(task.BaseURL),
		"image_size=", task.Size,
		"video_ratio=", task.VideoRatio,
		"video_width=", task.VideoWidth,
		"video_height=", task.VideoHeight,
		"video_duration=", task.VideoDuration,
		"ref_images=", len(task.ReferenceImages),
		"ref_videos=", len(task.ReferenceVideos),
		"ref_audios=", len(task.ReferenceAudios),
	)
	result, err := s.db.ExecContext(ctx, `INSERT INTO tasks (
	 id, api_key, base_url, task_type, status, prompt, final_prompt, model, size, quality, output_format,
	 output_compression, background, moderation, input_fidelity, n, stream, style, response_format, reference_images_json,
	 reference_videos_json, reference_audios_json, favorite, request_headers, request_json, response_headers,
	 response_json, result_images_json, result_videos_json, upstream_task_id, upstream_status, upstream_progress,
	 next_poll_at, poll_count, video_ratio, video_width, video_height, video_duration, generate_audio, watermark,
	 error_message, elapsed_ms, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20::jsonb, $21::jsonb, $22::jsonb, $23, $24, $25, $26, $27, $28::jsonb, $29::jsonb, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40, $41, $42, $43, $44)`,
		task.ID, task.APIKey, task.BaseURL, task.TaskType, task.Status, task.Prompt, task.FinalPrompt, task.Model,
		task.Size, task.Quality, task.OutputFormat, task.OutputCompression, task.Background,
		task.Moderation, task.InputFidelity, task.N, task.Stream, task.Style, task.ResponseFormat, string(refs), string(refVideos),
		string(refAudios), task.Favorite, task.RequestHeaders, task.RequestJSON, task.ResponseHeaders,
		task.ResponseJSON, "[]", "[]", task.UpstreamTaskID, task.UpstreamStatus, task.UpstreamProgress,
		task.NextPollAt, task.PollCount, task.VideoRatio, task.VideoWidth, task.VideoHeight, task.VideoDuration,
		task.GenerateAudio, task.Watermark, task.ErrorMessage, task.ElapsedMS, task.CreatedAt, task.UpdatedAt)
	if err != nil {
		fmt.Println("db create_task insert_failed:", "id=", task.ID, "task_type=", task.TaskType, "error=", err)
		return err
	}
	count, _ := result.RowsAffected()
	fmt.Println("db create_task insert_done:", "id=", task.ID, "task_type=", task.TaskType, "status=", task.Status, "rows=", count)
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
		queryPlaceholder := placeholder(len(args))
		where = append(where, "(prompt ILIKE "+queryPlaceholder+" OR final_prompt ILIKE "+queryPlaceholder+" OR model ILIKE "+queryPlaceholder+" OR task_type ILIKE "+queryPlaceholder+")")
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
	if task.Status != model.TaskSucceeded || ((len(task.ResultImages) == 0 || task.ResultImages[0].URL == "") && (len(task.ResultVideos) == 0 || task.ResultVideos[0].URL == "")) {
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
	refVideos, err := json.Marshal(task.ReferenceVideos)
	if err != nil {
		return nil, err
	}
	refAudios, err := json.Marshal(task.ReferenceAudios)
	if err != nil {
		return nil, err
	}
	results, err := json.Marshal(task.ResultImages)
	if err != nil {
		return nil, err
	}
	resultVideos, err := json.Marshal(task.ResultVideos)
	if err != nil {
		return nil, err
	}
	plazaID = uuid.NewString()
	_, err = s.db.ExecContext(ctx, `INSERT INTO plaza_items (
	 id, task_id, task_type, prompt, model, size, quality, output_format, output_compression,
	 background, moderation, input_fidelity, n, stream, style, response_format, reference_images_json,
	 reference_videos_json, reference_audios_json, result_images_json, result_videos_json,
	 video_ratio, video_width, video_height, video_duration, generate_audio, watermark,
	 like_count, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17::jsonb, $18::jsonb, $19::jsonb, $20::jsonb, $21::jsonb, $22, $23, $24, $25, $26, $27, 0, $28, $29)`,
		plazaID, task.ID, task.TaskType, task.Prompt, task.Model, task.Size, task.Quality, task.OutputFormat,
		task.OutputCompression, task.Background, task.Moderation, task.InputFidelity, task.N, task.Stream, task.Style,
		task.ResponseFormat, string(refs), string(refVideos), string(refAudios), string(results), string(resultVideos),
		task.VideoRatio, task.VideoWidth, task.VideoHeight, task.VideoDuration, task.GenerateAudio, task.Watermark,
		now, now)
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
	rows, err := s.db.QueryContext(ctx, `SELECT id, status, result_images_json::text, result_videos_json::text, error_message, elapsed_ms, updated_at, started_at, completed_at, CASE WHEN status = 'pending' THEN (SELECT COUNT(*) FROM tasks queued WHERE queued.status = 'pending' AND queued.created_at < tasks.created_at) ELSE 0 END, upstream_status, upstream_progress FROM tasks WHERE api_key = $1 AND regexp_replace(base_url, '^https?://', '') = $2 AND id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	updates := []model.TaskUpdate{}
	for rows.Next() {
		var update model.TaskUpdate
		var startedAt, completedAt sql.NullTime
		if err := rows.Scan(&update.ID, &update.Status, &update.ResultImagesJSON, &update.ResultVideosJSON, &update.ErrorMessage, &update.ElapsedMS, &update.UpdatedAt, &startedAt, &completedAt, &update.QueuePosition, &update.UpstreamStatus, &update.UpstreamProgress); err != nil {
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
		if update.ResultVideosJSON != "" {
			_ = json.Unmarshal([]byte(update.ResultVideosJSON), &update.ResultVideos)
		}
		updates = append(updates, update)
	}
	return updates, rows.Err()
}

func (s *Store) ListPlazaItems(ctx context.Context, sort, q, beforeCreatedAt, beforeID string, beforeLikeCount int, clientID string, limit int) ([]model.PlazaItem, int, error) {
	total := 0
	args := []any{clientID}
	where := []string{"1 = 1"}
	countArgs := []any{}
	countWhere := []string{"1 = 1"}
	keyword := strings.TrimSpace(q)
	if keyword != "" {
		pattern := "%" + strings.ToLower(keyword) + "%"
		args = append(args, pattern)
		wherePlaceholder := placeholder(len(args))
		where = append(where, fmt.Sprintf("(LOWER(prompt) LIKE %s OR LOWER(model) LIKE %s OR LOWER(size) LIKE %s OR LOWER(quality) LIKE %s OR LOWER(output_format) LIKE %s OR LOWER(background) LIKE %s)", wherePlaceholder, wherePlaceholder, wherePlaceholder, wherePlaceholder, wherePlaceholder, wherePlaceholder))
		countArgs = append(countArgs, pattern)
		countPlaceholder := placeholder(len(countArgs))
		countWhere = append(countWhere, fmt.Sprintf("(LOWER(prompt) LIKE %s OR LOWER(model) LIKE %s OR LOWER(size) LIKE %s OR LOWER(quality) LIKE %s OR LOWER(output_format) LIKE %s OR LOWER(background) LIKE %s)", countPlaceholder, countPlaceholder, countPlaceholder, countPlaceholder, countPlaceholder, countPlaceholder))
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM plaza_items WHERE `+strings.Join(countWhere, " AND "), countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}
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
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET status = $1, updated_at = $2, error_message = '' WHERE status = $3 AND task_type <> $4 AND started_at < $5`, model.TaskPending, time.Now().UTC(), model.TaskRunning, model.TaskTypeVideoGeneration, cutoff)
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
	fmt.Println("db next_pending selected:",
		"id=", task.ID,
		"task_type=", task.TaskType,
		"status=", task.Status,
		"model=", task.Model,
		"base_url_match=", baseURLMatchPattern(task.BaseURL),
		"video_ratio=", task.VideoRatio,
		"video_width=", task.VideoWidth,
		"video_height=", task.VideoHeight,
		"video_duration=", task.VideoDuration,
		"created_at=", task.CreatedAt.Format(time.RFC3339Nano),
	)
	now := time.Now().UTC()
	fmt.Println("db next_pending status_update_begin:", "id=", task.ID, "from=", model.TaskPending, "to=", model.TaskRunning, "task_type=", task.TaskType)
	result, err := tx.ExecContext(ctx, `UPDATE tasks SET status = $1, started_at = $2, updated_at = $3 WHERE id = $4 AND status = $5`, model.TaskRunning, now, now, task.ID, model.TaskPending)
	if err != nil {
		fmt.Println("db next_pending status_update_failed:", "id=", task.ID, "task_type=", task.TaskType, "error=", err)
		return nil, err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		fmt.Println("db next_pending status_update_no_rows:", "id=", task.ID, "task_type=", task.TaskType)
		return nil, sql.ErrNoRows
	}
	task.Status = model.TaskRunning
	task.StartedAt = &now
	task.UpdatedAt = now
	if err := tx.Commit(); err != nil {
		fmt.Println("db next_pending commit_failed:", "id=", task.ID, "task_type=", task.TaskType, "error=", err)
		return nil, err
	}
	fmt.Println("db next_pending dispatched:", "id=", task.ID, "task_type=", task.TaskType, "status=", task.Status, "rows=", count)
	return task, nil
}

func (s *Store) CompleteTask(ctx context.Context, id string, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON string, results []model.UploadedImage, elapsedMS int64) error {
	payload, err := json.Marshal(results)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	fmt.Println("db complete_image begin:", "id=", id, "results=", len(results), "elapsed_ms=", elapsedMS)
	result, err := s.db.ExecContext(ctx, `UPDATE tasks SET status = $1, final_prompt = $2, request_headers = $3, request_json = $4, response_headers = $5, response_json = $6, result_images_json = $7::jsonb, elapsed_ms = $8, completed_at = $9, updated_at = $10, error_message = '' WHERE id = $11`, model.TaskSucceeded, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, string(payload), elapsedMS, now, now, id)
	if err != nil {
		fmt.Println("db complete_image failed:", "id=", id, "error=", err)
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		fmt.Println("db complete_image no_rows:", "id=", id)
		return sql.ErrNoRows
	}
	fmt.Println("db complete_image done:", "id=", id, "status=", model.TaskSucceeded, "rows=", count)
	return nil
}

func (s *Store) MarkVideoSubmitted(ctx context.Context, id, upstreamTaskID, upstreamStatus string, progress int, requestHeaders, requestJSON, responseHeaders, responseJSON string) error {
	now := time.Now().UTC()
	nextPollAt := now.Add(5 * time.Second)
	fmt.Println("db mark_video_submitted begin:", "id=", id, "upstream_task_id=", upstreamTaskID, "upstream_status=", upstreamStatus, "progress=", progress, "next_poll_at=", nextPollAt.Format(time.RFC3339Nano))
	result, err := s.db.ExecContext(ctx, `UPDATE tasks SET status = $1, request_headers = $2, request_json = $3, response_headers = $4, response_json = $5, upstream_task_id = $6, upstream_status = $7, upstream_progress = $8, next_poll_at = $9, poll_count = 0, updated_at = $10 WHERE id = $11`, model.TaskRunning, requestHeaders, requestJSON, responseHeaders, responseJSON, upstreamTaskID, upstreamStatus, progress, nextPollAt, now, id)
	if err != nil {
		fmt.Println("db mark_video_submitted failed:", "id=", id, "error=", err)
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		fmt.Println("db mark_video_submitted no_rows:", "id=", id)
		return sql.ErrNoRows
	}
	fmt.Println("db mark_video_submitted done:", "id=", id, "status=", model.TaskRunning, "rows=", count)
	return nil
}

func (s *Store) VideoTasksToPoll(ctx context.Context, limit int) ([]model.Task, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	fmt.Println("db video_tasks_to_poll query:", "limit=", limit, "now=", time.Now().UTC().Format(time.RFC3339Nano))
	rows, err := s.db.QueryContext(ctx, `SELECT `+taskDetailColumns()+` FROM tasks WHERE task_type = $1 AND status = $2 AND upstream_task_id <> '' AND (next_poll_at IS NULL OR next_poll_at <= $3) ORDER BY COALESCE(next_poll_at, updated_at) ASC LIMIT $4`, model.TaskTypeVideoGeneration, model.TaskRunning, time.Now().UTC(), limit)
	if err != nil {
		fmt.Println("db video_tasks_to_poll failed:", "error=", err)
		return nil, err
	}
	defer rows.Close()
	tasks, err := scanTasks(rows)
	if err != nil {
		fmt.Println("db video_tasks_to_poll scan_failed:", "error=", err)
		return nil, err
	}
	fmt.Println("db video_tasks_to_poll result:", "count=", len(tasks))
	for _, task := range tasks {
		fmt.Println("db video_tasks_to_poll item:", "id=", task.ID, "task_type=", task.TaskType, "status=", task.Status, "upstream_task_id=", task.UpstreamTaskID, "upstream_status=", task.UpstreamStatus, "progress=", task.UpstreamProgress, "poll_count=", task.PollCount)
	}
	return tasks, nil
}

func (s *Store) UpdateVideoPoll(ctx context.Context, id, upstreamStatus string, progress int, responseHeaders, responseJSON string, nextPollAt time.Time) error {
	now := time.Now().UTC()
	fmt.Println("db update_video_poll begin:", "id=", id, "upstream_status=", upstreamStatus, "progress=", progress, "next_poll_at=", nextPollAt.Format(time.RFC3339Nano))
	result, err := s.db.ExecContext(ctx, `UPDATE tasks SET upstream_status = $1, upstream_progress = $2, response_headers = $3, response_json = $4, next_poll_at = $5, poll_count = poll_count + 1, updated_at = $6 WHERE id = $7`, upstreamStatus, progress, responseHeaders, responseJSON, nextPollAt, now, id)
	if err != nil {
		fmt.Println("db update_video_poll failed:", "id=", id, "error=", err)
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		fmt.Println("db update_video_poll no_rows:", "id=", id)
		return sql.ErrNoRows
	}
	fmt.Println("db update_video_poll done:", "id=", id, "rows=", count)
	return nil
}

func (s *Store) CompleteVideoTask(ctx context.Context, id string, finalPrompt, responseHeaders, responseJSON string, results []model.MediaAsset, elapsedMS int64) error {
	payload, err := json.Marshal(results)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	fmt.Println("db complete_video begin:", "id=", id, "results=", len(results), "elapsed_ms=", elapsedMS)
	result, err := s.db.ExecContext(ctx, `UPDATE tasks SET status = $1, final_prompt = $2, response_headers = $3, response_json = $4, result_videos_json = $5::jsonb, upstream_status = $6, upstream_progress = 100, elapsed_ms = $7, completed_at = $8, updated_at = $9, next_poll_at = NULL, error_message = '' WHERE id = $10`, model.TaskSucceeded, finalPrompt, responseHeaders, responseJSON, string(payload), "SUCCESS", elapsedMS, now, now, id)
	if err != nil {
		fmt.Println("db complete_video failed:", "id=", id, "error=", err)
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		fmt.Println("db complete_video no_rows:", "id=", id)
		return sql.ErrNoRows
	}
	fmt.Println("db complete_video done:", "id=", id, "status=", model.TaskSucceeded, "rows=", count)
	return nil
}

func (s *Store) FailTask(ctx context.Context, id string, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, message string, elapsedMS int64) error {
	now := time.Now().UTC()
	fmt.Println("db fail_task begin:", "id=", id, "message=", message, "elapsed_ms=", elapsedMS)
	result, err := s.db.ExecContext(ctx, `UPDATE tasks SET status = $1, final_prompt = $2, request_headers = $3, request_json = $4, response_headers = $5, response_json = $6, error_message = $7, elapsed_ms = $8, completed_at = $9, updated_at = $10 WHERE id = $11`, model.TaskFailed, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, message, elapsedMS, now, now, id)
	if err != nil {
		fmt.Println("db fail_task failed:", "id=", id, "error=", err)
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		fmt.Println("db fail_task no_rows:", "id=", id)
		return sql.ErrNoRows
	}
	fmt.Println("db fail_task done:", "id=", id, "status=", model.TaskFailed, "rows=", count)
	return nil
}

func baseURLMatchPattern(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	baseURL = strings.TrimPrefix(baseURL, "http://")
	baseURL = strings.TrimPrefix(baseURL, "https://")
	return strings.TrimRight(baseURL, "/")
}

func taskColumns() string {
	return `id, api_key, base_url, task_type, status, prompt, final_prompt, model, size, quality, output_format, output_compression, background, moderation, input_fidelity, n, stream, style, response_format, reference_images_json::text, reference_videos_json::text, reference_audios_json::text, favorite, '' AS request_headers, '' AS request_json, '' AS response_headers, '' AS response_json, result_images_json::text, result_videos_json::text, upstream_task_id, upstream_status, upstream_progress, next_poll_at, poll_count, video_ratio, video_width, video_height, video_duration, generate_audio, watermark, error_message, elapsed_ms, created_at, updated_at, started_at, completed_at, CASE WHEN status = 'pending' THEN (SELECT COUNT(*) FROM tasks queued WHERE queued.status = 'pending' AND queued.created_at < tasks.created_at) ELSE 0 END, EXISTS(SELECT 1 FROM plaza_items WHERE plaza_items.task_id = tasks.id)`
}

func plazaColumns() string {
	return `id, task_id, task_type, prompt, model, size, quality, output_format, output_compression, background, moderation, input_fidelity, n, stream, style, response_format, reference_images_json::text, reference_videos_json::text, reference_audios_json::text, result_images_json::text, result_videos_json::text, video_ratio, video_width, video_height, video_duration, generate_audio, watermark, like_count, EXISTS(SELECT 1 FROM plaza_likes WHERE plaza_likes.plaza_id = plaza_items.id AND plaza_likes.client_id = $1), created_at`
}

func taskDetailColumns() string {
	return `id, api_key, base_url, task_type, status, prompt, final_prompt, model, size, quality, output_format, output_compression, background, moderation, input_fidelity, n, stream, style, response_format, reference_images_json::text, reference_videos_json::text, reference_audios_json::text, favorite, request_headers, request_json, response_headers, response_json, result_images_json::text, result_videos_json::text, upstream_task_id, upstream_status, upstream_progress, next_poll_at, poll_count, video_ratio, video_width, video_height, video_duration, generate_audio, watermark, error_message, elapsed_ms, created_at, updated_at, started_at, completed_at, CASE WHEN status = 'pending' THEN (SELECT COUNT(*) FROM tasks queued WHERE queued.status = 'pending' AND queued.created_at < tasks.created_at) ELSE 0 END, EXISTS(SELECT 1 FROM plaza_items WHERE plaza_items.task_id = tasks.id)`
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(row scanner) (*model.Task, error) {
	var task model.Task
	var startedAt, completedAt, nextPollAt sql.NullTime
	if err := row.Scan(&task.ID, &task.APIKey, &task.BaseURL, &task.TaskType, &task.Status, &task.Prompt, &task.FinalPrompt, &task.Model, &task.Size, &task.Quality, &task.OutputFormat, &task.OutputCompression, &task.Background, &task.Moderation, &task.InputFidelity, &task.N, &task.Stream, &task.Style, &task.ResponseFormat, &task.ReferenceImagesJSON, &task.ReferenceVideosJSON, &task.ReferenceAudiosJSON, &task.Favorite, &task.RequestHeaders, &task.RequestJSON, &task.ResponseHeaders, &task.ResponseJSON, &task.ResultImagesJSON, &task.ResultVideosJSON, &task.UpstreamTaskID, &task.UpstreamStatus, &task.UpstreamProgress, &nextPollAt, &task.PollCount, &task.VideoRatio, &task.VideoWidth, &task.VideoHeight, &task.VideoDuration, &task.GenerateAudio, &task.Watermark, &task.ErrorMessage, &task.ElapsedMS, &task.CreatedAt, &task.UpdatedAt, &startedAt, &completedAt, &task.QueuePosition, &task.SharedToPlaza); err != nil {
		return nil, err
	}
	if task.TaskType == "" {
		task.TaskType = model.TaskTypeImageGeneration
	}
	if nextPollAt.Valid {
		task.NextPollAt = &nextPollAt.Time
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
	if err := row.Scan(&item.ID, &item.TaskID, &item.TaskType, &item.Prompt, &item.Model, &item.Size, &item.Quality, &item.OutputFormat, &item.OutputCompression, &item.Background, &item.Moderation, &item.InputFidelity, &item.N, &item.Stream, &item.Style, &item.ResponseFormat, &item.ReferenceImagesJSON, &item.ReferenceVideosJSON, &item.ReferenceAudiosJSON, &item.ResultImagesJSON, &item.ResultVideosJSON, &item.VideoRatio, &item.VideoWidth, &item.VideoHeight, &item.VideoDuration, &item.GenerateAudio, &item.Watermark, &item.LikeCount, &item.Liked, &item.CreatedAt); err != nil {
		return nil, err
	}
	if item.TaskType == "" {
		item.TaskType = model.TaskTypeImageGeneration
	}
	if item.InputFidelity == "" {
		item.InputFidelity = "high"
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
	if task.ReferenceVideosJSON != "" {
		_ = json.Unmarshal([]byte(task.ReferenceVideosJSON), &task.ReferenceVideos)
	}
	if task.ReferenceAudiosJSON != "" {
		_ = json.Unmarshal([]byte(task.ReferenceAudiosJSON), &task.ReferenceAudios)
	}
	if task.ResultVideosJSON != "" {
		_ = json.Unmarshal([]byte(task.ResultVideosJSON), &task.ResultVideos)
	}
}

func decodePlazaJSON(item *model.PlazaItem) {
	if item.ReferenceImagesJSON != "" {
		_ = json.Unmarshal([]byte(item.ReferenceImagesJSON), &item.ReferenceImages)
	}
	if item.ReferenceVideosJSON != "" {
		_ = json.Unmarshal([]byte(item.ReferenceVideosJSON), &item.ReferenceVideos)
	}
	if item.ReferenceAudiosJSON != "" {
		_ = json.Unmarshal([]byte(item.ReferenceAudiosJSON), &item.ReferenceAudios)
	}
	if item.ResultImagesJSON != "" {
		_ = json.Unmarshal([]byte(item.ResultImagesJSON), &item.ResultImages)
	}
	if item.ResultVideosJSON != "" {
		_ = json.Unmarshal([]byte(item.ResultVideosJSON), &item.ResultVideos)
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
