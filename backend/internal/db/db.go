package db

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"image-web/backend/internal/model"
	"image-web/backend/internal/sourceclean"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

type Store struct {
	db                     *sql.DB
	credentialKey          [32]byte
	workspaceMu            sync.Mutex
	workspaceCache         map[string]workspace
	workspaceCacheExpires  map[string]time.Time
	siteConfigMu           sync.Mutex
	siteConfigCache        model.SiteConfig
	siteConfigCacheExpires time.Time
	siteConfigCacheOK      bool
}

type GalleryUserListOptions struct {
	Query  string
	Limit  int
	Offset int
	Page   int
}

type workspace struct {
	ID              string
	BaseURL         string
	EncryptedAPIKey string
}

type sqlExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type sqlQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type sqlExecQueryer interface {
	sqlExecer
	sqlQueryer
}

type traceIDContextKey struct{}

type dbTxLog struct {
	id      uint64
	op      string
	ctx     context.Context
	started time.Time
	quiet   bool
}

const dbSlowOperationThreshold = 750 * time.Millisecond
const dbMigrationTimeout = 30 * time.Second
const dbLockTimeout = 5 * time.Second
const dbStatementTimeout = 60 * time.Second
const dbMaxOpenConns = 10
const dbMaxIdleConns = 5
const dbConnMaxIdleTime = 10 * time.Minute
const dbConnMaxLifetime = 30 * time.Minute
const workspaceCacheTTL = 10 * time.Minute
const siteConfigCacheTTL = time.Minute
const canvasCompressionMinBytes = 32 * 1024
const canvasCompressionEncoding = "gzip+base64"

var dbTxSeq uint64
var dbLogOnce sync.Once
var dbLogQueue = make(chan string, 2048)

func WithTraceID(ctx context.Context, traceID string) context.Context {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return ctx
	}
	return context.WithValue(ctx, traceIDContextKey{}, traceID)
}

func contextTraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	traceID, _ := ctx.Value(traceIDContextKey{}).(string)
	return traceID
}

func logDB(ctx context.Context, format string, args ...any) {
	dbLogOnce.Do(func() {
		go func() {
			for message := range dbLogQueue {
				log.Print(message)
			}
		}()
	})
	message := ""
	if traceID := contextTraceID(ctx); traceID != "" {
		args = append([]any{traceID}, args...)
		message = fmt.Sprintf("[db] trace=%s "+format, args...)
	} else {
		message = fmt.Sprintf("[db] "+format, args...)
	}
	select {
	case dbLogQueue <- message:
	default:
	}
}

func compactID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 14 {
		return id
	}
	return id[:8] + "..." + id[len(id)-4:]
}

func quietDBOp(op string) bool {
	return op == "task.next_pending" || op == "task.next_pending.select_for_update"
}

func applyTransactionTimeouts(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `SET innodb_lock_wait_timeout = ?`, int(dbLockTimeout.Seconds()))
	return err
}

func sqlTimeoutLiteral(timeout time.Duration) string {
	return fmt.Sprintf("'%dms'", timeout.Milliseconds())
}

func execLogged(ctx context.Context, exec sqlExecer, op string, query string, args ...any) (sql.Result, error) {
	quiet := quietDBOp(op)
	if !quiet {
		logDB(ctx, "exec begin op=%s", op)
	}
	started := time.Now()
	result, err := exec.ExecContext(ctx, query, args...)
	elapsed := time.Since(started)
	if err != nil {
		logDB(ctx, "exec error op=%s elapsed=%s err=%v", op, elapsed, err)
		return result, err
	}
	count, _ := result.RowsAffected()
	if !quiet || elapsed >= dbSlowOperationThreshold {
		logDB(ctx, "exec done op=%s elapsed=%s rows=%d", op, elapsed, count)
	}
	return result, nil
}

func scanLogged(ctx context.Context, op string, scan func() error) error {
	quiet := quietDBOp(op)
	if !quiet {
		logDB(ctx, "query begin op=%s", op)
	}
	started := time.Now()
	err := scan()
	elapsed := time.Since(started)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if !quiet || elapsed >= dbSlowOperationThreshold {
				logDB(ctx, "query no_rows op=%s elapsed=%s", op, elapsed)
			}
			return err
		}
		logDB(ctx, "query error op=%s elapsed=%s err=%v", op, elapsed, err)
		return err
	}
	if !quiet || elapsed >= dbSlowOperationThreshold {
		logDB(ctx, "query done op=%s elapsed=%s", op, elapsed)
	}
	return nil
}

func (s *Store) beginTx(ctx context.Context, op string) (*sql.Tx, *dbTxLog, error) {
	id := atomic.AddUint64(&dbTxSeq, 1)
	quiet := quietDBOp(op)
	if !quiet {
		logDB(ctx, "tx begin id=%d op=%s", id, op)
	}
	started := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		logDB(ctx, "tx begin error id=%d op=%s elapsed=%s err=%v", id, op, time.Since(started), err)
		return nil, nil, err
	}
	if err := applyTransactionTimeouts(ctx, tx); err != nil {
		_ = tx.Rollback()
		logDB(ctx, "tx setup error id=%d op=%s elapsed=%s err=%v", id, op, time.Since(started), err)
		return nil, nil, err
	}
	return tx, &dbTxLog{id: id, op: op, ctx: ctx, started: started, quiet: quiet}, nil
}

func (l *dbTxLog) Commit(tx *sql.Tx) error {
	if !l.quiet {
		logDB(l.ctx, "tx commit begin id=%d op=%s", l.id, l.op)
	}
	err := tx.Commit()
	elapsed := time.Since(l.started)
	if err != nil {
		logDB(l.ctx, "tx commit error id=%d op=%s elapsed=%s err=%v", l.id, l.op, elapsed, err)
		return err
	}
	if !l.quiet || elapsed >= dbSlowOperationThreshold {
		logDB(l.ctx, "tx commit done id=%d op=%s elapsed=%s", l.id, l.op, elapsed)
	}
	return nil
}

func (l *dbTxLog) Rollback(tx *sql.Tx) {
	err := tx.Rollback()
	if err == nil {
		elapsed := time.Since(l.started)
		if !l.quiet || elapsed >= dbSlowOperationThreshold {
			logDB(l.ctx, "tx rollback done id=%d op=%s elapsed=%s", l.id, l.op, elapsed)
		}
		return
	}
	if !errors.Is(err, sql.ErrTxDone) {
		logDB(l.ctx, "tx rollback error id=%d op=%s elapsed=%s err=%v", l.id, l.op, time.Since(l.started), err)
	}
}

func Open(dsn, credentialSecret string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("database dsn is required")
	}
	dsn = withMySQLDSNParams(dsn)
	key, err := deriveCredentialKey(credentialSecret)
	if err != nil {
		return nil, err
	}
	database, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	fmt.Println("db init: ping")
	database.SetConnMaxIdleTime(dbConnMaxIdleTime)
	database.SetConnMaxLifetime(dbConnMaxLifetime)
	database.SetMaxIdleConns(dbMaxIdleConns)
	database.SetMaxOpenConns(dbMaxOpenConns)
	log.Printf("[db] pool max_open=%d max_idle=%d max_idle_time=%s max_lifetime=%s", dbMaxOpenConns, dbMaxIdleConns, dbConnMaxIdleTime, dbConnMaxLifetime)
	pingCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	if err := database.PingContext(pingCtx); err != nil {
		cancel()
		database.Close()
		return nil, err
	}
	cancel()
	store := &Store{
		db:                    database,
		credentialKey:         key,
		workspaceCache:        map[string]workspace{},
		workspaceCacheExpires: map[string]time.Time{},
	}
	fmt.Println("db init: migrate")
	if err := store.migrate(context.Background()); err != nil {
		database.Close()
		return nil, err
	}
	fmt.Println("db init: migrate done")
	return store, nil
}

func withMySQLDSNParams(dsn string) string {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return dsn
	}
	before, query, hasQuery := strings.Cut(trimmed, "?")
	values, err := url.ParseQuery(query)
	if err != nil {
		return trimmed
	}
	if values.Get("parseTime") == "" {
		values.Set("parseTime", "true")
	}
	if values.Get("charset") == "" {
		values.Set("charset", "utf8mb4")
	}
	if values.Get("loc") == "" {
		values.Set("loc", "UTC")
	}
	if values.Get("multiStatements") == "" {
		values.Set("multiStatements", "true")
	}
	encoded := values.Encode()
	if encoded == "" && !hasQuery {
		return before
	}
	return before + "?" + encoded
}

