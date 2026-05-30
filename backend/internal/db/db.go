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
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"image-web/backend/internal/model"
	"image-web/backend/internal/sourceclean"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
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
const dbIdleTransactionTimeout = 15 * time.Second
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
	_, err := tx.ExecContext(ctx, fmt.Sprintf(
		`SET LOCAL lock_timeout = %s; SET LOCAL statement_timeout = %s; SET LOCAL idle_in_transaction_session_timeout = %s`,
		sqlTimeoutLiteral(dbLockTimeout),
		sqlTimeoutLiteral(dbStatementTimeout),
		sqlTimeoutLiteral(dbIdleTransactionTimeout),
	))
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
	dsn = withRuntimeTimeoutParams(dsn)
	key, err := deriveCredentialKey(credentialSecret)
	if err != nil {
		return nil, err
	}
	database, err := sql.Open("pgx", dsn)
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

func withRuntimeTimeoutParams(dsn string) string {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return dsn
	}
	parsed, err := url.Parse(trimmed)
	if err == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
		values := parsed.Query()
		setDefaultRuntimeParam(values, "lock_timeout", dbLockTimeout)
		setDefaultRuntimeParam(values, "statement_timeout", dbStatementTimeout)
		setDefaultRuntimeParam(values, "idle_in_transaction_session_timeout", dbIdleTransactionTimeout)
		parsed.RawQuery = values.Encode()
		return parsed.String()
	}
	if strings.Contains(trimmed, "=") && !strings.ContainsAny(trimmed, "\r\n") {
		params := []struct {
			key     string
			timeout time.Duration
		}{
			{"lock_timeout", dbLockTimeout},
			{"statement_timeout", dbStatementTimeout},
			{"idle_in_transaction_session_timeout", dbIdleTransactionTimeout},
		}
		for _, param := range params {
			if !keywordDSNHasKey(trimmed, param.key) {
				trimmed += fmt.Sprintf(" %s=%d", param.key, param.timeout.Milliseconds())
			}
		}
	}
	return trimmed
}

func setDefaultRuntimeParam(values url.Values, key string, timeout time.Duration) {
	if values.Get(key) == "" {
		values.Set(key, strconv.FormatInt(timeout.Milliseconds(), 10))
	}
}

