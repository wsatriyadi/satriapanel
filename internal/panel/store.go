package panel

import (
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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS admin_users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	username TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
	token_hash TEXT NOT NULL UNIQUE,
	created_at TEXT NOT NULL,
	last_seen_at TEXT NOT NULL,
	expires_at TEXT NOT NULL,
	ip TEXT NOT NULL DEFAULT '',
	user_agent TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS apps (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	slug TEXT NOT NULL UNIQUE,
	source_type TEXT NOT NULL DEFAULT 'github',
	repo_url TEXT NOT NULL DEFAULT '',
	branch TEXT NOT NULL DEFAULT 'main',
	runtime TEXT NOT NULL DEFAULT 'node',
	runtime_version TEXT NOT NULL DEFAULT '',
	mode TEXT NOT NULL DEFAULT 'native',
	driver TEXT NOT NULL DEFAULT 'auto',
	install_cmd TEXT NOT NULL DEFAULT '',
	build_cmd TEXT NOT NULL DEFAULT '',
	start_cmd TEXT NOT NULL DEFAULT '',
	port INTEGER NOT NULL UNIQUE,
	hostname TEXT NOT NULL DEFAULT '',
	auto_deploy INTEGER NOT NULL DEFAULT 0,
	webhook_secret TEXT NOT NULL DEFAULT '',
	webhook_id TEXT NOT NULL DEFAULT '',
	workdir TEXT NOT NULL DEFAULT '',
	persistent_dirs TEXT NOT NULL DEFAULT '[]',
	status TEXT NOT NULL DEFAULT 'berhenti',
	active_release_id INTEGER,
	health_status TEXT NOT NULL DEFAULT 'unknown',
	health_path TEXT NOT NULL DEFAULT '/',
	health_checked_at TEXT,
	last_deploy_at TEXT,
	needs_restart INTEGER NOT NULL DEFAULT 0,
	image TEXT NOT NULL DEFAULT '',
	internal_port INTEGER NOT NULL DEFAULT 0,
	compose_service TEXT NOT NULL DEFAULT '',
	dns_record_id TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS app_env (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	app_id INTEGER NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
	key TEXT NOT NULL,
	value TEXT NOT NULL DEFAULT '',
	is_secret INTEGER NOT NULL DEFAULT 0,
	UNIQUE(app_id, key)
);
CREATE TABLE IF NOT EXISTS releases (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	app_id INTEGER NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
	commit_hash TEXT NOT NULL DEFAULT '',
	dir_path TEXT NOT NULL DEFAULT '',
	image_tag TEXT NOT NULL DEFAULT '',
	built_at TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'arsip'
);
CREATE TABLE IF NOT EXISTS deployments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	app_id INTEGER NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
	release_id INTEGER,
	trigger TEXT NOT NULL DEFAULT 'manual',
	status TEXT NOT NULL DEFAULT 'antre',
	commit_hash TEXT NOT NULL DEFAULT '',
	log_path TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	started_at TEXT,
	finished_at TEXT,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS databases (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	engine TEXT NOT NULL,
	name TEXT NOT NULL,
	host TEXT NOT NULL DEFAULT '',
	port INTEGER NOT NULL DEFAULT 0,
	username TEXT NOT NULL DEFAULT '',
	password_enc TEXT NOT NULL DEFAULT '',
	file_path TEXT NOT NULL DEFAULT '',
	managed_by_panel INTEGER NOT NULL DEFAULT 0,
	app_id INTEGER,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS redis_allocations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	app_id INTEGER NOT NULL UNIQUE REFERENCES apps(id) ON DELETE CASCADE,
	acl_user TEXT NOT NULL,
	acl_password_enc TEXT NOT NULL,
	prefix TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS cron_jobs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	kind TEXT NOT NULL DEFAULT 'system',
	app_id INTEGER,
	command TEXT NOT NULL DEFAULT '',
	workdir TEXT NOT NULL DEFAULT '',
	schedule_expr TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 1,
	timeout_seconds INTEGER NOT NULL DEFAULT 300,
	builtin_key TEXT,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_cron_builtin ON cron_jobs(builtin_key) WHERE builtin_key IS NOT NULL;
CREATE TABLE IF NOT EXISTS cron_runs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	job_id INTEGER NOT NULL REFERENCES cron_jobs(id) ON DELETE CASCADE,
	started_at TEXT NOT NULL,
	finished_at TEXT,
	status TEXT NOT NULL,
	exit_code INTEGER,
	output TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS backups (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	trigger TEXT NOT NULL,
	status TEXT NOT NULL,
	drive_folder TEXT NOT NULL DEFAULT '',
	total_size INTEGER NOT NULL DEFAULT 0,
	manifest_summary TEXT NOT NULL DEFAULT '',
	local_path TEXT NOT NULL DEFAULT '',
	started_at TEXT NOT NULL,
	finished_at TEXT,
	error TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS credentials (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	kind TEXT NOT NULL UNIQUE,
	label TEXT NOT NULL DEFAULT '',
	secret TEXT NOT NULL DEFAULT '',
	meta TEXT NOT NULL DEFAULT '{}',
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS notifications (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	kind TEXT NOT NULL,
	title TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	app_id INTEGER,
	is_read INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS audit_logs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	actor TEXT NOT NULL DEFAULT 'admin',
	action TEXT NOT NULL,
	object_type TEXT NOT NULL DEFAULT '',
	object_id TEXT NOT NULL DEFAULT '',
	summary TEXT NOT NULL DEFAULT '',
	ip TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at DESC);
CREATE TABLE IF NOT EXISTS settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS metric_samples (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	scope TEXT NOT NULL,
	app_id INTEGER,
	cpu_pct REAL NOT NULL DEFAULT 0,
	mem_bytes INTEGER NOT NULL DEFAULT 0,
	disk_pct REAL NOT NULL DEFAULT 0,
	sampled_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_metric_scope_time ON metric_samples(scope, sampled_at);
CREATE TABLE IF NOT EXISTS query_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	database_id INTEGER NOT NULL REFERENCES databases(id) ON DELETE CASCADE,
	sql_text TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT '',
	duration_ms INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS db_backups (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	database_id INTEGER NOT NULL REFERENCES databases(id) ON DELETE CASCADE,
	file_path TEXT NOT NULL,
	size INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL
);
`

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func (p *Panel) openDB() error {
	dbPath := filepath.Join(p.cfg.DataDir, "panel.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(4)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return fmt.Errorf("migrasi basis data: %w", err)
	}
	p.db = db
	return nil
}

func (p *Panel) loadKey() error {
	path := filepath.Join(p.cfg.DataDir, "secret.key")
	b, err := os.ReadFile(path)
	if err == nil && len(b) >= 32 {
		p.key = b[:32]
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return fmt.Errorf("menulis kunci enkripsi: %w", err)
	}
	p.key = key
	return nil
}

func (p *Panel) encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return "enc:v1:" + base64.StdEncoding.EncodeToString(out), nil
}

func (p *Panel) decrypt(encoded string) string {
	if encoded == "" {
		return ""
	}
	if !strings.HasPrefix(encoded, "enc:v1:") {
		return encoded
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, "enc:v1:"))
	if err != nil {
		return ""
	}
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(raw) < gcm.NonceSize() {
		return ""
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return ""
	}
	return string(plain)
}

func (p *Panel) setting(key, def string) string {
	var v string
	err := p.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if err != nil || v == "" {
		return def
	}
	return v
}

func (p *Panel) setSetting(key, value string) error {
	_, err := p.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (p *Panel) intSetting(key string, def int) int {
	v, err := strconv.Atoi(p.setting(key, strconv.Itoa(def)))
	if err != nil {
		return def
	}
	return v
}

func (p *Panel) boolSetting(key string, def bool) bool {
	v := p.setting(key, strconv.FormatBool(def))
	return v == "true" || v == "1"
}

func (p *Panel) defaults() {
	def := map[string]string{
		"server_name":          hostnameOr("home-server"),
		"port_pool_start":      "3100",
		"port_pool_end":        "3999",
		"panel_port":           "8282",
		"release_keep":         "5",
		"cron_retention_days":  "30",
		"log_retention_days":   "14",
		"session_idle_minutes": "120",
		"alert_disk":           "90",
		"alert_ram":            "90",
		"alert_redis":          "85",
		"redis_enabled":        "false",
		"backup_enabled":       "false",
		"backup_schedule":      "0 2 * * *",
		"backup_retention_d":   "7",
		"backup_retention_w":   "4",
		"backup_redis":         "false",
		"cloudflared_config":   "/etc/cloudflared/config.yml",
		"cloudflared_service":  "cloudflared",
		"panel_hostname":       "panel.satriyadi.com",
		"base_url":             p.cfg.BaseURL,
		"notif_rules":          `{"deploy":true,"cron":true,"backup":true,"alert":true,"security":true,"cloudflare":true}`,
		"setup_done":           "false",
		"timezone":             systemTimezone(),
	}
	for k, v := range def {
		var exists int
		_ = p.db.QueryRow(`SELECT COUNT(*) FROM settings WHERE key=?`, k).Scan(&exists)
		if exists == 0 {
			_ = p.setSetting(k, v)
		}
	}
}

func hostnameOr(def string) string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return def
	}
	return h
}

func systemTimezone() string {
	if v := strings.TrimSpace(os.Getenv("TZ")); v != "" {
		return v
	}
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			return v
		}
	}
	return time.Local.String()
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func tokenHash(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "app"
	}
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (p *Panel) audit(action, objType, objID, summary string) {
	_, _ = p.db.Exec(`INSERT INTO audit_logs(actor,action,object_type,object_id,summary,created_at) VALUES('admin',?,?,?,?,?)`,
		action, objType, objID, summary, nowUTC())
}

func (p *Panel) notify(kind, title, body string, appID *int64) {
	res, err := p.db.Exec(`INSERT INTO notifications(kind,title,body,app_id,created_at) VALUES(?,?,?,?,?)`,
		kind, title, body, appID, nowUTC())
	if err != nil {
		return
	}
	id, _ := res.LastInsertId()
	p.sendOutbound(kind, title, body)
	p.publish("notification", map[string]any{"id": id, "kind": kind, "title": title, "body": body, "createdAt": nowUTC()})
}

func nullableID(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