func deriveCredentialKey(secret string) ([32]byte, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return [32]byte{}, fmt.Errorf("APP_CREDENTIAL_KEY is required")
	}
	return sha256.Sum256([]byte(secret)), nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, dbMigrationTimeout)
	defer cancel()
	if err := s.ensureNoLegacySchema(ctx); err != nil {
		return err
	}
	_, err := execLogged(ctx, s.db, "migration base schema", `
CREATE TABLE IF NOT EXISTS workspaces (
  id VARCHAR(64) PRIMARY KEY,
  base_url VARCHAR(512) NOT NULL,
  api_key_hash VARCHAR(128) NOT NULL,
  api_key_encrypted TEXT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  UNIQUE KEY idx_workspaces_base_key (base_url, api_key_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS tasks (
  id VARCHAR(64) PRIMARY KEY,
  workspace_id VARCHAR(64) NOT NULL,
  task_type VARCHAR(32) NOT NULL DEFAULT 'image_generation' CHECK (task_type IN ('image_generation', 'video_generation')),
  status VARCHAR(32) NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed')),
  prompt MEDIUMTEXT NOT NULL,
  final_prompt MEDIUMTEXT NOT NULL,
  model VARCHAR(128) NOT NULL,
  size VARCHAR(64) NOT NULL,
  quality VARCHAR(32) NOT NULL,
  output_format VARCHAR(32) NOT NULL,
  output_compression INT NOT NULL,
  background VARCHAR(32) NOT NULL,
  moderation VARCHAR(32) NOT NULL,
  input_fidelity VARCHAR(32) NOT NULL DEFAULT 'high',
  n INT NOT NULL,
  stream BOOLEAN NOT NULL DEFAULT FALSE,
  style VARCHAR(128) NOT NULL DEFAULT '',
  response_format VARCHAR(128) NOT NULL DEFAULT '',
  favorite BOOLEAN NOT NULL DEFAULT FALSE,
  upstream_task_id VARCHAR(255) NOT NULL DEFAULT '',
  upstream_status VARCHAR(64) NOT NULL DEFAULT '',
  upstream_progress INT NOT NULL DEFAULT 0,
  next_poll_at DATETIME(3),
  poll_count INT NOT NULL DEFAULT 0,
  video_ratio VARCHAR(32) NOT NULL DEFAULT '',
  video_width INT NOT NULL DEFAULT 0,
  video_height INT NOT NULL DEFAULT 0,
  video_duration INT NOT NULL DEFAULT 0,
  generate_audio BOOLEAN NOT NULL DEFAULT FALSE,
  video_draft BOOLEAN NOT NULL DEFAULT FALSE,
  watermark BOOLEAN NOT NULL DEFAULT FALSE,
  error_message MEDIUMTEXT NOT NULL,
  elapsed_ms BIGINT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  started_at DATETIME(3),
  completed_at DATETIME(3),
  INDEX idx_tasks_workspace_created (workspace_id, created_at DESC, id DESC),
  INDEX idx_tasks_workspace_status_created (workspace_id, status, created_at DESC, id DESC),
  INDEX idx_tasks_pending_queue (status, created_at ASC, id ASC),
  INDEX idx_tasks_video_poll (task_type, status, upstream_task_id, next_poll_at, updated_at, id),
  CONSTRAINT fk_tasks_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS task_sources (
  task_id VARCHAR(64) PRIMARY KEY,
  request_headers MEDIUMTEXT NOT NULL,
  request_json MEDIUMTEXT NOT NULL,
  response_headers MEDIUMTEXT NOT NULL,
  response_json MEDIUMTEXT NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  CONSTRAINT fk_task_sources_task FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS task_media_assets (
  id VARCHAR(64) PRIMARY KEY,
  task_id VARCHAR(64) NOT NULL,
  role VARCHAR(32) NOT NULL CHECK (role IN ('reference_image', 'reference_video', 'reference_audio', 'result_image', 'result_video', 'result_audio')),
  asset_type VARCHAR(32) NOT NULL DEFAULT '',
  url VARCHAR(2048) NOT NULL,
  thumbnail_url VARCHAR(2048) NOT NULL DEFAULT '',
  first_frame_url VARCHAR(2048) NOT NULL DEFAULT '',
  last_frame_url VARCHAR(2048) NOT NULL DEFAULT '',
  filename VARCHAR(512) NOT NULL DEFAULT '',
  node_id VARCHAR(128) NOT NULL DEFAULT '',
  reference_label VARCHAR(128) NOT NULL DEFAULT '',
  video_frame_role VARCHAR(64) NOT NULL DEFAULT '',
  mask_reference_label VARCHAR(128) NOT NULL DEFAULT '',
  mask_url VARCHAR(2048) NOT NULL DEFAULT '',
  duration INT NOT NULL DEFAULT 0,
  clip_start INT NOT NULL DEFAULT 0,
  clip_end INT NOT NULL DEFAULT 0,
  width INT NOT NULL DEFAULT 0,
  height INT NOT NULL DEFAULT 0,
  original_size BIGINT NOT NULL DEFAULT 0,
  compressed_size BIGINT NOT NULL DEFAULT 0,
  compression_ratio DOUBLE NOT NULL DEFAULT 0,
  sort_order INT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  INDEX idx_task_media_task_role_order (task_id, role, sort_order, created_at),
  CONSTRAINT fk_task_media_task FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS canvases (
  workspace_id VARCHAR(64) NOT NULL,
  canvas_id VARCHAR(128) NOT NULL,
  name VARCHAR(255) NOT NULL DEFAULT '',
  canvas_json JSON NOT NULL,
  sort_order INT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (workspace_id, canvas_id),
  INDEX idx_canvases_workspace_order (workspace_id, sort_order, canvas_id),
  CONSTRAINT fk_canvases_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS plaza_items (
  id VARCHAR(64) PRIMARY KEY,
  item_type VARCHAR(32) NOT NULL DEFAULT 'task' CHECK (item_type IN ('task', 'canvas')),
  task_id VARCHAR(64),
  workspace_id VARCHAR(64),
  canvas_id VARCHAR(128),
  canvas_name VARCHAR(255) NOT NULL DEFAULT '',
  canvas_json JSON NOT NULL,
  task_type VARCHAR(32) NOT NULL DEFAULT 'image_generation' CHECK (task_type IN ('image_generation', 'video_generation')),
  prompt MEDIUMTEXT NOT NULL,
  model VARCHAR(128) NOT NULL,
  size VARCHAR(64) NOT NULL,
  quality VARCHAR(32) NOT NULL,
  output_format VARCHAR(32) NOT NULL,
  output_compression INT NOT NULL,
  background VARCHAR(32) NOT NULL,
  moderation VARCHAR(32) NOT NULL,
  input_fidelity VARCHAR(32) NOT NULL DEFAULT 'high',
  n INT NOT NULL,
  stream BOOLEAN NOT NULL DEFAULT FALSE,
  style VARCHAR(128) NOT NULL DEFAULT '',
  response_format VARCHAR(128) NOT NULL DEFAULT '',
  reference_images_json JSON NOT NULL,
  reference_videos_json JSON NOT NULL,
  reference_audios_json JSON NOT NULL,
  result_images_json JSON NOT NULL,
  result_videos_json JSON NOT NULL,
  video_ratio VARCHAR(32) NOT NULL DEFAULT '',
  video_width INT NOT NULL DEFAULT 0,
  video_height INT NOT NULL DEFAULT 0,
  video_duration INT NOT NULL DEFAULT 0,
  generate_audio BOOLEAN NOT NULL DEFAULT FALSE,
  video_draft BOOLEAN NOT NULL DEFAULT FALSE,
  watermark BOOLEAN NOT NULL DEFAULT FALSE,
  like_count INT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  CHECK (
    (item_type = 'task' AND task_id IS NOT NULL AND canvas_id IS NULL)
    OR
    (item_type = 'canvas' AND task_id IS NULL AND workspace_id IS NOT NULL AND canvas_id IS NOT NULL)
  ),
  UNIQUE KEY idx_plaza_task_unique (task_id),
  UNIQUE KEY idx_plaza_canvas_unique (workspace_id, canvas_id),
  INDEX idx_plaza_created (created_at DESC, id DESC),
  INDEX idx_plaza_likes (like_count DESC, created_at DESC, id DESC),
  CONSTRAINT fk_plaza_task FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
  CONSTRAINT fk_plaza_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_plaza_canvas FOREIGN KEY (workspace_id, canvas_id) REFERENCES canvases(workspace_id, canvas_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS plaza_likes (
  plaza_id VARCHAR(64) NOT NULL,
  client_id VARCHAR(128) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (plaza_id, client_id),
  CONSTRAINT fk_plaza_likes_item FOREIGN KEY (plaza_id) REFERENCES plaza_items(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS site_config (
  config_key VARCHAR(128) PRIMARY KEY,
  value TEXT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS gallery_assets (
  sha256 CHAR(64) NOT NULL PRIMARY KEY,
  url VARCHAR(2048) NOT NULL,
  thumbnail_url VARCHAR(2048) NOT NULL DEFAULT '',
  first_frame_url VARCHAR(2048) NOT NULL DEFAULT '',
  last_frame_url VARCHAR(2048) NOT NULL DEFAULT '',
  filename VARCHAR(512) NOT NULL DEFAULT '',
  content_type VARCHAR(255) NOT NULL DEFAULT '',
  etag VARCHAR(255) NOT NULL DEFAULT '',
  size BIGINT NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`)
	if err != nil {
		return err
	}
	return s.ensureSiteConfig(ctx)
}
func (s *Store) ensureNoLegacySchema(ctx context.Context) error {
	columns, err := s.loadSchemaColumns(ctx, []string{"tasks", "canvases", "plaza_items", "task_media_assets"})
	if err != nil {
		return err
	}
	hasTable := func(table string) bool {
		_, exists := columns[table]
		return exists
	}
	hasColumn := func(table, column string) bool {
		tableColumns, exists := columns[table]
		return exists && tableColumns[column]
	}
	checks := []struct {
		table  string
		column string
	}{
		{"tasks", "api_key"},
		{"tasks", "reference_images_json"},
		{"canvases", "api_key"},
		{"canvases", "canvases_json"},
	}
	for _, check := range checks {
		if hasColumn(check.table, check.column) {
			return fmt.Errorf("检测到旧数据库结构 %s.%s；本版本只支持空库或最新结构，请清空/重建数据库后启动", check.table, check.column)
		}
	}
	for _, table := range []string{"tasks", "canvases", "plaza_items"} {
		if !hasTable(table) {
			continue
		}
		requiredColumn := "workspace_id"
		if table == "plaza_items" {
			requiredColumn = "canvas_id"
		}
		if !hasColumn(table, requiredColumn) {
			return fmt.Errorf("检测到旧数据库结构 %s；本版本不做兼容迁移，请清空/重建数据库后启动", table)
		}
	}
	if hasTable("task_media_assets") && !hasColumn("task_media_assets", "first_frame_url") {
		return fmt.Errorf("检测到旧数据库结构 task_media_assets；本版本不做兼容迁移，请清空/重建数据库后启动")
	}
	return nil
}

func (s *Store) loadSchemaColumns(ctx context.Context, tables []string) (map[string]map[string]bool, error) {
	started := time.Now()
	logDB(ctx, "query begin op=migration.load_schema_columns tables=%s", strings.Join(tables, ","))
	placeholders := make([]string, 0, len(tables))
	args := make([]any, 0, len(tables))
	for _, table := range tables {
		args = append(args, table)
		placeholders = append(placeholders, placeholder(len(args)))
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT table_name, column_name
FROM information_schema.columns
WHERE table_schema = DATABASE()
  AND table_name IN (`+strings.Join(placeholders, ",")+`)
ORDER BY table_name, ordinal_position`, args...)
	if err != nil {
		logDB(ctx, "query error op=migration.load_schema_columns elapsed=%s err=%v", time.Since(started), err)
		return nil, err
	}
	defer rows.Close()
	columns := map[string]map[string]bool{}
	for rows.Next() {
		table := ""
		column := ""
		if err := rows.Scan(&table, &column); err != nil {
			logDB(ctx, "query error op=migration.load_schema_columns elapsed=%s err=%v", time.Since(started), err)
			return nil, err
		}
		if columns[table] == nil {
			columns[table] = map[string]bool{}
		}
		columns[table][column] = true
	}
	if err := rows.Err(); err != nil {
		logDB(ctx, "query error op=migration.load_schema_columns elapsed=%s err=%v", time.Since(started), err)
		return nil, err
	}
	logDB(ctx, "query done op=migration.load_schema_columns elapsed=%s tables=%d", time.Since(started), len(columns))
	return columns, nil
}

func (s *Store) tableExists(ctx context.Context, table string) (bool, error) {
	var exists bool
	err := scanLogged(ctx, "migration.table_exists table="+table, func() error {
		return s.db.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM information_schema.tables
  WHERE table_schema = DATABASE() AND table_name = ?
)`, table).Scan(&exists)
	})
	return exists, err
}