func keywordDSNHasKey(dsn, key string) bool {
	for _, field := range strings.Fields(dsn) {
		name, _, ok := strings.Cut(field, "=")
		if ok && name == key {
			return true
		}
	}
	return false
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
  id TEXT PRIMARY KEY,
  base_url TEXT NOT NULL,
  api_key_hash TEXT NOT NULL,
  api_key_encrypted TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (base_url, api_key_hash)
);
CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  task_type TEXT NOT NULL DEFAULT 'image_generation' CHECK (task_type IN ('image_generation', 'video_generation')),
  status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed')),
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
  favorite BOOLEAN NOT NULL DEFAULT FALSE,
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
  video_draft BOOLEAN NOT NULL DEFAULT FALSE,
  watermark BOOLEAN NOT NULL DEFAULT FALSE,
  error_message TEXT NOT NULL DEFAULT '',
  elapsed_ms BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ
);
CREATE TABLE IF NOT EXISTS task_sources (
  task_id TEXT PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
  request_headers TEXT NOT NULL DEFAULT '',
  request_json TEXT NOT NULL DEFAULT '',
  response_headers TEXT NOT NULL DEFAULT '',
  response_json TEXT NOT NULL DEFAULT '',
  updated_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS task_media_assets (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  role TEXT NOT NULL CHECK (role IN ('reference_image', 'reference_video', 'reference_audio', 'result_image', 'result_video', 'result_audio')),
  asset_type TEXT NOT NULL DEFAULT '',
  url TEXT NOT NULL,
  thumbnail_url TEXT NOT NULL DEFAULT '',
  first_frame_url TEXT NOT NULL DEFAULT '',
  last_frame_url TEXT NOT NULL DEFAULT '',
  filename TEXT NOT NULL DEFAULT '',
  node_id TEXT NOT NULL DEFAULT '',
  reference_label TEXT NOT NULL DEFAULT '',
  video_frame_role TEXT NOT NULL DEFAULT '',
  mask_reference_label TEXT NOT NULL DEFAULT '',
  mask_url TEXT NOT NULL DEFAULT '',
  duration INT NOT NULL DEFAULT 0,
  clip_start INT NOT NULL DEFAULT 0,
  clip_end INT NOT NULL DEFAULT 0,
  width INT NOT NULL DEFAULT 0,
  height INT NOT NULL DEFAULT 0,
  original_size BIGINT NOT NULL DEFAULT 0,
  compressed_size BIGINT NOT NULL DEFAULT 0,
  compression_ratio DOUBLE PRECISION NOT NULL DEFAULT 0,
  sort_order INT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS canvases (
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  canvas_id TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  canvas_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  sort_order INT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (workspace_id, canvas_id)
);
CREATE TABLE IF NOT EXISTS plaza_items (
  id TEXT PRIMARY KEY,
  item_type TEXT NOT NULL DEFAULT 'task' CHECK (item_type IN ('task', 'canvas')),
  task_id TEXT REFERENCES tasks(id) ON DELETE CASCADE,
  workspace_id TEXT REFERENCES workspaces(id) ON DELETE CASCADE,
  canvas_id TEXT,
  canvas_name TEXT NOT NULL DEFAULT '',
  canvas_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  task_type TEXT NOT NULL DEFAULT 'image_generation' CHECK (task_type IN ('image_generation', 'video_generation')),
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
  video_draft BOOLEAN NOT NULL DEFAULT FALSE,
  watermark BOOLEAN NOT NULL DEFAULT FALSE,
  like_count INT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  CHECK (
    (item_type = 'task' AND task_id IS NOT NULL AND canvas_id IS NULL)
    OR
    (item_type = 'canvas' AND task_id IS NULL AND workspace_id IS NOT NULL AND canvas_id IS NOT NULL)
  ),
  FOREIGN KEY (workspace_id, canvas_id) REFERENCES canvases(workspace_id, canvas_id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS plaza_likes (
  plaza_id TEXT NOT NULL REFERENCES plaza_items(id) ON DELETE CASCADE,
  client_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (plaza_id, client_id)
);
CREATE TABLE IF NOT EXISTS site_config (
  config_key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tasks_workspace_created ON tasks(workspace_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_workspace_status_created ON tasks(workspace_id, status, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_pending_queue ON tasks(created_at ASC, id ASC) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_tasks_video_poll ON tasks ((COALESCE(next_poll_at, updated_at)), id) WHERE task_type = 'video_generation' AND status = 'running' AND upstream_task_id <> '';
CREATE INDEX IF NOT EXISTS idx_task_media_task_role_order ON task_media_assets(task_id, role, sort_order, created_at);
CREATE INDEX IF NOT EXISTS idx_canvases_workspace_order ON canvases(workspace_id, sort_order, canvas_id);
CREATE INDEX IF NOT EXISTS idx_plaza_created ON plaza_items(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_plaza_likes ON plaza_items(like_count DESC, created_at DESC, id DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_plaza_task_unique ON plaza_items(task_id) WHERE item_type = 'task';
CREATE UNIQUE INDEX IF NOT EXISTS idx_plaza_canvas_unique ON plaza_items(workspace_id, canvas_id) WHERE item_type = 'canvas';
`)
	if err != nil {
		return err
	}
	s.ensureTrigramIndexes(ctx)
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
SELECT c.relname, a.attname
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_attribute a ON a.attrelid = c.oid
WHERE n.nspname = current_schema()
  AND c.relkind IN ('r', 'p')
  AND c.relname IN (`+strings.Join(placeholders, ",")+`)
  AND a.attnum > 0
  AND NOT a.attisdropped
ORDER BY c.relname, a.attnum`, args...)
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
  WHERE table_schema = current_schema() AND table_name = $1
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
  WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2
)`, table, column).Scan(&exists)
	})
	return exists, err
}

func (s *Store) ensureTrigramIndexes(ctx context.Context) {
	if _, err := execLogged(ctx, s.db, "migration pg_trgm extension", `CREATE EXTENSION IF NOT EXISTS pg_trgm`); err != nil {
		fmt.Println("db pg_trgm unavailable, fallback to LIKE search:", err)
		return
	}
	if _, err := execLogged(ctx, s.db, "migration pg_trgm indexes", `
CREATE INDEX IF NOT EXISTS idx_tasks_prompt_trgm ON tasks USING GIN (prompt gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_tasks_final_prompt_trgm ON tasks USING GIN (final_prompt gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_plaza_prompt_trgm ON plaza_items USING GIN (prompt gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_plaza_canvas_name_trgm ON plaza_items USING GIN (canvas_name gin_trgm_ops);
`); err != nil {
		fmt.Println("db pg_trgm indexes unavailable, fallback to LIKE search:", err)
	}
}

func (s *Store) ensureSiteConfig(ctx context.Context) error {
	_, err := execLogged(ctx, s.db, "migration site_config defaults", `
INSERT INTO site_config (config_key, value)
VALUES
  ('baseurl_whitelist_enabled', 'false'),
  ('baseurl_whitelist', '[]'),
  ('admin_contact_image', ''),
  ('site_title', '图片生成工作台'),
  ('site_icon', 'AI'),
  ('worker_concurrency', '1')
ON CONFLICT (config_key) DO NOTHING`)
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
		return s.db.QueryRowContext(ctx, `SELECT id, base_url, api_key_encrypted FROM workspaces WHERE base_url = $1 AND api_key_hash = $2`, baseURLKey, hash).Scan(&ws.ID, &ws.BaseURL, &ws.EncryptedAPIKey)
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
INSERT INTO workspaces (id, base_url, api_key_hash, api_key_encrypted, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $5)
ON CONFLICT (base_url, api_key_hash) DO NOTHING
`, ws.ID, ws.BaseURL, hash, encrypted, now)
	if err != nil {
		return workspace{}, err
	}
	if count, _ := result.RowsAffected(); count > 0 {
		s.cacheWorkspace(cacheKey, ws)
		return ws, nil
	}
	err = scanLogged(ctx, "workspace.resolve_after_conflict", func() error {
		return s.db.QueryRowContext(ctx, `SELECT id, base_url, api_key_encrypted FROM workspaces WHERE base_url = $1 AND api_key_hash = $2`, baseURLKey, hash).Scan(&ws.ID, &ws.BaseURL, &ws.EncryptedAPIKey)
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
	rows, err := s.db.QueryContext(ctx, `SELECT canvas_json::text, updated_at FROM canvases WHERE workspace_id = $1 ORDER BY sort_order ASC, updated_at ASC, canvas_id ASC`, ws.ID)
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
		if _, err := execLogged(ctx, s.db, "canvas.delete_row canvas_id="+compactID(id), `DELETE FROM canvases WHERE workspace_id = $1 AND canvas_id = $2`, ws.ID, id); err != nil {
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
			return s.db.QueryRowContext(ctx, `SELECT canvas_json::text, sort_order FROM canvases WHERE workspace_id = $1 AND canvas_id = $2`, ws.ID, id).Scan(&currentRaw, &sortOrder)
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
	_, err := execLogged(ctx, tx, "canvas.advisory_lock workspace_id="+compactID(workspaceID), `SELECT pg_advisory_xact_lock(hashtext($1), hashtext('canvases'))`, workspaceID)
	return err
}

func upsertCanvasRow(ctx context.Context, exec sqlExecer, workspaceID, canvasID, name string, canvas json.RawMessage, sortOrder int, now time.Time) error {
	storedCanvas, err := packCanvasForStorage(canvas)
	if err != nil {
		return err
	}
	_, err = execLogged(ctx, exec, fmt.Sprintf("canvas.upsert_row canvas_id=%s bytes=%d stored_bytes=%d", compactID(canvasID), len(canvas), len(storedCanvas)), `
INSERT INTO canvases (workspace_id, canvas_id, name, canvas_json, sort_order, created_at, updated_at)
VALUES ($1, $2, $3, $4::jsonb, $5, $6, $6)
ON CONFLICT (workspace_id, canvas_id)
DO UPDATE SET name = EXCLUDED.name, canvas_json = EXCLUDED.canvas_json, sort_order = EXCLUDED.sort_order, updated_at = EXCLUDED.updated_at
`, workspaceID, canvasID, name, string(storedCanvas), sortOrder, now)
	return err
}

func upsertCanvasRowPreserveSort(ctx context.Context, exec sqlExecQueryer, workspaceID, canvasID, name string, canvas json.RawMessage, now time.Time) error {
	storedCanvas, err := packCanvasForStorage(canvas)
	if err != nil {
		return err
	}
	result, err := execLogged(ctx, exec, fmt.Sprintf("canvas.update_row canvas_id=%s bytes=%d stored_bytes=%d", compactID(canvasID), len(canvas), len(storedCanvas)), `
UPDATE canvases
SET name = $3, canvas_json = $4::jsonb, updated_at = $5
WHERE workspace_id = $1 AND canvas_id = $2
`, workspaceID, canvasID, name, string(storedCanvas), now)
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
VALUES ($1, $2, $3, $4::jsonb, $5, $6, $6)
ON CONFLICT (workspace_id, canvas_id)
DO UPDATE SET name = EXCLUDED.name, canvas_json = EXCLUDED.canvas_json, sort_order = EXCLUDED.sort_order, updated_at = EXCLUDED.updated_at
`, workspaceID, canvasID, name, string(storedCanvas), sortOrder, now)
	return err
}

func deleteMissingCanvases(ctx context.Context, exec sqlExecer, workspaceID string, keepIDs []string) error {
	if len(keepIDs) == 0 {
		_, err := execLogged(ctx, exec, "canvas.delete_missing all", `DELETE FROM canvases WHERE workspace_id = $1`, workspaceID)
		return err
	}
	args := []any{workspaceID}
	placeholders := make([]string, 0, len(keepIDs))
	for _, id := range keepIDs {
		args = append(args, id)
		placeholders = append(placeholders, placeholder(len(args)))
	}
	_, err := execLogged(ctx, exec, fmt.Sprintf("canvas.delete_missing keep=%d", len(keepIDs)), `DELETE FROM canvases WHERE workspace_id = $1 AND canvas_id NOT IN (`+strings.Join(placeholders, ",")+`)`, args...)
	return err
}

func nextCanvasSortOrder(ctx context.Context, query sqlQueryer, workspaceID string) (int, error) {
	var next int
	if err := scanLogged(ctx, "canvas.next_sort_order", func() error {
		return query.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order) + 1, 0) FROM canvases WHERE workspace_id = $1`, workspaceID).Scan(&next)
	}); err != nil {
		return 0, err
	}
	return next, nil
}

func currentCanvasSortOrder(ctx context.Context, query sqlQueryer, workspaceID, canvasID string) (int, bool, error) {
	var sortOrder int
	err := scanLogged(ctx, "canvas.current_sort_order canvas_id="+compactID(canvasID), func() error {
		return query.QueryRowContext(ctx, `SELECT sort_order FROM canvases WHERE workspace_id = $1 AND canvas_id = $2`, workspaceID, canvasID).Scan(&sortOrder)
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
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35)`,
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
	args := []any{ws.ID}
	where := []string{"t.workspace_id = $1"}
	if status != "" && status != "all" {
		args = append(args, status)
		where = append(where, "t.status = "+placeholder(len(args)))
	}
	if query != "" {
		args = append(args, "%"+query+"%")
		queryPlaceholder := placeholder(len(args))
		where = append(where, "(t.prompt ILIKE "+queryPlaceholder+" OR t.final_prompt ILIKE "+queryPlaceholder+" OR t.model ILIKE "+queryPlaceholder+" OR t.task_type ILIKE "+queryPlaceholder+")")
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
	row := s.db.QueryRowContext(ctx, `SELECT `+taskColumns(true)+` FROM `+taskFromClause(true)+` WHERE t.id = $1 AND t.workspace_id = $2`, id, ws.ID)
	task, err := s.scanTask(row)
	if err != nil {
		return nil, err
	}
	return task, s.attachTaskMediaToTask(ctx, task)
}

func (s *Store) GetAnyTask(ctx context.Context, id string) (*model.Task, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+taskColumns(true)+` FROM `+taskFromClause(true)+` WHERE t.id = $1`, id)
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
	_, err = execLogged(ctx, s.db, "plaza.task.insert task_id="+compactID(task.ID), `INSERT INTO plaza_items (
	 id, item_type, task_id, workspace_id, task_type, prompt, model, size, quality, output_format, output_compression,
	 background, moderation, input_fidelity, n, stream, style, response_format, reference_images_json,
	 reference_videos_json, reference_audios_json, result_images_json, result_videos_json,
	 video_ratio, video_width, video_height, video_duration, generate_audio, video_draft, watermark,
	 like_count, created_at, updated_at
	) VALUES ($1, 'task', $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18::jsonb, $19::jsonb, $20::jsonb, $21::jsonb, $22::jsonb, $23, $24, $25, $26, $27, $28, $29, 0, $30, $31)`,
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
		return tx.QueryRowContext(ctx, `SELECT id FROM plaza_items WHERE item_type = 'canvas' AND workspace_id = $1 AND canvas_id = $2 FOR UPDATE`, ws.ID, canvasID).Scan(&plazaID)
	})
	if err == nil {
		if _, err := execLogged(ctx, tx, "plaza.canvas.update plaza_id="+compactID(plazaID), `UPDATE plaza_items SET canvas_name = $1, canvas_json = $2::jsonb, prompt = $3, updated_at = $4 WHERE id = $5`, name, string(canvas), prompt, now, plazaID); err != nil {
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
		) VALUES ($1, 'canvas', NULL, $2, $3, $4, $5::jsonb, 'image_generation', $6, 'canvas', '', '', '', 0, '', '', 'high', 1, false, '', '', '[]'::jsonb, '[]'::jsonb, '[]'::jsonb, '[]'::jsonb, '[]'::jsonb, '', 0, 0, 0, false, false, false, 0, $7, $8)`,
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
	_, err = execLogged(ctx, s.db, "plaza.canvas.delete canvas_id="+compactID(canvasID), `DELETE FROM plaza_items WHERE item_type = 'canvas' AND workspace_id = $1 AND canvas_id = $2`, ws.ID, canvasID)
	return err
}

func (s *Store) UnshareTaskFromPlaza(ctx context.Context, id, apiKey, baseURL string) error {
	task, err := s.GetTask(ctx, id, apiKey, baseURL)
	if err != nil {
		return err
	}
	result, err := execLogged(ctx, s.db, "plaza.task.delete task_id="+compactID(task.ID), `DELETE FROM plaza_items WHERE item_type = 'task' AND task_id = $1`, task.ID)
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
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.status, t.error_message, t.elapsed_ms, t.updated_at, t.started_at, t.completed_at, CASE WHEN t.status = 'pending' THEN (SELECT COUNT(*) FROM tasks queued WHERE queued.status = 'pending' AND queued.created_at < t.created_at) ELSE 0 END, t.upstream_status, t.upstream_progress FROM tasks t WHERE t.workspace_id = $1 AND t.id IN (`+strings.Join(placeholders, ",")+`)`, args...)
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
		args = append(args, pattern)
		wherePlaceholder := placeholder(len(args))
		where = append(where, fmt.Sprintf("(LOWER(prompt) LIKE %s OR LOWER(model) LIKE %s OR LOWER(size) LIKE %s OR LOWER(quality) LIKE %s OR LOWER(output_format) LIKE %s OR LOWER(background) LIKE %s OR LOWER(canvas_name) LIKE %s)", wherePlaceholder, wherePlaceholder, wherePlaceholder, wherePlaceholder, wherePlaceholder, wherePlaceholder, wherePlaceholder))
		countArgs = append(countArgs, pattern)
		countPlaceholder := placeholder(len(countArgs))
		countWhere = append(countWhere, fmt.Sprintf("(LOWER(prompt) LIKE %s OR LOWER(model) LIKE %s OR LOWER(size) LIKE %s OR LOWER(quality) LIKE %s OR LOWER(output_format) LIKE %s OR LOWER(background) LIKE %s OR LOWER(canvas_name) LIKE %s)", countPlaceholder, countPlaceholder, countPlaceholder, countPlaceholder, countPlaceholder, countPlaceholder, countPlaceholder))
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
	tx, txLog, err := s.beginTx(ctx, "plaza.like id="+compactID(id))
	if err != nil {
		return nil, err
	}
	defer txLog.Rollback(tx)
	var exists bool
	if err := scanLogged(ctx, "plaza.like.exists id="+compactID(id), func() error {
		return tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM plaza_items WHERE id = $1)`, id).Scan(&exists)
	}); err != nil {
		return nil, err
	}
	if !exists {
		return nil, sql.ErrNoRows
	}
	if liked {
		result, err := execLogged(ctx, tx, "plaza.like.insert id="+compactID(id), `INSERT INTO plaza_likes (plaza_id, client_id, created_at) VALUES ($1, $2, $3) ON CONFLICT (plaza_id, client_id) DO NOTHING`, id, clientID, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		if count, _ := result.RowsAffected(); count > 0 {
			if _, err := execLogged(ctx, tx, "plaza.like_count.increment id="+compactID(id), `UPDATE plaza_items SET like_count = like_count + 1, updated_at = $1 WHERE id = $2`, time.Now().UTC(), id); err != nil {
				return nil, err
			}
		}
	} else {
		result, err := execLogged(ctx, tx, "plaza.like.delete id="+compactID(id), `DELETE FROM plaza_likes WHERE plaza_id = $1 AND client_id = $2`, id, clientID)
		if err != nil {
			return nil, err
		}
		if count, _ := result.RowsAffected(); count > 0 {
			if _, err := execLogged(ctx, tx, "plaza.like_count.decrement id="+compactID(id), `UPDATE plaza_items SET like_count = GREATEST(like_count - 1, 0), updated_at = $1 WHERE id = $2`, time.Now().UTC(), id); err != nil {
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
	result, err := execLogged(ctx, s.db, "task.delete id="+compactID(id), `DELETE FROM tasks WHERE id = $1 AND workspace_id = $2`, id, ws.ID)
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
	result, err := execLogged(ctx, s.db, "task.favorite id="+compactID(id), `UPDATE tasks SET favorite = $1, updated_at = $2 WHERE id = $3 AND workspace_id = $4`, favorite, time.Now().UTC(), id, ws.ID)
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
	_, err := execLogged(ctx, s.db, "task.fail_stale_image", `UPDATE tasks SET status = $1, error_message = $2, elapsed_ms = GREATEST(elapsed_ms, EXTRACT(EPOCH FROM ($3 - started_at))::BIGINT * 1000), completed_at = $3, updated_at = $3 WHERE status = $4 AND task_type = $5 AND started_at < $6`, model.TaskFailed, message, now, model.TaskRunning, model.TaskTypeImageGeneration, cutoff)
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
		task, scanErr = s.scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns(false)+` FROM `+taskFromClause(false)+` WHERE t.status = $1 ORDER BY t.created_at ASC LIMIT 1 FOR UPDATE OF t SKIP LOCKED`, model.TaskPending))
		return scanErr
	})
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	result, err := execLogged(ctx, tx, "task.next_pending.mark_running id="+compactID(task.ID), `UPDATE tasks SET status = $1, started_at = $2, updated_at = $3 WHERE id = $4 AND status = $5`, model.TaskRunning, now, now, task.ID, model.TaskPending)
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
	result, err := execLogged(ctx, tx, "task.complete_image.update id="+compactID(id), `UPDATE tasks SET status = $1, final_prompt = $2, elapsed_ms = $3, completed_at = $4, updated_at = $5, error_message = '' WHERE id = $6`, model.TaskSucceeded, finalPrompt, elapsedMS, now, now, id)
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
	result, err := execLogged(ctx, tx, "task.video_submitted.update id="+compactID(id), `UPDATE tasks SET status = $1, upstream_task_id = $2, upstream_status = $3, upstream_progress = $4, next_poll_at = $5, poll_count = 0, updated_at = $6 WHERE id = $7`, model.TaskRunning, upstreamTaskID, upstreamStatus, progress, nextPollAt, now, id)
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
	rows, err := s.db.QueryContext(ctx, `SELECT `+taskColumns(true)+` FROM `+taskFromClause(true)+` WHERE t.task_type = $1 AND t.status = $2 AND t.upstream_task_id <> '' AND (t.next_poll_at IS NULL OR t.next_poll_at <= $3) ORDER BY COALESCE(t.next_poll_at, t.updated_at) ASC LIMIT $4`, model.TaskTypeVideoGeneration, model.TaskRunning, time.Now().UTC(), limit)
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
	result, err := execLogged(ctx, tx, "task.video_poll_update.update id="+compactID(id), `UPDATE tasks SET upstream_status = $1, upstream_progress = $2, next_poll_at = $3, poll_count = poll_count + 1, updated_at = $4 WHERE id = $5`, upstreamStatus, progress, nextPollAt, now, id)
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
	result, err := execLogged(ctx, tx, "task.complete_video.update id="+compactID(id), `UPDATE tasks SET status = $1, final_prompt = $2, upstream_status = $3, upstream_progress = 100, elapsed_ms = $4, completed_at = $5, updated_at = $6, next_poll_at = NULL, error_message = '' WHERE id = $7`, model.TaskSucceeded, finalPrompt, "SUCCESS", elapsedMS, now, now, id)
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

func (s *Store) FailTask(ctx context.Context, id string, finalPrompt, requestHeaders, requestJSON, responseHeaders, responseJSON, message string, elapsedMS int64) error {
	now := time.Now().UTC()
	tx, txLog, err := s.beginTx(ctx, "task.fail id="+compactID(id))
	if err != nil {
		return err
	}
	defer txLog.Rollback(tx)
	result, err := execLogged(ctx, tx, "task.fail.update id="+compactID(id), `UPDATE tasks SET status = $1, final_prompt = $2, error_message = $3, elapsed_ms = $4, completed_at = $5, updated_at = $6 WHERE id = $7`, model.TaskFailed, finalPrompt, message, elapsedMS, now, now, id)
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
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (task_id) DO UPDATE SET
  request_headers = EXCLUDED.request_headers,
  request_json = EXCLUDED.request_json,
  response_headers = EXCLUDED.response_headers,
  response_json = EXCLUDED.response_json,
  updated_at = EXCLUDED.updated_at
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
INSERT INTO task_sources (task_id, response_headers, response_json, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (task_id) DO UPDATE SET
  response_headers = EXCLUDED.response_headers,
  response_json = EXCLUDED.response_json,
  updated_at = EXCLUDED.updated_at
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
	if _, err := execLogged(ctx, exec, fmt.Sprintf("task_media.delete task_id=%s role=%s", compactID(taskID), role), `DELETE FROM task_media_assets WHERE task_id = $1 AND role = $2`, taskID, role); err != nil {
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
) VALUES ($1, $2, $3, 'image', $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
`, uuid.NewString(), taskID, role, item.URL, item.ThumbnailURL, item.Filename, item.NodeID, item.ReferenceLabel,
			item.VideoFrameRole, item.MaskReferenceLabel, item.MaskURL, item.OriginalSize, item.CompressedSize,
			item.CompressionRatio, index, now); err != nil {
			return err
		}
	}
	return nil
}

func replaceTaskMediaAssets(ctx context.Context, exec sqlExecer, taskID, role string, items []model.MediaAsset, now time.Time) error {
	if _, err := execLogged(ctx, exec, fmt.Sprintf("task_media.delete task_id=%s role=%s", compactID(taskID), role), `DELETE FROM task_media_assets WHERE task_id = $1 AND role = $2`, taskID, role); err != nil {
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
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
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
	return `id, item_type, COALESCE(task_id, ''), COALESCE(canvas_id, ''), canvas_name, canvas_json::text, task_type, prompt, model, size, quality, output_format, output_compression, background, moderation, input_fidelity, n, stream, style, response_format, reference_images_json::text, reference_videos_json::text, reference_audios_json::text, result_images_json::text, result_videos_json::text, video_ratio, video_width, video_height, video_duration, generate_audio, video_draft, watermark, like_count, EXISTS(SELECT 1 FROM plaza_likes WHERE plaza_likes.plaza_id = plaza_items.id AND plaza_likes.client_id = $1), created_at`
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
	return fmt.Sprintf("$%d", index)
}
