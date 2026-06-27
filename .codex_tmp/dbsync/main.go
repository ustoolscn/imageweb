package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type tableInfo struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Count   int64    `json:"count"`
}

var appTables = []string{
	"workspaces",
	"tasks",
	"task_sources",
	"task_media_assets",
	"canvases",
	"plaza_items",
	"plaza_likes",
	"site_config",
}

func main() {
	log.SetFlags(0)
	mode := "inspect"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	pgDSN := os.Getenv("PG_DSN")
	myDSN := os.Getenv("MYSQL_DSN")
	if pgDSN == "" || myDSN == "" {
		log.Fatal("PG_DSN and MYSQL_DSN are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	pg, err := sql.Open("pgx", pgDSN)
	must(err)
	defer pg.Close()
	my, err := sql.Open("mysql", myDSN)
	must(err)
	defer my.Close()
	must(pg.PingContext(ctx))
	must(my.PingContext(ctx))
	switch mode {
	case "inspect":
		inspect(ctx, pg, my)
	case "migrate":
		secret := os.Getenv("APP_CREDENTIAL_KEY")
		if secret == "" {
			log.Fatal("APP_CREDENTIAL_KEY is required")
		}
		migrate(ctx, pg, my, secret)
	default:
		log.Fatalf("unknown mode %q", mode)
	}
}

func inspect(ctx context.Context, pg, my *sql.DB) {
	must(execPGReadOnly(ctx, pg))
	result := map[string]any{
		"postgres": mustTables(ctx, pg, "postgres"),
		"mysql":    mustTables(ctx, my, "mysql"),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	must(enc.Encode(result))
}

func execPGReadOnly(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, "SET default_transaction_read_only = on")
	return err
}

type syncStats struct {
	Workspaces      int `json:"workspaces"`
	Tasks           int `json:"tasks"`
	TaskSources     int `json:"task_sources"`
	TaskMediaAssets int `json:"task_media_assets"`
	PlazaItems      int `json:"plaza_items"`
	PlazaLikes      int `json:"plaza_likes"`
	SiteConfig      int `json:"site_config"`
}

type oldTask struct {
	ID                  string
	APIKey              string
	BaseURL             string
	Status              string
	Prompt              string
	FinalPrompt         string
	Model               string
	Size                string
	Quality             string
	OutputFormat        string
	OutputCompression   int
	Background          string
	Moderation          string
	N                   int
	Stream              bool
	Style               string
	ResponseFormat      string
	ReferenceImagesJSON string
	Favorite            bool
	RequestHeaders      string
	RequestJSON         string
	ResponseHeaders     string
	ResponseJSON        string
	ResultImagesJSON    string
	ErrorMessage        string
	ElapsedMS           int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
	StartedAt           sql.NullTime
	CompletedAt         sql.NullTime
	TaskType            string
	ReferenceVideosJSON string
	ReferenceAudiosJSON string
	ResultVideosJSON    string
	UpstreamTaskID      string
	UpstreamStatus      string
	UpstreamProgress    int
	NextPollAt          sql.NullTime
	PollCount           int
	VideoRatio          string
	VideoWidth          int
	VideoHeight         int
	VideoDuration       int
	GenerateAudio       bool
	Watermark           bool
}

func migrate(ctx context.Context, pg, my *sql.DB, secret string) {
	must(execPGReadOnly(ctx, pg))
	pgtx, err := pg.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	must(err)
	defer pgtx.Rollback()
	_, err = pgtx.ExecContext(ctx, "SET TRANSACTION READ ONLY")
	must(err)
	key := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	stats := syncStats{}
	workspaceIDs := map[string]string{}
	must(pgtx.Commit())
	const batchSize = 100
	skipTasks := envInt("SKIP_TASKS")
	var allTasks []oldTask
	var afterCreated time.Time
	afterID := ""
	firstBatch := true
	for {
		offset := 0
		if firstBatch {
			offset = skipTasks
			firstBatch = false
		}
		tasks := readOldTaskBatch(ctx, pg, afterCreated, afterID, batchSize, offset)
		if len(tasks) == 0 {
			break
		}
		allTasks = append(allTasks, tasks...)
		last := tasks[len(tasks)-1]
		afterCreated = last.CreatedAt
		afterID = last.ID
		tx, err := my.BeginTx(ctx, nil)
		must(err)
		func() {
			defer tx.Rollback()
			for _, task := range tasks {
				wsID := ensureWorkspace(ctx, tx, key, task.BaseURL, task.APIKey)
				workspaceIDs[workspaceKey(task.BaseURL, task.APIKey)] = wsID
				stats.Workspaces++
				upsertTask(ctx, tx, task, wsID)
				stats.Tasks++
				upsertTaskSource(ctx, tx, task)
				stats.TaskSources++
				stats.TaskMediaAssets += replaceTaskMedia(ctx, tx, task)
			}
			must(tx.Commit())
		}()
		log.Printf("tasks synced %d", stats.Tasks)
	}
	tx, err := my.BeginTx(ctx, nil)
	must(err)
	func() {
		defer tx.Rollback()
		stats.PlazaItems = migratePlazaItems(ctx, pg, tx, allTasks, workspaceIDs)
		log.Printf("plaza_items synced %d", stats.PlazaItems)
		stats.PlazaLikes = migratePlazaLikes(ctx, pg, tx)
		log.Printf("plaza_likes synced %d", stats.PlazaLikes)
		stats.SiteConfig = migrateSiteConfig(ctx, pg, tx)
		log.Printf("site_config synced %d", stats.SiteConfig)
		must(tx.Commit())
	}()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	must(enc.Encode(stats))
}

func readOldTasks(ctx context.Context, tx *sql.Tx) []oldTask {
	rows, err := tx.QueryContext(ctx, `
SELECT id, api_key, base_url, status, prompt, final_prompt, model, size, quality, output_format,
       output_compression, background, moderation, n, stream, style, response_format,
       COALESCE(reference_images_json::text, '[]'), favorite, request_headers, request_json,
       response_headers, response_json, COALESCE(result_images_json::text, '[]'), error_message,
       elapsed_ms, created_at, updated_at, started_at, completed_at, task_type,
       COALESCE(reference_videos_json::text, '[]'), COALESCE(reference_audios_json::text, '[]'),
       COALESCE(result_videos_json::text, '[]'), upstream_task_id, upstream_status, upstream_progress,
       next_poll_at, poll_count, video_ratio, video_width, video_height, video_duration,
       generate_audio, watermark
FROM tasks
ORDER BY created_at ASC, id ASC`)
	must(err)
	defer rows.Close()
	out := []oldTask{}
	for rows.Next() {
		var task oldTask
		must(rows.Scan(&task.ID, &task.APIKey, &task.BaseURL, &task.Status, &task.Prompt, &task.FinalPrompt,
			&task.Model, &task.Size, &task.Quality, &task.OutputFormat, &task.OutputCompression, &task.Background,
			&task.Moderation, &task.N, &task.Stream, &task.Style, &task.ResponseFormat, &task.ReferenceImagesJSON,
			&task.Favorite, &task.RequestHeaders, &task.RequestJSON, &task.ResponseHeaders, &task.ResponseJSON,
			&task.ResultImagesJSON, &task.ErrorMessage, &task.ElapsedMS, &task.CreatedAt, &task.UpdatedAt,
			&task.StartedAt, &task.CompletedAt, &task.TaskType, &task.ReferenceVideosJSON, &task.ReferenceAudiosJSON,
			&task.ResultVideosJSON, &task.UpstreamTaskID, &task.UpstreamStatus, &task.UpstreamProgress, &task.NextPollAt,
			&task.PollCount, &task.VideoRatio, &task.VideoWidth, &task.VideoHeight, &task.VideoDuration,
			&task.GenerateAudio, &task.Watermark))
		task.TaskType = defaultString(task.TaskType, "image_generation")
		out = append(out, task)
	}
	must(rows.Err())
	return out
}

func readOldTaskBatch(ctx context.Context, db *sql.DB, afterCreated time.Time, afterID string, limit, offset int) []oldTask {
	where := ""
	args := []any{}
	if !afterCreated.IsZero() || afterID != "" {
		where = "WHERE (created_at > $1 OR (created_at = $1 AND id > $2))"
		args = append(args, afterCreated, afterID)
	}
	args = append(args, limit)
	limitPlaceholder := len(args)
	offsetClause := ""
	if offset > 0 {
		args = append(args, offset)
		offsetClause = " OFFSET $" + fmt.Sprint(len(args))
	}
	rows, err := db.QueryContext(ctx, `
SELECT id, api_key, base_url, status, prompt, final_prompt, model, size, quality, output_format,
       output_compression, background, moderation, n, stream, style, response_format,
       COALESCE(reference_images_json::text, '[]'), favorite, request_headers, request_json,
       response_headers, response_json, COALESCE(result_images_json::text, '[]'), error_message,
       elapsed_ms, created_at, updated_at, started_at, completed_at, task_type,
       COALESCE(reference_videos_json::text, '[]'), COALESCE(reference_audios_json::text, '[]'),
       COALESCE(result_videos_json::text, '[]'), upstream_task_id, upstream_status, upstream_progress,
       next_poll_at, poll_count, video_ratio, video_width, video_height, video_duration,
       generate_audio, watermark
FROM tasks
`+where+`
ORDER BY created_at ASC, id ASC
LIMIT $`+fmt.Sprint(limitPlaceholder)+offsetClause, args...)
	must(err)
	defer rows.Close()
	out := []oldTask{}
	for rows.Next() {
		var task oldTask
		must(rows.Scan(&task.ID, &task.APIKey, &task.BaseURL, &task.Status, &task.Prompt, &task.FinalPrompt,
			&task.Model, &task.Size, &task.Quality, &task.OutputFormat, &task.OutputCompression, &task.Background,
			&task.Moderation, &task.N, &task.Stream, &task.Style, &task.ResponseFormat, &task.ReferenceImagesJSON,
			&task.Favorite, &task.RequestHeaders, &task.RequestJSON, &task.ResponseHeaders, &task.ResponseJSON,
			&task.ResultImagesJSON, &task.ErrorMessage, &task.ElapsedMS, &task.CreatedAt, &task.UpdatedAt,
			&task.StartedAt, &task.CompletedAt, &task.TaskType, &task.ReferenceVideosJSON, &task.ReferenceAudiosJSON,
			&task.ResultVideosJSON, &task.UpstreamTaskID, &task.UpstreamStatus, &task.UpstreamProgress, &task.NextPollAt,
			&task.PollCount, &task.VideoRatio, &task.VideoWidth, &task.VideoHeight, &task.VideoDuration,
			&task.GenerateAudio, &task.Watermark))
		task.TaskType = defaultString(task.TaskType, "image_generation")
		out = append(out, task)
	}
	must(rows.Err())
	return out
}

func ensureWorkspace(ctx context.Context, tx *sql.Tx, key [32]byte, baseURL, apiKey string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	apiKey = strings.TrimSpace(apiKey)
	hash := apiKeyHash(apiKey)
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM workspaces WHERE base_url = ? AND api_key_hash = ?`, baseURL, hash).Scan(&id)
	if err == nil {
		return id
	}
	if err != nil && err != sql.ErrNoRows {
		must(err)
	}
	id = deterministicID("workspace:" + baseURL + ":" + hash)
	encrypted, err := encryptAPIKey(key, apiKey)
	must(err)
	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `
INSERT INTO workspaces (id, base_url, api_key_hash, api_key_encrypted, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE base_url = VALUES(base_url), api_key_hash = VALUES(api_key_hash), updated_at = VALUES(updated_at)`,
		id, baseURL, hash, encrypted, now, now)
	must(err)
	return id
}

func upsertTask(ctx context.Context, tx *sql.Tx, task oldTask, workspaceID string) {
	_, err := tx.ExecContext(ctx, `
INSERT INTO tasks (
 id, workspace_id, task_type, status, prompt, final_prompt, model, size, quality, output_format,
 output_compression, background, moderation, input_fidelity, n, stream, style, response_format,
 favorite, upstream_task_id, upstream_status, upstream_progress, next_poll_at, poll_count,
 video_ratio, video_width, video_height, video_duration, generate_audio, video_draft, watermark,
 error_message, elapsed_ms, created_at, updated_at, started_at, completed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'high', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, false, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
 workspace_id = VALUES(workspace_id), task_type = VALUES(task_type), status = VALUES(status),
 prompt = VALUES(prompt), final_prompt = VALUES(final_prompt), model = VALUES(model), size = VALUES(size),
 quality = VALUES(quality), output_format = VALUES(output_format), output_compression = VALUES(output_compression),
 background = VALUES(background), moderation = VALUES(moderation), input_fidelity = VALUES(input_fidelity),
 n = VALUES(n), stream = VALUES(stream), style = VALUES(style), response_format = VALUES(response_format),
 favorite = VALUES(favorite), upstream_task_id = VALUES(upstream_task_id), upstream_status = VALUES(upstream_status),
 upstream_progress = VALUES(upstream_progress), next_poll_at = VALUES(next_poll_at), poll_count = VALUES(poll_count),
 video_ratio = VALUES(video_ratio), video_width = VALUES(video_width), video_height = VALUES(video_height),
 video_duration = VALUES(video_duration), generate_audio = VALUES(generate_audio), video_draft = VALUES(video_draft),
 watermark = VALUES(watermark), error_message = VALUES(error_message), elapsed_ms = VALUES(elapsed_ms),
 created_at = VALUES(created_at), updated_at = VALUES(updated_at), started_at = VALUES(started_at),
 completed_at = VALUES(completed_at)`,
		task.ID, workspaceID, task.TaskType, task.Status, task.Prompt, task.FinalPrompt, task.Model, task.Size,
		task.Quality, task.OutputFormat, task.OutputCompression, task.Background, task.Moderation, task.N,
		task.Stream, task.Style, task.ResponseFormat, task.Favorite, task.UpstreamTaskID, task.UpstreamStatus,
		task.UpstreamProgress, nullableTime(task.NextPollAt), task.PollCount, task.VideoRatio, task.VideoWidth,
		task.VideoHeight, task.VideoDuration, task.GenerateAudio, task.Watermark, task.ErrorMessage, task.ElapsedMS,
		task.CreatedAt, task.UpdatedAt, nullableTime(task.StartedAt), nullableTime(task.CompletedAt))
	must(err)
}

func upsertTaskSource(ctx context.Context, tx *sql.Tx, task oldTask) {
	_, err := tx.ExecContext(ctx, `
INSERT INTO task_sources (task_id, request_headers, request_json, response_headers, response_json, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE request_headers = VALUES(request_headers), request_json = VALUES(request_json),
 response_headers = VALUES(response_headers), response_json = VALUES(response_json), updated_at = VALUES(updated_at)`,
		task.ID, task.RequestHeaders, task.RequestJSON, task.ResponseHeaders, task.ResponseJSON, task.UpdatedAt)
	must(err)
}

func replaceTaskMedia(ctx context.Context, tx *sql.Tx, task oldTask) int {
	_, err := tx.ExecContext(ctx, `DELETE FROM task_media_assets WHERE task_id = ?`, task.ID)
	must(err)
	total := 0
	total += insertMediaArray(ctx, tx, task.ID, "reference_image", "image", task.ReferenceImagesJSON, task.CreatedAt)
	total += insertMediaArray(ctx, tx, task.ID, "reference_video", "video", task.ReferenceVideosJSON, task.CreatedAt)
	total += insertMediaArray(ctx, tx, task.ID, "reference_audio", "audio", task.ReferenceAudiosJSON, task.CreatedAt)
	total += insertMediaArray(ctx, tx, task.ID, "result_image", "image", task.ResultImagesJSON, task.CreatedAt)
	total += insertMediaArray(ctx, tx, task.ID, "result_video", "video", task.ResultVideosJSON, task.CreatedAt)
	return total
}

func insertMediaArray(ctx context.Context, tx *sql.Tx, taskID, role, assetType, raw string, createdAt time.Time) int {
	items := decodeJSONArray(raw)
	for index, item := range items {
		_, err := tx.ExecContext(ctx, `
INSERT INTO task_media_assets (
 id, task_id, role, asset_type, url, thumbnail_url, first_frame_url, last_frame_url, filename,
 node_id, reference_label, video_frame_role, mask_reference_label, mask_url, duration, clip_start,
 clip_end, width, height, original_size, compressed_size, compression_ratio, sort_order, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			deterministicID("media:"+taskID+":"+role+":"+fmt.Sprint(index)), taskID, role, assetType,
			stringField(item, "url"), stringField(item, "thumbnail_url"), stringField(item, "first_frame_url"),
			stringField(item, "last_frame_url"), stringField(item, "filename"), stringField(item, "node_id"),
			stringField(item, "reference_label"), stringField(item, "video_frame_role"),
			stringField(item, "mask_reference_label"), stringField(item, "mask_url"), intField(item, "duration"),
			intField(item, "clip_start"), intField(item, "clip_end"), intField(item, "width"), intField(item, "height"),
			int64Field(item, "original_size"), int64Field(item, "compressed_size"), floatField(item, "compression_ratio"),
			index, createdAt)
		must(err)
	}
	return len(items)
}

func migratePlazaItems(ctx context.Context, pg *sql.DB, my *sql.Tx, tasks []oldTask, workspaceIDs map[string]string) int {
	taskByID := map[string]oldTask{}
	for _, task := range tasks {
		taskByID[task.ID] = task
	}
	rows, err := pg.QueryContext(ctx, `
SELECT id, task_id, prompt, model, size, quality, output_format, output_compression, background, moderation,
       n, stream, style, response_format, COALESCE(reference_images_json::text, '[]'),
       COALESCE(result_images_json::text, '[]'), like_count, created_at, updated_at, task_type,
       COALESCE(reference_videos_json::text, '[]'), COALESCE(reference_audios_json::text, '[]'),
       COALESCE(result_videos_json::text, '[]'), video_ratio, video_width, video_height, video_duration,
       generate_audio, watermark
FROM plaza_items
ORDER BY created_at ASC, id ASC`)
	must(err)
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id, taskID, prompt, model, size, quality, outputFormat, background, moderation, style, responseFormat, refImages, resultImages, taskType, refVideos, refAudios, resultVideos, videoRatio string
		var outputCompression, n, likeCount, videoWidth, videoHeight, videoDuration int
		var stream, generateAudio, watermark bool
		var createdAt, updatedAt time.Time
		must(rows.Scan(&id, &taskID, &prompt, &model, &size, &quality, &outputFormat, &outputCompression,
			&background, &moderation, &n, &stream, &style, &responseFormat, &refImages, &resultImages,
			&likeCount, &createdAt, &updatedAt, &taskType, &refVideos, &refAudios, &resultVideos, &videoRatio,
			&videoWidth, &videoHeight, &videoDuration, &generateAudio, &watermark))
		workspaceID := ""
		if task, ok := taskByID[taskID]; ok {
			workspaceID = workspaceIDs[workspaceKey(task.BaseURL, task.APIKey)]
		}
		taskIDValue := any(nil)
		if workspaceID == "" {
			var existingWorkspaceID sql.NullString
			err := my.QueryRowContext(ctx, `SELECT workspace_id FROM tasks WHERE id = ?`, taskID).Scan(&existingWorkspaceID)
			if err == nil {
				taskIDValue = taskID
				if existingWorkspaceID.Valid {
					workspaceID = existingWorkspaceID.String
				}
			} else if err != sql.ErrNoRows {
				must(err)
			}
		} else {
			taskIDValue = taskID
		}
		if taskIDValue == nil {
			log.Printf("skip plaza_item %s: missing task %s", id, taskID)
			continue
		}
		_, err := my.ExecContext(ctx, `
INSERT INTO plaza_items (
 id, item_type, task_id, workspace_id, canvas_id, canvas_name, canvas_json, task_type, prompt, model,
 size, quality, output_format, output_compression, background, moderation, input_fidelity, n, stream,
 style, response_format, reference_images_json, reference_videos_json, reference_audios_json,
 result_images_json, result_videos_json, video_ratio, video_width, video_height, video_duration,
 generate_audio, video_draft, watermark, like_count, created_at, updated_at
) VALUES (?, 'task', ?, ?, NULL, '', '{}', ?, ?, ?, ?, ?, ?, ?, ?, ?, 'high', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, false, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE prompt = VALUES(prompt), model = VALUES(model), size = VALUES(size),
 quality = VALUES(quality), output_format = VALUES(output_format), like_count = VALUES(like_count),
 updated_at = VALUES(updated_at)`,
			id, taskIDValue, nullEmpty(workspaceID), defaultString(taskType, "image_generation"), prompt, model, size, quality,
			outputFormat, outputCompression, background, moderation, n, stream, style, responseFormat,
			jsonArray(refImages), jsonArray(refVideos), jsonArray(refAudios), jsonArray(resultImages),
			jsonArray(resultVideos), videoRatio, videoWidth, videoHeight, videoDuration, generateAudio,
			watermark, likeCount, createdAt, updatedAt)
		must(err)
		count++
	}
	must(rows.Err())
	return count
}

func migratePlazaLikes(ctx context.Context, pg *sql.DB, my *sql.Tx) int {
	rows, err := pg.QueryContext(ctx, `SELECT plaza_id, client_id, created_at FROM plaza_likes ORDER BY created_at ASC`)
	must(err)
	defer rows.Close()
	count := 0
	for rows.Next() {
		var plazaID, clientID string
		var createdAt time.Time
		must(rows.Scan(&plazaID, &clientID, &createdAt))
		result, err := my.ExecContext(ctx, `
INSERT IGNORE INTO plaza_likes (plaza_id, client_id, created_at)
SELECT ?, ?, ? WHERE EXISTS (SELECT 1 FROM plaza_items WHERE id = ?)`, plazaID, clientID, createdAt, plazaID)
		must(err)
		affected, err := result.RowsAffected()
		must(err)
		if affected > 0 {
			count++
		}
	}
	must(rows.Err())
	return count
}

func migrateSiteConfig(ctx context.Context, pg *sql.DB, my *sql.Tx) int {
	rows, err := pg.QueryContext(ctx, `SELECT config_key, value FROM site_config ORDER BY config_key`)
	must(err)
	defer rows.Close()
	count := 0
	for rows.Next() {
		var key, value string
		must(rows.Scan(&key, &value))
		_, err := my.ExecContext(ctx, `INSERT INTO site_config (config_key, value) VALUES (?, ?) ON DUPLICATE KEY UPDATE value = VALUES(value)`, key, value)
		must(err)
		count++
	}
	must(rows.Err())
	return count
}

func mustTables(ctx context.Context, db *sql.DB, kind string) []tableInfo {
	out := make([]tableInfo, 0, len(appTables))
	for _, table := range appTables {
		columns, err := columns(ctx, db, kind, table)
		if err != nil {
			out = append(out, tableInfo{Name: table, Columns: []string{"<missing: " + err.Error() + ">"}})
			continue
		}
		count, err := rowCount(ctx, db, table)
		if err != nil {
			out = append(out, tableInfo{Name: table, Columns: columns, Count: -1})
			continue
		}
		out = append(out, tableInfo{Name: table, Columns: columns, Count: count})
	}
	return out
}

func columns(ctx context.Context, db *sql.DB, kind, table string) ([]string, error) {
	var rows *sql.Rows
	var err error
	if kind == "postgres" {
		rows, err = db.QueryContext(ctx, `
SELECT column_name
FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = $1
ORDER BY ordinal_position`, table)
	} else {
		rows, err = db.QueryContext(ctx, `
SELECT column_name
FROM information_schema.columns
WHERE table_schema = DATABASE() AND table_name = ?
ORDER BY ordinal_position`, table)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		items = append(items, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("table not found")
	}
	return items, nil
}

func rowCount(ctx context.Context, db *sql.DB, table string) (int64, error) {
	if !knownTable(table) {
		return 0, fmt.Errorf("unknown table")
	}
	row := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table)
	var count int64
	return count, row.Scan(&count)
}

func apiKeyHash(apiKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(apiKey)))
	return hex.EncodeToString(sum[:])
}

func encryptAPIKey(key [32]byte, apiKey string) (string, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(apiKey), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func workspaceKey(baseURL, apiKey string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "\x00" + apiKeyHash(apiKey)
}

func deterministicID(value string) string {
	sum := sha1.Sum([]byte(value))
	hexed := hex.EncodeToString(sum[:])
	return hexed[:8] + "-" + hexed[8:12] + "-" + hexed[12:16] + "-" + hexed[16:20] + "-" + hexed[20:32]
}

func decodeJSONArray(raw string) []map[string]any {
	raw = jsonArray(raw)
	var items []map[string]any
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	return items
}

func jsonArray(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return "[]"
	}
	if !json.Valid([]byte(raw)) {
		return "[]"
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return "[]"
	}
	if _, ok := value.([]any); !ok {
		return "[]"
	}
	return raw
}

func stringField(item map[string]any, key string) string {
	if value, ok := item[key].(string); ok {
		return value
	}
	return ""
}

func intField(item map[string]any, key string) int {
	return int(int64Field(item, key))
}

func int64Field(item map[string]any, key string) int64 {
	switch value := item[key].(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	case int:
		return int64(value)
	case json.Number:
		n, _ := value.Int64()
		return n
	default:
		return 0
	}
}

func floatField(item map[string]any, key string) float64 {
	switch value := item[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case json.Number:
		n, _ := value.Float64()
		return n
	default:
		return 0
	}
}

func nullableTime(value sql.NullTime) any {
	if value.Valid {
		return value.Time
	}
	return nil
}

func nullEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func envInt(key string) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	var value int
	if _, err := fmt.Sscanf(raw, "%d", &value); err != nil || value < 0 {
		log.Fatalf("%s must be a non-negative integer", key)
	}
	return value
}

func knownTable(table string) bool {
	index := sort.SearchStrings(appTables, table)
	if index < len(appTables) && appTables[index] == table {
		return true
	}
	for _, item := range appTables {
		if item == table {
			return true
		}
	}
	return false
}

func must(err error) {
	if err != nil {
		if strings.Contains(err.Error(), "password") {
			log.Fatal("database error")
		}
		log.Fatal(err)
	}
}