func (s *Store) columnExists(ctx context.Context, table, column string) (bool, error) {
	var exists bool
	err := scanLogged(ctx, "migration.column_exists table="+table+" column="+column, func() error {
		return s.db.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1 FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?
)`, table, column).Scan(&exists)
	})
	return exists, err
}

func (s *Store) ensureSiteConfig(ctx context.Context) error {
	_, err := execLogged(ctx, s.db, "migration site_config defaults", `
INSERT IGNORE INTO site_config (config_key, value)
VALUES
  ('baseurl_whitelist_enabled', 'false'),
  ('baseurl_whitelist', '[]'),
  ('admin_contact_image', ''),
  ('site_title', '图片生成工作台'),
  ('site_icon', 'AI'),
  ('worker_concurrency', '1')`)
	return err
}

func (s *Store) SiteConfig(ctx context.Context) (model.SiteConfig, error) {
	now := time.Now()
	s.siteConfigMu.Lock()
	if s.siteConfigCacheOK && now.Before(s.siteConfigCacheExpires) {
		config := s.siteConfigCache
		s.siteConfigMu.Unlock()
		return config, nil
	}
	s.siteConfigMu.Unlock()

	started := time.Now()
	config := model.SiteConfig{}
	rows, err := s.db.QueryContext(ctx, `SELECT config_key, value FROM site_config WHERE config_key IN ('baseurl_whitelist_enabled', 'baseurl_whitelist', 'admin_contact_image', 'site_title', 'site_icon', 'worker_concurrency')`)
	if err != nil {
		logDB(ctx, "site_config query error elapsed=%s err=%v", time.Since(started), err)
		if cached, ok := s.cachedSiteConfigFallback(); ok {
			return cached, nil
		}
		return model.SiteConfig{}, err
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
	if err := rows.Err(); err != nil {
		logDB(ctx, "site_config scan error elapsed=%s err=%v", time.Since(started), err)
		if cached, ok := s.cachedSiteConfigFallback(); ok {
			return cached, nil
		}
		return model.SiteConfig{}, err
	}
	s.siteConfigMu.Lock()
	s.siteConfigCache = config
	s.siteConfigCacheExpires = time.Now().Add(siteConfigCacheTTL)
	s.siteConfigCacheOK = true
	s.siteConfigMu.Unlock()
	logDB(ctx, "site_config loaded elapsed=%s ttl=%s", time.Since(started), siteConfigCacheTTL)
	return config, nil
}

func (s *Store) cachedSiteConfigFallback() (model.SiteConfig, bool) {
	s.siteConfigMu.Lock()
	defer s.siteConfigMu.Unlock()
	if !s.siteConfigCacheOK {
		return model.SiteConfig{}, false
	}
	return s.siteConfigCache, true
}

func (s *Store) resolveWorkspace(ctx context.Context, apiKey, baseURL string) (workspace, error) {
	apiKey = strings.TrimSpace(apiKey)
	baseURLKey := baseURLStorageKey(baseURL)
	if apiKey == "" || baseURLKey == "" {
		return workspace{}, fmt.Errorf("缺少 baseurl 或 apikey")
	}
	hash := apiKeyHash(apiKey)
	cacheKey := workspaceCacheKey(baseURLKey, hash)
	if ws, ok := s.cachedWorkspace(cacheKey); ok {
		return ws, nil
	}
	var ws workspace
	err := scanLogged(ctx, "workspace.resolve", func() error {
		return s.db.QueryRowContext(ctx, `SELECT id, base_url, api_key_encrypted FROM workspaces WHERE base_url = ? AND api_key_hash = ?`, baseURLKey, hash).Scan(&ws.ID, &ws.BaseURL, &ws.EncryptedAPIKey)
	})
	if err == nil {
		if _, err := s.decryptAPIKey(ws.EncryptedAPIKey); err != nil {
			return workspace{}, fmt.Errorf("workspace api key 解密失败，请检查 APP_CREDENTIAL_KEY 是否与创建该 workspace 时一致")
		}
		s.cacheWorkspace(cacheKey, ws)
		return ws, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return workspace{}, err
	}
	encrypted, err := s.encryptAPIKey(apiKey)
	if err != nil {
		return workspace{}, err
	}
	now := time.Now().UTC()
	ws = workspace{ID: uuid.NewString(), BaseURL: baseURLKey, EncryptedAPIKey: encrypted}
	result, err := execLogged(ctx, s.db, "workspace.insert", `
INSERT IGNORE INTO workspaces (id, base_url, api_key_hash, api_key_encrypted, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
`, ws.ID, ws.BaseURL, hash, encrypted, now, now)
	if err != nil {
		return workspace{}, err
	}
	if count, _ := result.RowsAffected(); count > 0 {
		s.cacheWorkspace(cacheKey, ws)
		return ws, nil
	}
	err = scanLogged(ctx, "workspace.resolve_after_conflict", func() error {
		return s.db.QueryRowContext(ctx, `SELECT id, base_url, api_key_encrypted FROM workspaces WHERE base_url = ? AND api_key_hash = ?`, baseURLKey, hash).Scan(&ws.ID, &ws.BaseURL, &ws.EncryptedAPIKey)
	})
	if err != nil {
		return workspace{}, err
	}
	if _, err := s.decryptAPIKey(ws.EncryptedAPIKey); err != nil {
		return workspace{}, fmt.Errorf("workspace api key 解密失败，请检查 APP_CREDENTIAL_KEY 是否与创建该 workspace 时一致")
	}
	s.cacheWorkspace(cacheKey, ws)
	return ws, nil
}

func workspaceCacheKey(baseURLKey, apiKeyHash string) string {
	return baseURLKey + "\x00" + apiKeyHash
}

func (s *Store) cachedWorkspace(cacheKey string) (workspace, bool) {
	s.workspaceMu.Lock()
	defer s.workspaceMu.Unlock()
	expires, ok := s.workspaceCacheExpires[cacheKey]
	if !ok || time.Now().After(expires) {
		delete(s.workspaceCache, cacheKey)
		delete(s.workspaceCacheExpires, cacheKey)
		return workspace{}, false
	}
	return s.workspaceCache[cacheKey], true
}

func (s *Store) cacheWorkspace(cacheKey string, ws workspace) {
	s.workspaceMu.Lock()
	if s.workspaceCache == nil {
		s.workspaceCache = map[string]workspace{}
	}
	if s.workspaceCacheExpires == nil {
		s.workspaceCacheExpires = map[string]time.Time{}
	}
	s.workspaceCache[cacheKey] = ws
	s.workspaceCacheExpires[cacheKey] = time.Now().Add(workspaceCacheTTL)
	s.workspaceMu.Unlock()
}

func apiKeyHash(apiKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(apiKey)))
	return hex.EncodeToString(sum[:])
}

func (s *Store) encryptAPIKey(apiKey string) (string, error) {
	block, err := aes.NewCipher(s.credentialKey[:])
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

func (s *Store) decryptAPIKey(encrypted string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.credentialKey[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("stored api key is invalid")
	}
	nonce := raw[:gcm.NonceSize()]
	ciphertext := raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (s *Store) CanvasState(ctx context.Context, apiKey, baseURL string) (model.CanvasState, error) {
	ws, err := s.resolveWorkspace(ctx, apiKey, baseURL)
	if err != nil {
		return model.CanvasState{}, err
	}
	return s.canvasStateByWorkspace(ctx, ws.ID)
}

func (s *Store) CanvasStateByWorkspace(ctx context.Context, workspaceID string) (model.CanvasState, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return model.CanvasState{}, fmt.Errorf("缺少 workspace_id")
	}
	return s.canvasStateByWorkspace(ctx, workspaceID)
}

func (s *Store) canvasStateByWorkspace(ctx context.Context, workspaceID string) (model.CanvasState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT canvas_json, updated_at FROM canvases WHERE workspace_id = ? ORDER BY sort_order ASC, updated_at ASC, canvas_id ASC`, workspaceID)
	if err != nil {
		return model.CanvasState{}, err
	}
	defer rows.Close()
	items := []json.RawMessage{}
	var latest time.Time
	for rows.Next() {
		var raw string
		var updatedAt time.Time
		if err := rows.Scan(&raw, &updatedAt); err != nil {
			return model.CanvasState{}, err
		}
		canvas, err := unpackCanvasFromStorage(json.RawMessage(raw))
		if err != nil {
			return model.CanvasState{}, err
		}
		if json.Valid(canvas) {
			items = append(items, canvas)
		}
		if updatedAt.After(latest) {
			latest = updatedAt
		}
	}
	if err := rows.Err(); err != nil {
		return model.CanvasState{}, err
	}
	payload, err := json.Marshal(items)
	if err != nil {
		return model.CanvasState{}, err
	}
	return model.CanvasState{Canvases: payload, UpdatedAt: latest}, nil
}

func (s *Store) SaveCanvasState(ctx context.Context, apiKey, baseURL string, canvases []byte) (model.CanvasState, error) {
	started := time.Now()
	items, err := decodeCanvasArray(canvases)
	if err != nil {
		return model.CanvasState{}, err
	}
	logDB(ctx, "canvas.save_full begin canvases=%d bytes=%d", len(items), len(canvases))
	ws, err := s.resolveWorkspace(ctx, apiKey, baseURL)
	if err != nil {
		return model.CanvasState{}, err
	}
	ids := make([]string, 0, len(items))
	now := time.Now().UTC()
	for index, canvas := range items {
		id := jsonObjectID(canvas)
		if id == "" {
			return model.CanvasState{}, fmt.Errorf("画布数据必须包含 id")
		}
		ids = append(ids, id)
		if err := upsertCanvasRow(ctx, s.db, ws.ID, id, jsonObjectString(canvas, "name"), canvas, index, now); err != nil {
			return model.CanvasState{}, err
		}
	}
	if err := deleteMissingCanvases(ctx, s.db, ws.ID, ids); err != nil {
		return model.CanvasState{}, err
	}
	payload, err := json.Marshal(items)
	if err != nil {
		return model.CanvasState{}, err
	}
	logDB(ctx, "canvas.save_full done canvases=%d elapsed=%s", len(items), time.Since(started))
	return model.CanvasState{Canvases: payload, UpdatedAt: now}, nil
}

func (s *Store) PatchCanvasState(ctx context.Context, apiKey, baseURL string, changed []json.RawMessage, patches []model.CanvasItemPatch, deletedIDs []string) (model.CanvasState, error) {
	started := time.Now()
	logDB(ctx, "canvas.patch begin changed=%d patches=%d deleted=%d", len(changed), len(patches), len(deletedIDs))
	ws, err := s.resolveWorkspace(ctx, apiKey, baseURL)
	if err != nil {
		return model.CanvasState{}, err
	}
	deleted := map[string]bool{}
	for _, id := range deletedIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			deleted[id] = true
		}
	}
	changedByID := map[string]json.RawMessage{}
	changedOrder := []string{}
	for _, canvas := range changed {
		id := jsonObjectID(canvas)
		if id == "" {
			continue
		}
		if _, exists := changedByID[id]; !exists {
			changedOrder = append(changedOrder, id)
		}
		changedByID[id] = canvas
		delete(deleted, id)
	}
	patchByID := map[string]model.CanvasItemPatch{}
	patchOrder := []string{}
	for _, patch := range patches {
		patch.ID = strings.TrimSpace(patch.ID)
		if patch.ID == "" {
			continue
		}
		if _, exists := patchByID[patch.ID]; !exists {
			patchOrder = append(patchOrder, patch.ID)
		}
		patchByID[patch.ID] = patch
		delete(deleted, patch.ID)
	}

	for id := range deleted {
		if _, err := execLogged(ctx, s.db, "canvas.delete_row canvas_id="+compactID(id), `DELETE FROM canvases WHERE workspace_id = ? AND canvas_id = ?`, ws.ID, id); err != nil {
			return model.CanvasState{}, err
		}
	}
	now := time.Now().UTC()
	nextSort := 0
	nextSortLoaded := false
	consumeNextSort := func() (int, error) {
		if !nextSortLoaded {
			var err error
			nextSort, err = nextCanvasSortOrder(ctx, s.db, ws.ID)
			if err != nil {
				return 0, err
			}
			nextSortLoaded = true
		}
		sortOrder := nextSort
		nextSort++
		return sortOrder, nil
	}
	for _, id := range changedOrder {
		canvas := changedByID[id]
		if err := upsertCanvasRowPreserveSort(ctx, s.db, ws.ID, id, jsonObjectString(canvas, "name"), canvas, now); err != nil {
			return model.CanvasState{}, err
		}
	}
	for _, id := range patchOrder {
		if _, replaced := changedByID[id]; replaced {
			continue
		}
		patch := patchByID[id]
		var currentRaw string
		var sortOrder int
		err := scanLogged(ctx, "canvas.current_snapshot canvas_id="+compactID(id), func() error {
			return s.db.QueryRowContext(ctx, `SELECT canvas_json, sort_order FROM canvases WHERE workspace_id = ? AND canvas_id = ?`, ws.ID, id).Scan(&currentRaw, &sortOrder)
		})
		var canvas json.RawMessage
		if errors.Is(err, sql.ErrNoRows) {
			canvas, err = canvasFromPatch(patch)
			if err != nil {
				return model.CanvasState{}, err
			}
			sortOrder, err = consumeNextSort()
			if err != nil {
				return model.CanvasState{}, err
			}
		} else if err != nil {
			return model.CanvasState{}, err
		} else {
			currentCanvas, err := unpackCanvasFromStorage(json.RawMessage(currentRaw))
			if err != nil {
				return model.CanvasState{}, err
			}
			canvas, err = applyCanvasPatch(currentCanvas, patch)
			if err != nil {
				return model.CanvasState{}, err
			}
		}
		if err := upsertCanvasRow(ctx, s.db, ws.ID, id, jsonObjectString(canvas, "name"), canvas, sortOrder, now); err != nil {
			return model.CanvasState{}, err
		}
	}

	logDB(ctx, "canvas.patch done changed=%d patches=%d deleted=%d sort_loaded=%t elapsed=%s", len(changed), len(patches), len(deletedIDs), nextSortLoaded, time.Since(started))
	return model.CanvasState{UpdatedAt: now}, nil
}

