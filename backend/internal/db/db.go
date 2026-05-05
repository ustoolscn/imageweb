package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"image-web/backend/internal/model"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(dir(path), 0o755); err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
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
  output_compression INTEGER NOT NULL,
  background TEXT NOT NULL,
  moderation TEXT NOT NULL,
  n INTEGER NOT NULL,
  style TEXT NOT NULL DEFAULT '',
  response_format TEXT NOT NULL DEFAULT '',
  reference_images_json TEXT NOT NULL DEFAULT '[]',
  favorite INTEGER NOT NULL DEFAULT 0,
  request_headers TEXT NOT NULL DEFAULT '',
  request_json TEXT NOT NULL DEFAULT '',
  response_headers TEXT NOT NULL DEFAULT '',
  response_json TEXT NOT NULL DEFAULT '',
  result_images_json TEXT NOT NULL DEFAULT '[]',
  error_message TEXT NOT NULL DEFAULT '',
  elapsed_ms INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  started_at DATETIME,
  completed_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_tasks_api_key_created ON tasks(api_key, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_status_created ON tasks(status, created_at ASC);
`)
	if err != nil {
		return err
	}
	for _, statement := range []string{
		`ALTER TABLE tasks ADD COLUMN request_headers TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tasks ADD COLUMN response_headers TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tasks ADD COLUMN favorite INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, alterErr := s.db.Exec(statement); alterErr != nil && !strings.Contains(alterErr.Error(), "duplicate column") {
			return alterErr
		}
	}
	return nil
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
 output_compression, background, moderation, n, style, response_format, reference_images_json,
 request_headers, request_json, response_headers, response_json, result_images_json,
 error_message, elapsed_ms, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.APIKey, task.BaseURL, task.Status, task.Prompt, task.FinalPrompt, task.Model,
		task.Size, task.Quality, task.OutputFormat, task.OutputCompression, task.Background,
		task.Moderation, task.N, task.Style, task.ResponseFormat, string(refs), task.RequestHeaders,
		task.RequestJSON, task.ResponseHeaders, task.ResponseJSON, "[]", task.ErrorMessage,
		task.ElapsedMS, task.CreatedAt, task.UpdatedAt)
	return err
}

func (s *Store) ListTasks(ctx context.Context, apiKey, status, query string, favoriteOnly bool) ([]model.Task, error) {
	args := []any{apiKey}
	where := []string{"api_key = ?"}
	if status != "" && status != "all" {
		where = append(where, "status = ?")
		args = append(args, status)
	}
	if query != "" {
		where = append(where, "prompt LIKE ?")
		args = append(args, "%"+query+"%")
	}
	if favoriteOnly {
		where = append(where, "favorite = 1")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+taskColumns()+` FROM tasks WHERE `+strings.Join(where, " AND ")+` ORDER BY created_at DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

func (s *Store) GetTask(ctx context.Context, id, apiKey string) (*model.Task, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+taskColumns()+` FROM tasks WHERE id = ? AND api_key = ?`, id, apiKey)
	return scanTask(row)
}

func (s *Store) GetAnyTask(ctx context.Context, id string) (*model.Task, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+taskColumns()+` FROM tasks WHERE id = ?`, id)
	return scanTask(row)
}

func (s *Store) DeleteTask(ctx context.Context, id, apiKey string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ? AND api_key = ?`, id, apiKey)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) SetFavorite(ctx context.Context, id, apiKey string, favorite bool) error {
	value := 0
	if favorite {
		value = 1
	}
	result, err := s.db.ExecContext(ctx, `UPDATE tasks SET favorite = ?, updated_at = ? WHERE id = ? AND api_key = ?`, value, time.Now().UTC(), id, apiKey)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) NextPendingTask(ctx context.Context) (*model.Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `SELECT `+taskColumns()+` FROM tasks WHERE status = ? ORDER BY created_at ASC LIMIT 1`, model.TaskPending)
	task, err := scanTask(row)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `UPDATE tasks SET status = ?, started_at = ?, updated_at = ? WHERE id = ? AND status = ?`, model.TaskRunning, now, now, task.ID, model.TaskPending)
	if err != nil {
		return nil, err
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
	_, err = s.db.ExecContext(ctx, `UPDATE tasks SET status = ?, final_prompt = ?, request_headers = ?, request_json = ?, response_headers = ?, response_json = ?, result_images_json = ?, elapsed_ms = ?, completed_at = ?, updated_at = ?, error_message = '' WHERE id = ?`, model.TaskSucceeded, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, string(payload), elapsedMS, now, now, id)
	return err
}

func (s *Store) FailTask(ctx context.Context, id string, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, message string, elapsedMS int64) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET status = ?, final_prompt = ?, request_headers = ?, request_json = ?, response_headers = ?, response_json = ?, error_message = ?, elapsed_ms = ?, completed_at = ?, updated_at = ? WHERE id = ?`, model.TaskFailed, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, message, elapsedMS, now, now, id)
	return err
}

func taskColumns() string {
	return `id, api_key, base_url, status, prompt, final_prompt, model, size, quality, output_format, output_compression, background, moderation, n, style, response_format, reference_images_json, favorite, request_headers, request_json, response_headers, response_json, result_images_json, error_message, elapsed_ms, created_at, updated_at, started_at, completed_at`
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(row scanner) (*model.Task, error) {
	var task model.Task
	var startedAt, completedAt sql.NullTime
	if err := row.Scan(&task.ID, &task.APIKey, &task.BaseURL, &task.Status, &task.Prompt, &task.FinalPrompt, &task.Model, &task.Size, &task.Quality, &task.OutputFormat, &task.OutputCompression, &task.Background, &task.Moderation, &task.N, &task.Style, &task.ResponseFormat, &task.ReferenceImagesJSON, &task.Favorite, &task.RequestHeaders, &task.RequestJSON, &task.ResponseHeaders, &task.ResponseJSON, &task.ResultImagesJSON, &task.ErrorMessage, &task.ElapsedMS, &task.CreatedAt, &task.UpdatedAt, &startedAt, &completedAt); err != nil {
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

func decodeTaskJSON(task *model.Task) {
	if task.ReferenceImagesJSON != "" {
		_ = json.Unmarshal([]byte(task.ReferenceImagesJSON), &task.ReferenceImages)
	}
	if task.ResultImagesJSON != "" {
		_ = json.Unmarshal([]byte(task.ResultImagesJSON), &task.ResultImages)
	}
}

func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func dir(path string) string {
	idx := strings.LastIndexAny(path, `/\\`)
	if idx == -1 {
		return "."
	}
	return path[:idx]
}

func MustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(data)
}