func decodeCanvasArray(raw []byte) ([]json.RawMessage, error) {
	items := []json.RawMessage{}
	if strings.TrimSpace(string(raw)) == "" {
		return items, nil
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	for _, canvas := range items {
		if jsonObjectID(canvas) == "" {
			return nil, fmt.Errorf("画布数据必须包含 id")
		}
	}
	return items, nil
}

func lockCanvasScope(ctx context.Context, tx *sql.Tx, workspaceID string) error {
	_, err := execLogged(ctx, tx, "canvas.lock_workspace workspace_id="+compactID(workspaceID), `SELECT id FROM workspaces WHERE id = ? FOR UPDATE`, workspaceID)
	return err
}

func upsertCanvasRow(ctx context.Context, exec sqlExecer, workspaceID, canvasID, name string, canvas json.RawMessage, sortOrder int, now time.Time) error {
	storedCanvas, err := packCanvasForStorage(canvas)
	if err != nil {
		return err
	}
	_, err = execLogged(ctx, exec, fmt.Sprintf("canvas.upsert_row canvas_id=%s bytes=%d stored_bytes=%d", compactID(canvasID), len(canvas), len(storedCanvas)), `
INSERT INTO canvases (workspace_id, canvas_id, name, canvas_json, sort_order, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE name = VALUES(name), canvas_json = VALUES(canvas_json), sort_order = VALUES(sort_order), updated_at = VALUES(updated_at)
`, workspaceID, canvasID, name, string(storedCanvas), sortOrder, now, now)
	return err
}

func upsertCanvasRowPreserveSort(ctx context.Context, exec sqlExecQueryer, workspaceID, canvasID, name string, canvas json.RawMessage, now time.Time) error {
	storedCanvas, err := packCanvasForStorage(canvas)
	if err != nil {
		return err
	}
	result, err := execLogged(ctx, exec, fmt.Sprintf("canvas.update_row canvas_id=%s bytes=%d stored_bytes=%d", compactID(canvasID), len(canvas), len(storedCanvas)), `
UPDATE canvases
SET name = ?, canvas_json = ?, updated_at = ?
WHERE workspace_id = ? AND canvas_id = ?
`, name, string(storedCanvas), now, workspaceID, canvasID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count > 0 {
		return nil
	}
	sortOrder, err := nextCanvasSortOrder(ctx, exec, workspaceID)
	if err != nil {
		return err
	}
	return upsertStoredCanvasRow(ctx, exec, workspaceID, canvasID, name, storedCanvas, sortOrder, now, len(canvas))
}

func upsertStoredCanvasRow(ctx context.Context, exec sqlExecer, workspaceID, canvasID, name string, storedCanvas json.RawMessage, sortOrder int, now time.Time, originalBytes int) error {
	_, err := execLogged(ctx, exec, fmt.Sprintf("canvas.upsert_row canvas_id=%s bytes=%d stored_bytes=%d", compactID(canvasID), originalBytes, len(storedCanvas)), `
INSERT INTO canvases (workspace_id, canvas_id, name, canvas_json, sort_order, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE name = VALUES(name), canvas_json = VALUES(canvas_json), sort_order = VALUES(sort_order), updated_at = VALUES(updated_at)
`, workspaceID, canvasID, name, string(storedCanvas), sortOrder, now, now)
	return err
}

func deleteMissingCanvases(ctx context.Context, exec sqlExecer, workspaceID string, keepIDs []string) error {
	if len(keepIDs) == 0 {
		_, err := execLogged(ctx, exec, "canvas.delete_missing all", `DELETE FROM canvases WHERE workspace_id = ?`, workspaceID)
		return err
	}
	args := []any{workspaceID}
	placeholders := make([]string, 0, len(keepIDs))
	for _, id := range keepIDs {
		args = append(args, id)
		placeholders = append(placeholders, placeholder(len(args)))
	}
	_, err := execLogged(ctx, exec, fmt.Sprintf("canvas.delete_missing keep=%d", len(keepIDs)), `DELETE FROM canvases WHERE workspace_id = ? AND canvas_id NOT IN (`+strings.Join(placeholders, ",")+`)`, args...)
	return err
}

func nextCanvasSortOrder(ctx context.Context, query sqlQueryer, workspaceID string) (int, error) {
	var next int
	if err := scanLogged(ctx, "canvas.next_sort_order", func() error {
		return query.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order) + 1, 0) FROM canvases WHERE workspace_id = ?`, workspaceID).Scan(&next)
	}); err != nil {
		return 0, err
	}
	return next, nil
}

func currentCanvasSortOrder(ctx context.Context, query sqlQueryer, workspaceID, canvasID string) (int, bool, error) {
	var sortOrder int
	err := scanLogged(ctx, "canvas.current_sort_order canvas_id="+compactID(canvasID), func() error {
		return query.QueryRowContext(ctx, `SELECT sort_order FROM canvases WHERE workspace_id = ? AND canvas_id = ?`, workspaceID, canvasID).Scan(&sortOrder)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return sortOrder, true, nil
}

func packCanvasForStorage(canvas json.RawMessage) (json.RawMessage, error) {
	if len(canvas) < canvasCompressionMinBytes {
		return canvas, nil
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(canvas); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	wrapper, err := json.Marshal(map[string]any{
		"__image_web_canvas_storage": canvasCompressionEncoding,
		"size":                       len(canvas),
		"data":                       base64.StdEncoding.EncodeToString(compressed.Bytes()),
	})
	if err != nil {
		return nil, err
	}
	if len(wrapper) >= len(canvas) {
		return canvas, nil
	}
	return json.RawMessage(wrapper), nil
}

func unpackCanvasFromStorage(raw json.RawMessage) (json.RawMessage, error) {
	var wrapper struct {
		Storage string `json:"__image_web_canvas_storage"`
		Data    string `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil || wrapper.Storage != canvasCompressionEncoding {
		return raw, nil
	}
	compressed, err := base64.StdEncoding.DecodeString(wrapper.Data)
	if err != nil {
		return nil, err
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	canvas, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if !json.Valid(canvas) {
		return nil, fmt.Errorf("compressed canvas payload is invalid JSON")
	}
	return json.RawMessage(canvas), nil
}

func applyCanvasPatch(canvas json.RawMessage, patch model.CanvasItemPatch) (json.RawMessage, error) {
	data := map[string]json.RawMessage{}
	if err := json.Unmarshal(canvas, &data); err != nil {
		data = map[string]json.RawMessage{}
	}
	id, err := json.Marshal(patch.ID)
	if err != nil {
		return nil, err
	}
	data["id"] = id
	if patch.Name != nil {
		name, err := json.Marshal(*patch.Name)
		if err != nil {
			return nil, err
		}
		data["name"] = name
	}
	if len(patch.Elements) > 0 || len(patch.DeletedElementIDs) > 0 {
		elements, err := mergeJSONArrayByID(data["elements"], patch.Elements, patch.DeletedElementIDs)
		if err != nil {
			return nil, err
		}
		data["elements"] = elements
	}
	if len(patch.Connections) > 0 || len(patch.DeletedConnectionIDs) > 0 {
		connections, err := mergeJSONArrayByID(data["connections"], patch.Connections, patch.DeletedConnectionIDs)
		if err != nil {
			return nil, err
		}
		data["connections"] = connections
	}
	return json.Marshal(data)
}

func canvasFromPatch(patch model.CanvasItemPatch) (json.RawMessage, error) {
	data := map[string]json.RawMessage{}
	id, err := json.Marshal(patch.ID)
	if err != nil {
		return nil, err
	}
	name := "画布"
	if patch.Name != nil {
		name = *patch.Name
	}
	nameRaw, err := json.Marshal(name)
	if err != nil {
		return nil, err
	}
	elements, err := mergeJSONArrayByID(nil, patch.Elements, nil)
	if err != nil {
		return nil, err
	}
	connections, err := mergeJSONArrayByID(nil, patch.Connections, nil)
	if err != nil {
		return nil, err
	}
	data["id"] = id
	data["name"] = nameRaw
	data["elements"] = elements
	data["connections"] = connections
	return json.Marshal(data)
}

func mergeJSONArrayByID(currentRaw json.RawMessage, changed []json.RawMessage, deletedIDs []string) (json.RawMessage, error) {
	current := []json.RawMessage{}
	if len(currentRaw) > 0 {
		if err := json.Unmarshal(currentRaw, &current); err != nil {
			current = []json.RawMessage{}
		}
	}
	deleted := map[string]bool{}
	for _, id := range deletedIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			deleted[id] = true
		}
	}
	changedByID := map[string]json.RawMessage{}
	changedOrder := []string{}
	for _, item := range changed {
		id := jsonObjectID(item)
		if id == "" {
			continue
		}
		if _, exists := changedByID[id]; !exists {
			changedOrder = append(changedOrder, id)
		}
		changedByID[id] = item
		delete(deleted, id)
	}
	next := []json.RawMessage{}
	used := map[string]bool{}
	for _, item := range current {
		id := jsonObjectID(item)
		if id != "" && deleted[id] {
			continue
		}
		if replacement, ok := changedByID[id]; ok {
			next = append(next, replacement)
			used[id] = true
			continue
		}
		next = append(next, item)
	}
	for _, id := range changedOrder {
		if !used[id] {
			next = append(next, changedByID[id])
		}
	}
	return json.Marshal(next)
}

func jsonObjectID(raw json.RawMessage) string {
	var data struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return ""
	}
	return strings.TrimSpace(data.ID)
}

func jsonObjectString(raw json.RawMessage, key string) string {
	data := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return ""
	}
	valueRaw, ok := data[key]
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(valueRaw, &value); err != nil {
		return ""
	}
	return strings.TrimSpace(value)
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
	ws, err := s.resolveWorkspace(ctx, task.APIKey, task.BaseURL)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	task.WorkspaceID = ws.ID
	task.BaseURL = ws.BaseURL
	task.CreatedAt = now
	task.UpdatedAt = now
	if task.TaskType == "" {
		task.TaskType = model.TaskTypeImageGeneration
	}
	if task.InputFidelity == "" {
		task.InputFidelity = "high"
	}
	ensureTaskSlices(task)
	tx, txLog, err := s.beginTx(ctx, "task.create id="+compactID(task.ID))
	if err != nil {
		return err
	}
	defer txLog.Rollback(tx)
	result, err := execLogged(ctx, tx, "task.insert id="+compactID(task.ID), `INSERT INTO tasks (
	 id, workspace_id, task_type, status, prompt, final_prompt, model, size, quality, output_format,
	 output_compression, background, moderation, input_fidelity, n, stream, style, response_format,
	 favorite, upstream_task_id, upstream_status, upstream_progress, next_poll_at, poll_count,
	 video_ratio, video_width, video_height, video_duration, generate_audio, video_draft, watermark,
	 error_message, elapsed_ms, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.WorkspaceID, task.TaskType, task.Status, task.Prompt, task.FinalPrompt, task.Model,
		task.Size, task.Quality, task.OutputFormat, task.OutputCompression, task.Background,
		task.Moderation, task.InputFidelity, task.N, task.Stream, task.Style, task.ResponseFormat,
		task.Favorite, task.UpstreamTaskID, task.UpstreamStatus, task.UpstreamProgress,
		task.NextPollAt, task.PollCount, task.VideoRatio, task.VideoWidth, task.VideoHeight, task.VideoDuration,
		task.GenerateAudio, task.Draft, task.Watermark, task.ErrorMessage, task.ElapsedMS, task.CreatedAt, task.UpdatedAt)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	if err := replaceTaskImageAssets(ctx, tx, task.ID, "reference_image", task.ReferenceImages, now); err != nil {
		return err
	}
	if err := replaceTaskMediaAssets(ctx, tx, task.ID, "reference_video", task.ReferenceVideos, now); err != nil {
		return err
	}
	if err := replaceTaskMediaAssets(ctx, tx, task.ID, "reference_audio", task.ReferenceAudios, now); err != nil {
		return err
	}
	if err := txLog.Commit(tx); err != nil {
		return err
	}
	if task.RequestHeaders != "" || task.RequestJSON != "" || task.ResponseHeaders != "" || task.ResponseJSON != "" {
		saveTaskSourceBestEffort(ctx, s.db, task.ID, task.RequestHeaders, task.RequestJSON, task.ResponseHeaders, task.ResponseJSON, now)
	}
	return nil
}

func (s *Store) ListTasks(ctx context.Context, apiKey, baseURL, status, query, beforeCreatedAt, beforeID string, favoriteOnly bool, limit int) ([]model.Task, int, error) {
	ws, err := s.resolveWorkspace(ctx, apiKey, baseURL)
	if err != nil {
		return nil, 0, err
	}
	return s.listTasksByWorkspace(ctx, ws.ID, status, query, beforeCreatedAt, beforeID, favoriteOnly, limit)
}

func (s *Store) ListTasksByWorkspace(ctx context.Context, workspaceID, status, query, beforeCreatedAt, beforeID string, favoriteOnly bool, limit int) ([]model.Task, int, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, 0, fmt.Errorf("缺少 workspace_id")
	}
	return s.listTasksByWorkspace(ctx, workspaceID, status, query, beforeCreatedAt, beforeID, favoriteOnly, limit)
}

func (s *Store) listTasksByWorkspace(ctx context.Context, workspaceID, status, query, beforeCreatedAt, beforeID string, favoriteOnly bool, limit int) ([]model.Task, int, error) {
	args := []any{workspaceID}
	where := []string{"t.workspace_id = ?"}
	if status != "" && status != "all" {
		args = append(args, status)
		where = append(where, "t.status = "+placeholder(len(args)))
	}
	if query != "" {
		pattern := "%" + strings.ToLower(query) + "%"
		args = append(args, pattern, pattern, pattern, pattern)
		where = append(where, "(LOWER(t.prompt) LIKE ? OR LOWER(t.final_prompt) LIKE ? OR LOWER(t.model) LIKE ? OR LOWER(t.task_type) LIKE ?)")
	}
	if favoriteOnly {
		where = append(where, "t.favorite = TRUE")
	}
	whereClause := strings.Join(where, " AND ")
	total := 0
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks t WHERE `+whereClause, args...).Scan(&total); err != nil {
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
		listWhere = append(listWhere, fmt.Sprintf("(t.created_at < %s OR (t.created_at = %s AND t.id < %s))", placeholder(len(listArgs)-2), placeholder(len(listArgs)-1), placeholder(len(listArgs))))
	}
	if limit <= 0 || limit > 60 {
		limit = 30
	}
	listArgs = append(listArgs, limit+1)
	rows, err := s.db.QueryContext(ctx, `SELECT `+taskColumns(false)+` FROM `+taskFromClause(false)+` WHERE `+strings.Join(listWhere, " AND ")+` ORDER BY t.created_at DESC, t.id DESC LIMIT `+placeholder(len(listArgs)), listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	tasks, err := s.scanTasks(rows)
	if err != nil {
		return nil, 0, err
	}
	return tasks, total, s.attachTaskMedia(ctx, tasks)
}

func (s *Store) GetTask(ctx context.Context, id, apiKey, baseURL string) (*model.Task, error) {
	ws, err := s.resolveWorkspace(ctx, apiKey, baseURL)
	if err != nil {
		return nil, err
	}
	return s.getTaskByWorkspace(ctx, id, ws.ID)
}

func (s *Store) GetTaskByWorkspace(ctx context.Context, workspaceID, id string) (*model.Task, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("缺少 workspace_id")
	}
	return s.getTaskByWorkspace(ctx, id, workspaceID)
}

func (s *Store) getTaskByWorkspace(ctx context.Context, id, workspaceID string) (*model.Task, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+taskColumns(true)+` FROM `+taskFromClause(true)+` WHERE t.id = ? AND t.workspace_id = ?`, id, workspaceID)
	task, err := s.scanTask(row)
	if err != nil {
		return nil, err
	}
	return task, s.attachTaskMediaToTask(ctx, task)
}

func (s *Store) GetAnyTask(ctx context.Context, id string) (*model.Task, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+taskColumns(true)+` FROM `+taskFromClause(true)+` WHERE t.id = ?`, id)
	task, err := s.scanTask(row)
	if err != nil {
		return nil, err
	}
	return task, s.attachTaskMediaToTask(ctx, task)
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
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM plaza_items WHERE task_id = ?`, id).Scan(&plazaID); err == nil {
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
	_, err = execLogged(ctx, s.db, "plaza.task.insert task_id="+compactID(task.ID), `INSERT INTO plaza_items (
	 id, item_type, task_id, workspace_id, canvas_id, canvas_name, canvas_json, task_type, prompt, model, size, quality, output_format, output_compression,
	 background, moderation, input_fidelity, n, stream, style, response_format, reference_images_json,
	 reference_videos_json, reference_audios_json, result_images_json, result_videos_json,
	 video_ratio, video_width, video_height, video_duration, generate_audio, video_draft, watermark,
	 like_count, created_at, updated_at
	) VALUES (?, 'task', ?, ?, NULL, '', '{}', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		plazaID, task.ID, task.WorkspaceID, task.TaskType, task.Prompt, task.Model, task.Size, task.Quality, task.OutputFormat,
		task.OutputCompression, task.Background, task.Moderation, task.InputFidelity, task.N, task.Stream, task.Style,
		task.ResponseFormat, string(refs), string(refVideos), string(refAudios), string(results), string(resultVideos),
		task.VideoRatio, task.VideoWidth, task.VideoHeight, task.VideoDuration, task.GenerateAudio, task.Draft, task.Watermark,
		now, now)
	if err != nil {
		return nil, err
	}
	return s.PlazaItem(ctx, plazaID, "")
}

func (s *Store) ShareCanvasToPlaza(ctx context.Context, apiKey, baseURL, name string, canvas []byte) (*model.PlazaItem, error) {
	if !json.Valid(canvas) {
		return nil, fmt.Errorf("画布数据不是有效 JSON")
	}
	ws, err := s.resolveWorkspace(ctx, apiKey, baseURL)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(canvas, &raw); err != nil {
		return nil, fmt.Errorf("画布数据必须是对象")
	}
	elements, _ := raw["elements"].([]any)
	connections, _ := raw["connections"].([]any)
	canvasID := ""
	if rawID, ok := raw["id"].(string); ok {
		canvasID = strings.TrimSpace(rawID)
	}
	if canvasID == "" {
		return nil, fmt.Errorf("画布缺少 id")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		if rawName, ok := raw["name"].(string); ok {
			name = strings.TrimSpace(rawName)
		}
	}
	if name == "" {
		name = "未命名画布"
	}
	prompt := fmt.Sprintf("%s · %d 个节点 · %d 条连线", name, len(elements), len(connections))
	now := time.Now().UTC()
	tx, txLog, err := s.beginTx(ctx, "plaza.share_canvas canvas_id="+compactID(canvasID))
	if err != nil {
		return nil, err
	}
	defer txLog.Rollback(tx)
	if err := lockCanvasScope(ctx, tx, ws.ID); err != nil {
		return nil, err
	}
	sortOrder, exists, err := currentCanvasSortOrder(ctx, tx, ws.ID, canvasID)
	if err != nil {
		return nil, err
	}
	if !exists {
		sortOrder, err = nextCanvasSortOrder(ctx, tx, ws.ID)
		if err != nil {
			return nil, err
		}
	}
	if err := upsertCanvasRow(ctx, tx, ws.ID, canvasID, name, canvas, sortOrder, now); err != nil {
		return nil, err
	}
	plazaID := ""
	err = scanLogged(ctx, "plaza.canvas.select_for_update canvas_id="+compactID(canvasID), func() error {
		return tx.QueryRowContext(ctx, `SELECT id FROM plaza_items WHERE item_type = 'canvas' AND workspace_id = ? AND canvas_id = ? FOR UPDATE`, ws.ID, canvasID).Scan(&plazaID)
	})
	if err == nil {
		if _, err := execLogged(ctx, tx, "plaza.canvas.update plaza_id="+compactID(plazaID), `UPDATE plaza_items SET canvas_name = ?, canvas_json = ?, prompt = ?, updated_at = ? WHERE id = ?`, name, string(canvas), prompt, now, plazaID); err != nil {
			return nil, err
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		plazaID = uuid.NewString()
		_, err = execLogged(ctx, tx, "plaza.canvas.insert plaza_id="+compactID(plazaID), `INSERT INTO plaza_items (
		 id, item_type, task_id, workspace_id, canvas_id, canvas_name, canvas_json, task_type, prompt, model, size, quality,
		 output_format, output_compression, background, moderation, input_fidelity, n, stream, style,
		 response_format, reference_images_json, reference_videos_json, reference_audios_json,
		 result_images_json, result_videos_json, video_ratio, video_width, video_height, video_duration,
		 generate_audio, video_draft, watermark, like_count, created_at, updated_at
		) VALUES (?, 'canvas', NULL, ?, ?, ?, ?, 'image_generation', ?, 'canvas', '', '', '', 0, '', '', 'high', 1, false, '', '', '[]', '[]', '[]', '[]', '[]', '', 0, 0, 0, false, false, false, 0, ?, ?)`,
			plazaID, ws.ID, canvasID, name, string(canvas), prompt, now, now)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}
	if err := txLog.Commit(tx); err != nil {
		return nil, err
	}
	return s.PlazaItem(ctx, plazaID, "")
}

func (s *Store) UnshareCanvasFromPlaza(ctx context.Context, apiKey, baseURL, canvasID string) error {
	canvasID = strings.TrimSpace(canvasID)
	if canvasID == "" {
		return fmt.Errorf("缺少画布 id")
	}
	ws, err := s.resolveWorkspace(ctx, apiKey, baseURL)
	if err != nil {
		return err
	}
	_, err = execLogged(ctx, s.db, "plaza.canvas.delete canvas_id="+compactID(canvasID), `DELETE FROM plaza_items WHERE item_type = 'canvas' AND workspace_id = ? AND canvas_id = ?`, ws.ID, canvasID)
	return err
}

func (s *Store) UnshareTaskFromPlaza(ctx context.Context, id, apiKey, baseURL string) error {
	task, err := s.GetTask(ctx, id, apiKey, baseURL)
	if err != nil {
		return err
	}
	result, err := execLogged(ctx, s.db, "plaza.task.delete task_id="+compactID(task.ID), `DELETE FROM plaza_items WHERE item_type = 'task' AND task_id = ?`, task.ID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) TaskUpdates(ctx context.Context, apiKey, baseURL string, ids []string) ([]model.TaskUpdate, error) {
	if len(ids) == 0 {
		return []model.TaskUpdate{}, nil
	}
	ws, err := s.resolveWorkspace(ctx, apiKey, baseURL)
	if err != nil {
		return nil, err
	}
	args := []any{ws.ID}
	placeholders := make([]string, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
		placeholders = append(placeholders, placeholder(len(args)))
	}
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.status, t.error_message, t.elapsed_ms, t.updated_at, t.started_at, t.completed_at, CASE WHEN t.status = 'pending' THEN (SELECT COUNT(*) FROM tasks queued WHERE queued.status = 'pending' AND queued.created_at < t.created_at) ELSE 0 END, t.upstream_status, t.upstream_progress FROM tasks t WHERE t.workspace_id = ? AND t.id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	updates := []model.TaskUpdate{}
	for rows.Next() {
		var update model.TaskUpdate
		var startedAt, completedAt sql.NullTime
		if err := rows.Scan(&update.ID, &update.Status, &update.ErrorMessage, &update.ElapsedMS, &update.UpdatedAt, &startedAt, &completedAt, &update.QueuePosition, &update.UpstreamStatus, &update.UpstreamProgress); err != nil {
			return nil, err
		}
		if startedAt.Valid {
			update.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			update.CompletedAt = &completedAt.Time
		}
		updates = append(updates, update)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return updates, s.attachTaskUpdateResultMedia(ctx, updates)
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
		for range 7 {
			args = append(args, pattern)
			countArgs = append(countArgs, pattern)
		}
		where = append(where, "(LOWER(prompt) LIKE ? OR LOWER(model) LIKE ? OR LOWER(size) LIKE ? OR LOWER(quality) LIKE ? OR LOWER(output_format) LIKE ? OR LOWER(background) LIKE ? OR LOWER(canvas_name) LIKE ?)")
		countWhere = append(countWhere, "(LOWER(prompt) LIKE ? OR LOWER(model) LIKE ? OR LOWER(size) LIKE ? OR LOWER(quality) LIKE ? OR LOWER(output_format) LIKE ? OR LOWER(background) LIKE ? OR LOWER(canvas_name) LIKE ?)")
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
	row := s.db.QueryRowContext(ctx, `SELECT `+plazaColumns()+` FROM plaza_items WHERE id = ?`, clientID, id)
	return scanPlazaItem(row)
}

func (s *Store) SetPlazaLike(ctx context.Context, id, clientID string, liked bool) (*model.PlazaItem, error) {
	if strings.TrimSpace(clientID) == "" {
		return nil, fmt.Errorf("缺少 client_id")
	}
	tx, txLog, err := s.beginTx(ctx, "plaza.like id="+compactID(id))
	if err != nil {
		return nil, err
	}
	defer txLog.Rollback(tx)
	var exists bool
	if err := scanLogged(ctx, "plaza.like.exists id="+compactID(id), func() error {
		return tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM plaza_items WHERE id = ?)`, id).Scan(&exists)
	}); err != nil {
		return nil, err
	}
	if !exists {
		return nil, sql.ErrNoRows
	}
	if liked {
		result, err := execLogged(ctx, tx, "plaza.like.insert id="+compactID(id), `INSERT IGNORE INTO plaza_likes (plaza_id, client_id, created_at) VALUES (?, ?, ?)`, id, clientID, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		if count, _ := result.RowsAffected(); count > 0 {
			if _, err := execLogged(ctx, tx, "plaza.like_count.increment id="+compactID(id), `UPDATE plaza_items SET like_count = like_count + 1, updated_at = ? WHERE id = ?`, time.Now().UTC(), id); err != nil {
				return nil, err
			}
		}
	} else {
		result, err := execLogged(ctx, tx, "plaza.like.delete id="+compactID(id), `DELETE FROM plaza_likes WHERE plaza_id = ? AND client_id = ?`, id, clientID)
		if err != nil {
			return nil, err
		}
		if count, _ := result.RowsAffected(); count > 0 {
			if _, err := execLogged(ctx, tx, "plaza.like_count.decrement id="+compactID(id), `UPDATE plaza_items SET like_count = GREATEST(like_count - 1, 0), updated_at = ? WHERE id = ?`, time.Now().UTC(), id); err != nil {
				return nil, err
			}
		}
	}
	if err := txLog.Commit(tx); err != nil {
		return nil, err
	}
	return s.PlazaItem(ctx, id, clientID)
}

func (s *Store) DeleteTask(ctx context.Context, id, apiKey, baseURL string) error {
	ws, err := s.resolveWorkspace(ctx, apiKey, baseURL)
	if err != nil {
		return err
	}
	result, err := execLogged(ctx, s.db, "task.delete id="+compactID(id), `DELETE FROM tasks WHERE id = ? AND workspace_id = ?`, id, ws.ID)
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
	ws, err := s.resolveWorkspace(ctx, apiKey, baseURL)
	if err != nil {
		return err
	}
	result, err := execLogged(ctx, s.db, "task.favorite id="+compactID(id), `UPDATE tasks SET favorite = ?, updated_at = ? WHERE id = ? AND workspace_id = ?`, favorite, time.Now().UTC(), id, ws.ID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) FailStaleImageTasks(ctx context.Context, maxAge time.Duration) error {
	cutoff := time.Now().UTC().Add(-maxAge)
	now := time.Now().UTC()
	message := fmt.Sprintf("图片生成超过 %d 分钟，已自动断开并标记失败；上游可能仍在处理，请检查上游记录后手动重试", int(maxAge.Minutes()))
	_, err := execLogged(ctx, s.db, "task.fail_stale_image", `UPDATE tasks SET status = ?, error_message = ?, elapsed_ms = GREATEST(elapsed_ms, TIMESTAMPDIFF(MICROSECOND, started_at, ?) DIV 1000), completed_at = ?, updated_at = ? WHERE status = ? AND task_type = ? AND started_at < ?`, model.TaskFailed, message, now, now, now, model.TaskRunning, model.TaskTypeImageGeneration, cutoff)
	return err
}

func (s *Store) NextPendingTask(ctx context.Context) (*model.Task, error) {
	tx, txLog, err := s.beginTx(ctx, "task.next_pending")
	if err != nil {
		return nil, err
	}
	defer txLog.Rollback(tx)

	var task *model.Task
	err = scanLogged(ctx, "task.next_pending.select_for_update", func() error {
		var scanErr error
		task, scanErr = s.scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns(false)+` FROM `+taskFromClause(false)+` WHERE t.status = ? ORDER BY t.created_at ASC LIMIT 1 FOR UPDATE SKIP LOCKED`, model.TaskPending))
		return scanErr
	})
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	result, err := execLogged(ctx, tx, "task.next_pending.mark_running id="+compactID(task.ID), `UPDATE tasks SET status = ?, started_at = ?, updated_at = ? WHERE id = ? AND status = ?`, model.TaskRunning, now, now, task.ID, model.TaskPending)
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
	if err := txLog.Commit(tx); err != nil {
		return nil, err
	}
	return task, s.attachTaskMediaToTask(ctx, task)
}

func (s *Store) CompleteTask(ctx context.Context, id string, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON string, results []model.UploadedImage, elapsedMS int64) error {
	now := time.Now().UTC()
	tx, txLog, err := s.beginTx(ctx, "task.complete_image id="+compactID(id))
	if err != nil {
		return err
	}
	defer txLog.Rollback(tx)
	result, err := execLogged(ctx, tx, "task.complete_image.update id="+compactID(id), `UPDATE tasks SET status = ?, final_prompt = ?, elapsed_ms = ?, completed_at = ?, updated_at = ?, error_message = '' WHERE id = ?`, model.TaskSucceeded, finalPrompt, elapsedMS, now, now, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	if err := replaceTaskImageAssets(ctx, tx, id, "result_image", results, now); err != nil {
		return err
	}
	if err := txLog.Commit(tx); err != nil {
		return err
	}
	saveTaskSourceBestEffort(ctx, s.db, id, requestHeaders, requestJSON, responseHeaders, responseJSON, now)
	return nil
}

func (s *Store) MarkVideoSubmitted(ctx context.Context, id, upstreamTaskID, upstreamStatus string, progress int, requestHeaders, requestJSON, responseHeaders, responseJSON string) error {
	now := time.Now().UTC()
	nextPollAt := now.Add(5 * time.Second)
	tx, txLog, err := s.beginTx(ctx, "task.video_submitted id="+compactID(id))
	if err != nil {
		return err
	}
	defer txLog.Rollback(tx)
	result, err := execLogged(ctx, tx, "task.video_submitted.update id="+compactID(id), `UPDATE tasks SET status = ?, upstream_task_id = ?, upstream_status = ?, upstream_progress = ?, next_poll_at = ?, poll_count = 0, updated_at = ? WHERE id = ?`, model.TaskRunning, upstreamTaskID, upstreamStatus, progress, nextPollAt, now, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	if err := txLog.Commit(tx); err != nil {
		return err
	}
	saveTaskSourceBestEffort(ctx, s.db, id, requestHeaders, requestJSON, responseHeaders, responseJSON, now)
	return nil
}

func (s *Store) VideoTasksToPoll(ctx context.Context, limit int) ([]model.Task, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+taskColumns(true)+` FROM `+taskFromClause(true)+` WHERE t.task_type = ? AND t.status = ? AND t.upstream_task_id <> '' AND (t.next_poll_at IS NULL OR t.next_poll_at <= ?) ORDER BY COALESCE(t.next_poll_at, t.updated_at) ASC LIMIT ?`, model.TaskTypeVideoGeneration, model.TaskRunning, time.Now().UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks, err := s.scanTasks(rows)
	if err != nil {
		return nil, err
	}
	return tasks, s.attachTaskMedia(ctx, tasks)
}

func (s *Store) UpdateVideoPoll(ctx context.Context, id, upstreamStatus string, progress int, responseHeaders, responseJSON string, nextPollAt time.Time) error {
	now := time.Now().UTC()
	tx, txLog, err := s.beginTx(ctx, "task.video_poll_update id="+compactID(id))
	if err != nil {
		return err
	}
	defer txLog.Rollback(tx)
	result, err := execLogged(ctx, tx, "task.video_poll_update.update id="+compactID(id), `UPDATE tasks SET upstream_status = ?, upstream_progress = ?, next_poll_at = ?, poll_count = poll_count + 1, updated_at = ? WHERE id = ?`, upstreamStatus, progress, nextPollAt, now, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	if err := txLog.Commit(tx); err != nil {
		return err
	}
	updateTaskResponseSourceBestEffort(ctx, s.db, id, responseHeaders, responseJSON, now)
	return nil
}

func (s *Store) CompleteVideoTask(ctx context.Context, id string, finalPrompt, responseHeaders, responseJSON string, results []model.MediaAsset, elapsedMS int64) error {
	now := time.Now().UTC()
	tx, txLog, err := s.beginTx(ctx, "task.complete_video id="+compactID(id))
	if err != nil {
		return err
	}
	defer txLog.Rollback(tx)
	result, err := execLogged(ctx, tx, "task.complete_video.update id="+compactID(id), `UPDATE tasks SET status = ?, final_prompt = ?, upstream_status = ?, upstream_progress = 100, elapsed_ms = ?, completed_at = ?, updated_at = ?, next_poll_at = NULL, error_message = '' WHERE id = ?`, model.TaskSucceeded, finalPrompt, "SUCCESS", elapsedMS, now, now, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	if err := replaceTaskMediaAssets(ctx, tx, id, "result_video", results, now); err != nil {
		return err
	}
	if err := txLog.Commit(tx); err != nil {
		return err
	}
	updateTaskResponseSourceBestEffort(ctx, s.db, id, responseHeaders, responseJSON, now)
	return nil
}

func (s *Store) FindGalleryAssetBySHA256(ctx context.Context, sha256Hex string) (model.UploadedImage, bool, error) {
	sha256Hex = strings.TrimSpace(strings.ToLower(sha256Hex))
	if sha256Hex == "" {
		return model.UploadedImage{}, false, nil
	}
	var image model.UploadedImage
	err := s.db.QueryRowContext(ctx, `
SELECT url, thumbnail_url, first_frame_url, last_frame_url, filename, content_type, etag, size
FROM gallery_assets
WHERE sha256 = ?`, sha256Hex).Scan(&image.URL, &image.ThumbnailURL, &image.FirstFrameURL, &image.LastFrameURL, &image.Filename, &image.ContentType, &image.ETag, &image.OriginalSize)
	if errors.Is(err, sql.ErrNoRows) {
		return model.UploadedImage{}, false, nil
	}
	if err != nil {
		return model.UploadedImage{}, false, err
	}
	image.SHA256 = sha256Hex
	image.CompressedSize = image.OriginalSize
	image.Deduplicated = true
	return image, true, nil
}

func (s *Store) SaveGalleryAsset(ctx context.Context, sha256Hex string, image model.UploadedImage) error {
	sha256Hex = strings.TrimSpace(strings.ToLower(sha256Hex))
	if sha256Hex == "" || image.URL == "" {
		return nil
	}
	now := time.Now().UTC()
	_, err := execLogged(ctx, s.db, "gallery.asset.save sha256="+compactID(sha256Hex), `
INSERT INTO gallery_assets (
  sha256, url, thumbnail_url, first_frame_url, last_frame_url, filename, content_type, etag, size, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE sha256 = sha256`,
		sha256Hex, image.URL, image.ThumbnailURL, image.FirstFrameURL, image.LastFrameURL, image.Filename,
		image.ContentType, image.ETag, image.OriginalSize, now, now)
	return err
}

func normalizeGalleryUserListOptions(options GalleryUserListOptions) GalleryUserListOptions {
	options.Query = strings.TrimSpace(options.Query)
	if options.Limit <= 0 {
		options.Limit = 30
	}
	if options.Limit > 100 {
		options.Limit = 100
	}
	if options.Offset < 0 {
		options.Offset = 0
	}
	if options.Page <= 0 {
		options.Page = options.Offset/options.Limit + 1
	} else {
		options.Offset = (options.Page - 1) * options.Limit
	}
	return options
}

func (s *Store) ListGalleryUsers(ctx context.Context, options GalleryUserListOptions) ([]model.GalleryUser, int, error) {
	options = normalizeGalleryUserListOptions(options)
	where := ""
	args := []any{}
	if options.Query != "" {
		where = "WHERE w.base_url LIKE ? OR w.api_key_hash LIKE ?"
		like := "%" + options.Query + "%"
		args = append(args, like, like)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM (
	SELECT w.id
	FROM workspaces w
	JOIN tasks t ON t.workspace_id = w.id
	JOIN task_media_assets m ON m.task_id = t.id
	`+where+`
	GROUP BY w.id
) gallery_users`, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, options.Limit, options.Offset)
	rows, err := s.db.QueryContext(ctx, `
SELECT w.id, w.base_url, w.api_key_hash, w.api_key_encrypted, COUNT(m.id) AS asset_count, MAX(m.created_at) AS last_asset_at, w.created_at, w.updated_at
FROM workspaces w
JOIN tasks t ON t.workspace_id = w.id
JOIN task_media_assets m ON m.task_id = t.id
`+where+`
GROUP BY w.id, w.base_url, w.api_key_hash, w.api_key_encrypted, w.created_at, w.updated_at
ORDER BY COALESCE(MAX(m.created_at), w.updated_at) DESC, w.created_at DESC
LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	users := []model.GalleryUser{}
	for rows.Next() {
		var user model.GalleryUser
		var lastAssetAt sql.NullTime
		var encryptedAPIKey string
		if err := rows.Scan(&user.WorkspaceID, &user.BaseURL, &user.APIKeyHash, &encryptedAPIKey, &user.AssetCount, &lastAssetAt, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return nil, 0, err
		}
		if apiKey, err := s.decryptAPIKey(encryptedAPIKey); err == nil {
			user.APIKeyLabel = maskAPIKey(apiKey)
			tokenUser, _ := fetchTokenUser(ctx, user.BaseURL, apiKey)
			user.UserID = tokenUser.UserID
			user.Username = tokenUser.Username
		}
		if user.APIKeyLabel == "" {
			user.APIKeyLabel = compactID(user.APIKeyHash)
		}
		if lastAssetAt.Valid {
			user.LastAssetAt = &lastAssetAt.Time
		}
		users = append(users, user)
	}
	return users, total, rows.Err()
}

type tokenUserInfo struct {
	UserID   int64
	Username string
}

func fetchTokenUser(ctx context.Context, baseURL, apiKey string) (tokenUserInfo, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	apiKey = strings.TrimSpace(apiKey)
	if baseURL == "" || apiKey == "" {
		return tokenUserInfo{}, fmt.Errorf("缺少 baseurl 或 apikey")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/token/user", nil)
	if err != nil {
		return tokenUserInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return tokenUserInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tokenUserInfo{}, fmt.Errorf("token user status %d", resp.StatusCode)
	}
	var body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			UserID   int64  `json:"user_id"`
			Username string `json:"username"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return tokenUserInfo{}, err
	}
	if !body.Success {
		return tokenUserInfo{}, fmt.Errorf("token user failed: %s", body.Message)
	}
	return tokenUserInfo{UserID: body.Data.UserID, Username: strings.TrimSpace(body.Data.Username)}, nil
}

func maskAPIKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= 12 {
		return value
	}
	return string(runes[:8]) + "..." + string(runes[len(runes)-4:])
}

func (s *Store) ListGalleryAssetsByWorkspace(ctx context.Context, workspaceID string) ([]model.GalleryAssetRecord, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("缺少 workspace_id")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT m.id, t.workspace_id, w.base_url,
       CASE WHEN m.role IN ('result_image', 'result_video', 'result_audio') THEN 'oss'
            WHEN m.original_size > 0 OR m.compressed_size > 0 THEN 'oss'
            ELSE 'external' END AS asset_kind,
       CASE WHEN m.role IN ('result_image', 'result_video', 'result_audio') THEN 'ai_generated'
            WHEN m.original_size > 0 OR m.compressed_size > 0 THEN 'user_upload'
            ELSE 'external_url' END AS source_type,
       m.asset_type, m.role, m.url, m.thumbnail_url, m.first_frame_url, m.last_frame_url, m.filename,
       '', '', '', COALESCE(NULLIF(m.original_size, 0), m.compressed_size, 0), FALSE,
       t.id, t.task_type, t.prompt, t.final_prompt, t.model, t.size, t.quality, t.output_format,
       t.background, t.moderation, t.input_fidelity, t.video_ratio, t.video_width, t.video_height,
       t.video_duration, t.generate_audio, t.video_draft, t.watermark, m.created_at
FROM task_media_assets m
JOIN tasks t ON t.id = m.task_id
JOIN workspaces w ON w.id = t.workspace_id
WHERE t.workspace_id = ?
ORDER BY m.created_at DESC, m.sort_order ASC, m.id DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []model.GalleryAssetRecord{}
	for rows.Next() {
		var record model.GalleryAssetRecord
		if err := rows.Scan(&record.ID, &record.WorkspaceID, &record.BaseURL, &record.AssetKind, &record.SourceType, &record.MediaType, &record.Role, &record.URL,
			&record.ThumbnailURL, &record.FirstFrameURL, &record.LastFrameURL, &record.Filename, &record.SHA256, &record.ETag, &record.ContentType,
			&record.Size, &record.Deduplicated, &record.TaskID, &record.TaskType, &record.Prompt, &record.FinalPrompt, &record.Model,
			&record.SizeParam, &record.Quality, &record.OutputFormat, &record.Background, &record.Moderation, &record.InputFidelity,
			&record.VideoRatio, &record.VideoWidth, &record.VideoHeight, &record.VideoDuration, &record.GenerateAudio, &record.Draft,
			&record.Watermark, &record.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) FailTask(ctx context.Context, id string, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, message string, elapsedMS int64) error {
	now := time.Now().UTC()
	tx, txLog, err := s.beginTx(ctx, "task.fail id="+compactID(id))
	if err != nil {
		return err
	}
	defer txLog.Rollback(tx)
	result, err := execLogged(ctx, tx, "task.fail.update id="+compactID(id), `UPDATE tasks SET status = ?, final_prompt = ?, error_message = ?, elapsed_ms = ?, completed_at = ?, updated_at = ? WHERE id = ?`, model.TaskFailed, finalPrompt, message, elapsedMS, now, now, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	if err := txLog.Commit(tx); err != nil {
		return err
	}
	saveTaskSourceBestEffort(ctx, s.db, id, requestHeaders, requestJSON, responseHeaders, responseJSON, now)
	return nil
}

func replaceTaskSource(ctx context.Context, exec sqlExecer, taskID, requestHeaders, requestJSON, responseHeaders, responseJSON string, now time.Time) error {
	requestJSON = sourceclean.CompactStringForStorage(requestJSON)
	responseJSON = sourceclean.CompactStringForStorage(responseJSON)
	_, err := execLogged(ctx, exec, fmt.Sprintf("task_source.replace task_id=%s request_bytes=%d response_bytes=%d", compactID(taskID), len(requestJSON), len(responseJSON)), `
INSERT INTO task_sources (task_id, request_headers, request_json, response_headers, response_json, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  request_headers = VALUES(request_headers),
  request_json = VALUES(request_json),
  response_headers = VALUES(response_headers),
  response_json = VALUES(response_json),
  updated_at = VALUES(updated_at)
`, taskID, requestHeaders, requestJSON, responseHeaders, responseJSON, now)
	return err
}

func saveTaskSourceBestEffort(ctx context.Context, exec sqlExecer, taskID, requestHeaders, requestJSON, responseHeaders, responseJSON string, now time.Time) {
	if requestHeaders == "" && requestJSON == "" && responseHeaders == "" && responseJSON == "" {
		return
	}
	if err := replaceTaskSource(ctx, exec, taskID, requestHeaders, requestJSON, responseHeaders, responseJSON, now); err != nil {
		logDB(ctx, "task_source best_effort_failed task_id=%s err=%v", compactID(taskID), err)
	}
}

func updateTaskResponseSource(ctx context.Context, exec sqlExecer, taskID, responseHeaders, responseJSON string, now time.Time) error {
	responseJSON = sourceclean.CompactStringForStorage(responseJSON)
	_, err := execLogged(ctx, exec, fmt.Sprintf("task_source.update_response task_id=%s response_bytes=%d", compactID(taskID), len(responseJSON)), `
INSERT INTO task_sources (task_id, request_headers, request_json, response_headers, response_json, updated_at)
VALUES (?, '', '', ?, ?, ?)
ON DUPLICATE KEY UPDATE
  response_headers = VALUES(response_headers),
  response_json = VALUES(response_json),
  updated_at = VALUES(updated_at)
`, taskID, responseHeaders, responseJSON, now)
	return err
}

func updateTaskResponseSourceBestEffort(ctx context.Context, exec sqlExecer, taskID, responseHeaders, responseJSON string, now time.Time) {
	if responseHeaders == "" && responseJSON == "" {
		return
	}
	if err := updateTaskResponseSource(ctx, exec, taskID, responseHeaders, responseJSON, now); err != nil {
		logDB(ctx, "task_source response_best_effort_failed task_id=%s err=%v", compactID(taskID), err)
	}
}

func replaceTaskImageAssets(ctx context.Context, exec sqlExecer, taskID, role string, items []model.UploadedImage, now time.Time) error {
	if _, err := execLogged(ctx, exec, fmt.Sprintf("task_media.delete task_id=%s role=%s", compactID(taskID), role), `DELETE FROM task_media_assets WHERE task_id = ? AND role = ?`, taskID, role); err != nil {
		return err
	}
	for index, item := range items {
		item.URL = strings.TrimSpace(item.URL)
		if item.URL == "" {
			continue
		}
		if _, err := execLogged(ctx, exec, fmt.Sprintf("task_media.insert_image task_id=%s role=%s index=%d", compactID(taskID), role, index), `
INSERT INTO task_media_assets (
 id, task_id, role, asset_type, url, thumbnail_url, filename, node_id, reference_label,
 video_frame_role, mask_reference_label, mask_url, original_size, compressed_size, compression_ratio,
 sort_order, created_at
) VALUES (?, ?, ?, 'image', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, uuid.NewString(), taskID, role, item.URL, item.ThumbnailURL, item.Filename, item.NodeID, item.ReferenceLabel,
			item.VideoFrameRole, item.MaskReferenceLabel, item.MaskURL, item.OriginalSize, item.CompressedSize,
			item.CompressionRatio, index, now); err != nil {
			return err
		}
	}
	return nil
}

func replaceTaskMediaAssets(ctx context.Context, exec sqlExecer, taskID, role string, items []model.MediaAsset, now time.Time) error {
	if _, err := execLogged(ctx, exec, fmt.Sprintf("task_media.delete task_id=%s role=%s", compactID(taskID), role), `DELETE FROM task_media_assets WHERE task_id = ? AND role = ?`, taskID, role); err != nil {
		return err
	}
	for index, item := range items {
		item.URL = strings.TrimSpace(item.URL)
		if item.URL == "" {
			continue
		}
		assetType := item.Type
		if assetType == "" {
			assetType = mediaTypeForRole(role)
		}
		if _, err := execLogged(ctx, exec, fmt.Sprintf("task_media.insert_media task_id=%s role=%s index=%d type=%s", compactID(taskID), role, index, assetType), `
INSERT INTO task_media_assets (
 id, task_id, role, asset_type, url, thumbnail_url, first_frame_url, last_frame_url, filename, node_id, reference_label,
 duration, clip_start, clip_end, width, height, sort_order, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, uuid.NewString(), taskID, role, assetType, item.URL, item.ThumbnailURL, item.FirstFrameURL, item.LastFrameURL,
			item.Filename, item.NodeID, item.ReferenceLabel, item.Duration, item.ClipStart, item.ClipEnd, item.Width, item.Height, index, now); err != nil {
			return err
		}
	}
	return nil
}

func mediaTypeForRole(role string) string {
	if strings.Contains(role, "audio") {
		return "audio"
	}
	if strings.Contains(role, "video") {
		return "video"
	}
	return "image"
}

func baseURLStorageKey(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	return strings.TrimRight(baseURL, "/")
}

func taskFromClause(includeSource bool) string {
	if includeSource {
		return `tasks t JOIN workspaces w ON w.id = t.workspace_id LEFT JOIN task_sources ts ON ts.task_id = t.id`
	}
	return `tasks t JOIN workspaces w ON w.id = t.workspace_id`
}

func taskColumns(includeSource bool) string {
	sourceColumns := `'' AS request_headers, '' AS request_json, '' AS response_headers, '' AS response_json`
	if includeSource {
		sourceColumns = `COALESCE(ts.request_headers, '') AS request_headers, COALESCE(ts.request_json, '') AS request_json, COALESCE(ts.response_headers, '') AS response_headers, COALESCE(ts.response_json, '') AS response_json`
	}
	return `t.id, t.workspace_id, w.api_key_encrypted, w.base_url, t.task_type, t.status, t.prompt, t.final_prompt, t.model, t.size, t.quality, t.output_format, t.output_compression, t.background, t.moderation, t.input_fidelity, t.n, t.stream, t.style, t.response_format, t.favorite, ` + sourceColumns + `, t.upstream_task_id, t.upstream_status, t.upstream_progress, t.next_poll_at, t.poll_count, t.video_ratio, t.video_width, t.video_height, t.video_duration, t.generate_audio, t.video_draft, t.watermark, t.error_message, t.elapsed_ms, t.created_at, t.updated_at, t.started_at, t.completed_at, CASE WHEN t.status = 'pending' THEN (SELECT COUNT(*) FROM tasks queued WHERE queued.status = 'pending' AND queued.created_at < t.created_at) ELSE 0 END, EXISTS(SELECT 1 FROM plaza_items p WHERE p.task_id = t.id)`
}

func plazaColumns() string {
	return `id, item_type, COALESCE(task_id, ''), COALESCE(canvas_id, ''), canvas_name, canvas_json, task_type, prompt, model, size, quality, output_format, output_compression, background, moderation, input_fidelity, n, stream, style, response_format, reference_images_json, reference_videos_json, reference_audios_json, result_images_json, result_videos_json, video_ratio, video_width, video_height, video_duration, generate_audio, video_draft, watermark, like_count, EXISTS(SELECT 1 FROM plaza_likes WHERE plaza_likes.plaza_id = plaza_items.id AND plaza_likes.client_id = ?), created_at`
}

type scanner interface {
	Scan(dest ...any) error
}

func (s *Store) scanTask(row scanner) (*model.Task, error) {
	var task model.Task
	var encryptedAPIKey string
	var startedAt, completedAt, nextPollAt sql.NullTime
	if err := row.Scan(&task.ID, &task.WorkspaceID, &encryptedAPIKey, &task.BaseURL, &task.TaskType, &task.Status, &task.Prompt, &task.FinalPrompt, &task.Model, &task.Size, &task.Quality, &task.OutputFormat, &task.OutputCompression, &task.Background, &task.Moderation, &task.InputFidelity, &task.N, &task.Stream, &task.Style, &task.ResponseFormat, &task.Favorite, &task.RequestHeaders, &task.RequestJSON, &task.ResponseHeaders, &task.ResponseJSON, &task.UpstreamTaskID, &task.UpstreamStatus, &task.UpstreamProgress, &nextPollAt, &task.PollCount, &task.VideoRatio, &task.VideoWidth, &task.VideoHeight, &task.VideoDuration, &task.GenerateAudio, &task.Draft, &task.Watermark, &task.ErrorMessage, &task.ElapsedMS, &task.CreatedAt, &task.UpdatedAt, &startedAt, &completedAt, &task.QueuePosition, &task.SharedToPlaza); err != nil {
		return nil, err
	}
	apiKey, err := s.decryptAPIKey(encryptedAPIKey)
	if err != nil {
		return nil, err
	}
	task.APIKey = apiKey
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
	return &task, nil
}

func (s *Store) scanTasks(rows *sql.Rows) ([]model.Task, error) {
	tasks := []model.Task{}
	for rows.Next() {
		task, err := s.scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *task)
	}
	return tasks, rows.Err()
}

type taskMediaRow struct {
	TaskID             string
	Role               string
	AssetType          string
	URL                string
	ThumbnailURL       string
	FirstFrameURL      string
	LastFrameURL       string
	Filename           string
	NodeID             string
	ReferenceLabel     string
	VideoFrameRole     string
	MaskReferenceLabel string
	MaskURL            string
	Duration           int
	ClipStart          int
	ClipEnd            int
	Width              int
	Height             int
	OriginalSize       int64
	CompressedSize     int64
	CompressionRatio   float64
}

type taskMediaSet struct {
	ReferenceImages []model.UploadedImage
	ReferenceVideos []model.MediaAsset
	ReferenceAudios []model.MediaAsset
	ResultImages    []model.UploadedImage
	ResultVideos    []model.MediaAsset
}

func (s *Store) attachTaskMediaToTask(ctx context.Context, task *model.Task) error {
	tasks := []model.Task{*task}
	if err := s.attachTaskMedia(ctx, tasks); err != nil {
		return err
	}
	*task = tasks[0]
	return nil
}

func (s *Store) attachTaskMedia(ctx context.Context, tasks []model.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	sets, err := s.loadTaskMedia(ctx, ids, nil)
	if err != nil {
		return err
	}
	for index := range tasks {
		set := sets[tasks[index].ID]
		tasks[index].ReferenceImages = set.ReferenceImages
		tasks[index].ReferenceVideos = set.ReferenceVideos
		tasks[index].ReferenceAudios = set.ReferenceAudios
		tasks[index].ResultImages = set.ResultImages
		tasks[index].ResultVideos = set.ResultVideos
		ensureTaskSlices(&tasks[index])
	}
	return nil
}

func (s *Store) attachTaskUpdateResultMedia(ctx context.Context, updates []model.TaskUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	ids := make([]string, 0, len(updates))
	for _, update := range updates {
		ids = append(ids, update.ID)
	}
	sets, err := s.loadTaskMedia(ctx, ids, []string{"result_image", "result_video"})
	if err != nil {
		return err
	}
	for index := range updates {
		set := sets[updates[index].ID]
		updates[index].ResultImages = set.ResultImages
		updates[index].ResultVideos = set.ResultVideos
		if updates[index].ResultImages == nil {
			updates[index].ResultImages = []model.UploadedImage{}
		}
		if updates[index].ResultVideos == nil {
			updates[index].ResultVideos = []model.MediaAsset{}
		}
	}
	return nil
}

func (s *Store) loadTaskMedia(ctx context.Context, taskIDs []string, roles []string) (map[string]taskMediaSet, error) {
	sets := map[string]taskMediaSet{}
	if len(taskIDs) == 0 {
		return sets, nil
	}
	args := []any{}
	idPlaceholders := make([]string, 0, len(taskIDs))
	for _, id := range taskIDs {
		args = append(args, id)
		idPlaceholders = append(idPlaceholders, placeholder(len(args)))
	}
	where := `task_id IN (` + strings.Join(idPlaceholders, ",") + `)`
	if len(roles) > 0 {
		rolePlaceholders := make([]string, 0, len(roles))
		for _, role := range roles {
			args = append(args, role)
			rolePlaceholders = append(rolePlaceholders, placeholder(len(args)))
		}
		where += ` AND role IN (` + strings.Join(rolePlaceholders, ",") + `)`
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT task_id, role, asset_type, url, thumbnail_url, first_frame_url, last_frame_url, filename, node_id, reference_label, video_frame_role,
       mask_reference_label, mask_url, duration, clip_start, clip_end, width, height, original_size,
       compressed_size, compression_ratio
FROM task_media_assets
WHERE `+where+`
ORDER BY task_id, role, sort_order, created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var row taskMediaRow
		if err := rows.Scan(&row.TaskID, &row.Role, &row.AssetType, &row.URL, &row.ThumbnailURL, &row.FirstFrameURL, &row.LastFrameURL, &row.Filename, &row.NodeID, &row.ReferenceLabel, &row.VideoFrameRole, &row.MaskReferenceLabel, &row.MaskURL, &row.Duration, &row.ClipStart, &row.ClipEnd, &row.Width, &row.Height, &row.OriginalSize, &row.CompressedSize, &row.CompressionRatio); err != nil {
			return nil, err
		}
		set := sets[row.TaskID]
		switch row.Role {
		case "reference_image":
			set.ReferenceImages = append(set.ReferenceImages, row.uploadedImage())
		case "reference_video":
			set.ReferenceVideos = append(set.ReferenceVideos, row.mediaAsset())
		case "reference_audio":
			set.ReferenceAudios = append(set.ReferenceAudios, row.mediaAsset())
		case "result_image":
			set.ResultImages = append(set.ResultImages, row.uploadedImage())
		case "result_video":
			set.ResultVideos = append(set.ResultVideos, row.mediaAsset())
		}
		sets[row.TaskID] = set
	}
	return sets, rows.Err()
}

func (row taskMediaRow) uploadedImage() model.UploadedImage {
	return model.UploadedImage{
		URL:                row.URL,
		ThumbnailURL:       row.ThumbnailURL,
		FirstFrameURL:      row.FirstFrameURL,
		LastFrameURL:       row.LastFrameURL,
		Filename:           row.Filename,
		NodeID:             row.NodeID,
		ReferenceLabel:     row.ReferenceLabel,
		VideoFrameRole:     row.VideoFrameRole,
		MaskReferenceLabel: row.MaskReferenceLabel,
		OriginalSize:       row.OriginalSize,
		CompressedSize:     row.CompressedSize,
		CompressionRatio:   row.CompressionRatio,
		MaskURL:            row.MaskURL,
	}
}

func (row taskMediaRow) mediaAsset() model.MediaAsset {
	return model.MediaAsset{
		Type:           row.AssetType,
		URL:            row.URL,
		ThumbnailURL:   row.ThumbnailURL,
		FirstFrameURL:  row.FirstFrameURL,
		LastFrameURL:   row.LastFrameURL,
		Filename:       row.Filename,
		NodeID:         row.NodeID,
		ReferenceLabel: row.ReferenceLabel,
		Duration:       row.Duration,
		ClipStart:      row.ClipStart,
		ClipEnd:        row.ClipEnd,
		Width:          row.Width,
		Height:         row.Height,
	}
}

func scanPlazaItem(row scanner) (*model.PlazaItem, error) {
	var item model.PlazaItem
	if err := row.Scan(&item.ID, &item.ItemType, &item.TaskID, &item.CanvasID, &item.CanvasName, &item.CanvasJSONText, &item.TaskType, &item.Prompt, &item.Model, &item.Size, &item.Quality, &item.OutputFormat, &item.OutputCompression, &item.Background, &item.Moderation, &item.InputFidelity, &item.N, &item.Stream, &item.Style, &item.ResponseFormat, &item.ReferenceImagesJSON, &item.ReferenceVideosJSON, &item.ReferenceAudiosJSON, &item.ResultImagesJSON, &item.ResultVideosJSON, &item.VideoRatio, &item.VideoWidth, &item.VideoHeight, &item.VideoDuration, &item.GenerateAudio, &item.Draft, &item.Watermark, &item.LikeCount, &item.Liked, &item.CreatedAt); err != nil {
		return nil, err
	}
	if item.ItemType == "" {
		item.ItemType = "task"
	}
	if item.TaskType == "" {
		item.TaskType = model.TaskTypeImageGeneration
	}
	if item.InputFidelity == "" {
		item.InputFidelity = "high"
	}
	decodePlazaJSON(&item)
	ensurePlazaSlices(&item)
	return &item, nil
}

func ensureTaskSlices(task *model.Task) {
	if task.ReferenceImages == nil {
		task.ReferenceImages = []model.UploadedImage{}
	}
	if task.ReferenceVideos == nil {
		task.ReferenceVideos = []model.MediaAsset{}
	}
	if task.ReferenceAudios == nil {
		task.ReferenceAudios = []model.MediaAsset{}
	}
	if task.ResultImages == nil {
		task.ResultImages = []model.UploadedImage{}
	}
	if task.ResultVideos == nil {
		task.ResultVideos = []model.MediaAsset{}
	}
}

func ensurePlazaSlices(item *model.PlazaItem) {
	if item.ReferenceImages == nil {
		item.ReferenceImages = []model.UploadedImage{}
	}
	if item.ReferenceVideos == nil {
		item.ReferenceVideos = []model.MediaAsset{}
	}
	if item.ReferenceAudios == nil {
		item.ReferenceAudios = []model.MediaAsset{}
	}
	if item.ResultImages == nil {
		item.ResultImages = []model.UploadedImage{}
	}
	if item.ResultVideos == nil {
		item.ResultVideos = []model.MediaAsset{}
	}
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

func decodePlazaJSON(item *model.PlazaItem) {
	if item.ItemType == "canvas" && item.CanvasJSONText != "" {
		item.CanvasJSON = json.RawMessage(item.CanvasJSONText)
	}
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
	return "?"
}
