package panel

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

type Database struct {
	ID             int64  `json:"id"`
	Engine         string `json:"engine"`
	Name           string `json:"name"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	HasPassword    bool   `json:"hasPassword"`
	FilePath       string `json:"filePath"`
	ManagedByPanel bool   `json:"managedByPanel"`
	AppID          *int64 `json:"appId"`
	AppName        string `json:"appName"`
	CreatedAt      string `json:"createdAt"`
	Status         string `json:"status"`
	Size           int64  `json:"size"`
}

type DBColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	PK       bool   `json:"pk"`
	Default  string `json:"default"`
}

type DBTable struct {
	Name    string     `json:"name"`
	Schema  string     `json:"schema"`
	Type    string     `json:"type"`
	Columns []DBColumn `json:"columns"`
	Rows    int64      `json:"rows"`
}

func (p *Panel) dbPathDir() string { return filepath.Join(p.cfg.DataDir, "databases") }

func (p *Panel) listDatabases() []Database {
	rows, err := p.db.Query(`SELECT d.id,d.engine,d.name,d.host,d.port,d.username,d.password_enc<>'',d.file_path,d.managed_by_panel,d.app_id,
		COALESCE(a.name,''),d.created_at FROM databases d LEFT JOIN apps a ON a.id=d.app_id ORDER BY d.name`)
	if err != nil {
		return []Database{}
	}
	defer rows.Close()
	out := []Database{}
	for rows.Next() {
		var d Database
		var appID sql.NullInt64
		var hasPass int
		if rows.Scan(&d.ID, &d.Engine, &d.Name, &d.Host, &d.Port, &d.Username, &hasPass, &d.FilePath, &d.ManagedByPanel, &appID, &d.AppName, &d.CreatedAt) != nil {
			continue
		}
		d.HasPassword = hasPass == 1
		if appID.Valid {
			v := appID.Int64
			d.AppID = &v
		}
		if d.Engine == "sqlite" && d.FilePath != "" {
			if info, err := os.Stat(d.FilePath); err == nil {
				d.Size = info.Size()
			}
		}
		d.Status = "unknown"
		out = append(out, d)
	}
	return out
}

func (p *Panel) getDatabase(id int64) (*Database, string, error) {
	var d Database
	var appID sql.NullInt64
	var enc string
	var hasPass int
	err := p.db.QueryRow(`SELECT d.id,d.engine,d.name,d.host,d.port,d.username,d.password_enc,d.password_enc<>'',d.file_path,d.managed_by_panel,d.app_id,
		COALESCE(a.name,''),d.created_at FROM databases d LEFT JOIN apps a ON a.id=d.app_id WHERE d.id=?`, id).
		Scan(&d.ID, &d.Engine, &d.Name, &d.Host, &d.Port, &d.Username, &enc, &hasPass, &d.FilePath, &d.ManagedByPanel, &appID, &d.AppName, &d.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, "", errNotFound
		}
		return nil, "", err
	}
	d.HasPassword = hasPass == 1
	if appID.Valid {
		v := appID.Int64
		d.AppID = &v
	}
	if d.Engine == "sqlite" && d.FilePath != "" {
		if info, err := os.Stat(d.FilePath); err == nil {
			d.Size = info.Size()
		}
	}
	return &d, p.decrypt(enc), nil
}

func dbDSN(d *Database, password string) (driver, dsn string, err error) {
	switch d.Engine {
	case "sqlite":
		if d.FilePath == "" {
			return "", "", fmt.Errorf("berkas SQLite belum diisi")
		}
		return "sqlite", d.FilePath + "?_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)", nil
	case "postgresql":
		host := defaultStr(d.Host, "127.0.0.1")
		port := d.Port
		if port == 0 {
			port = 5432
		}
		dsn = fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable&connect_timeout=5",
			url.QueryEscape(d.Username), url.QueryEscape(password), host, port, url.PathEscape(d.Name))
		return "postgres", dsn, nil
	case "mysql":
		host := defaultStr(d.Host, "127.0.0.1")
		port := d.Port
		if port == 0 {
			port = 3306
		}
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&timeout=5s&charset=utf8mb4",
			d.Username, password, host, port, d.Name)
		return "mysql", dsn, nil
	}
	return "", "", fmt.Errorf("engine tidak didukung")
}

func openDatabase(d *Database, password string) (*sql.DB, string, error) {
	driver, dsn, err := dbDSN(d, password)
	if err != nil {
		return nil, "", err
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, "", err
	}
	db.SetMaxOpenConns(3)
	db.SetConnMaxLifetime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, driver, err
	}
	return db, driver, nil
}

func (p *Panel) databaseStatus() map[string]any {
	out := map[string]any{"postgresql": "unknown", "mysql": "unknown", "sqlite": "tersedia"}
	if runtime.GOOS == "linux" {
		for _, svc := range []struct{ key, name string }{{"postgresql", "postgresql"}, {"mysql", "mysql"}} {
			if o, err := runCmdOutput(context.Background(), "systemctl is-active "+svc.name, 4*time.Second); err == nil || strings.TrimSpace(o) != "" {
				out[svc.key] = strings.TrimSpace(o)
			}
		}
	} else {
		if _, err := runCmdOutput(context.Background(), "psql --version", 4*time.Second); err == nil {
			out["postgresql"] = "klien tersedia"
		}
		if _, err := runCmdOutput(context.Background(), "mysql --version", 4*time.Second); err == nil {
			out["mysql"] = "klien tersedia"
		}
	}
	return out
}

func (p *Panel) handleDatabases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		dbs := p.listDatabases()
		apps, _ := p.listApps()
		writeJSON(w, http.StatusOK, map[string]any{
			"databases": dbs, "apps": apps, "engines": []string{"postgresql", "mysql", "sqlite"},
			"server": p.databaseStatus(), "sqliteDir": p.dbPathDir(),
		})
	case http.MethodPost:
		var in struct {
			Engine         string `json:"engine"`
			Name           string `json:"name"`
			Host           string `json:"host"`
			Port           int    `json:"port"`
			Username       string `json:"username"`
			Password       string `json:"password"`
			FilePath       string `json:"filePath"`
			ManagedByPanel bool   `json:"managedByPanel"`
			AppID          int64  `json:"appId"`
			CreateDatabase bool   `json:"createDatabase"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		in.Engine = strings.ToLower(strings.TrimSpace(in.Engine))
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" {
			fail(w, http.StatusBadRequest, "Nama database wajib diisi")
			return
		}
		if in.Engine != "postgresql" && in.Engine != "mysql" && in.Engine != "sqlite" {
			fail(w, http.StatusBadRequest, "Engine harus PostgreSQL, MySQL, atau SQLite")
			return
		}
		if in.AppID > 0 {
			var n int
			_ = p.db.QueryRow(`SELECT COUNT(*) FROM databases WHERE app_id=?`, in.AppID).Scan(&n)
			if n > 0 {
				fail(w, http.StatusBadRequest, "Aplikasi ini sudah terhubung ke database lain")
				return
			}
		}
		if in.Engine == "sqlite" {
			_ = os.MkdirAll(p.dbPathDir(), 0o750)
			path := strings.TrimSpace(in.FilePath)
			if path == "" {
				path = filepath.Join(p.dbPathDir(), slugify(in.Name)+".db")
			} else if !filepath.IsAbs(path) {
				path = filepath.Join(p.dbPathDir(), filepath.Base(path))
			}
			f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o640)
			if err != nil {
				failDetail(w, http.StatusBadRequest, "Gagal membuat berkas SQLite", err.Error())
				return
			}
			f.Close()
			in.FilePath = path
		} else {
			if in.CreateDatabase {
				if !p.boolSetting("db_allow_provision", false) {
					fail(w, http.StatusBadRequest, "Provisioning database server dimatikan. Aktifkan izin di Pengaturan atau hubungkan database yang sudah ada.")
					return
				}
				if err := p.provisionDatabase(in.Engine, in.Name, in.Username, in.Password); err != nil {
					failDetail(w, http.StatusBadRequest, "Provisioning gagal", err.Error())
					return
				}
			}
			in.Host = defaultStr(in.Host, "127.0.0.1")
			if in.Port == 0 {
				if in.Engine == "postgresql" {
					in.Port = 5432
				} else {
					in.Port = 3306
				}
			}
			if in.Username == "" {
				fail(w, http.StatusBadRequest, "Username database wajib diisi")
				return
			}
		}
		enc, _ := p.encrypt(in.Password)
		res, err := p.db.Exec(`INSERT INTO databases(engine,name,host,port,username,password_enc,file_path,managed_by_panel,app_id,created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?)`, in.Engine, in.Name, in.Host, in.Port, in.Username, enc, in.FilePath, in.ManagedByPanel, nullableID(in.AppID), nowUTC())
		if err != nil {
			httpError(w, err)
			return
		}
		id, _ := res.LastInsertId()
		p.audit("database.create", "database", fmt.Sprint(id), "Database "+in.Name+" ("+in.Engine+") ditambahkan")
		d, _, _ := p.getDatabase(id)
		if d != nil && d.AppID != nil {
			_, _ = p.db.Exec(`UPDATE apps SET needs_restart=1 WHERE id=?`, *d.AppID)
		}
		writeJSON(w, http.StatusCreated, map[string]any{"database": d})
	default:
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
	}
}

func (p *Panel) provisionDatabase(engine, name, user, pass string) error {
	if engine == "postgresql" {
		if user == "" || pass == "" {
			return fmt.Errorf("username dan password wajib untuk membuat database PostgreSQL")
		}
		sqlText := fmt.Sprintf(`DO $$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='%s') THEN CREATE ROLE "%s" LOGIN PASSWORD '%s'; ELSE ALTER ROLE "%s" PASSWORD '%s'; END IF; END $$;`,
			strings.ReplaceAll(user, `"`, `""`), strings.ReplaceAll(user, `"`, `""`), strings.ReplaceAll(pass, `'`, `''`), strings.ReplaceAll(user, `"`, `""`), strings.ReplaceAll(pass, `'`, `''`))
		if _, err := runCmdOutput(context.Background(), "sudo -n -u postgres psql -v ON_ERROR_STOP=1 -c "+shellQuote(sqlText), 30*time.Second); err != nil {
			return fmt.Errorf("jalankan PostgreSQL sebagai admin (sudo -u postgres) gagal: %w", err)
		}
		if _, err := runCmdOutput(context.Background(), "sudo -n -u postgres createdb -O "+shellQuote(user)+" "+shellQuote(name), 30*time.Second); err != nil {
			if _, err2 := runCmdOutput(context.Background(), "sudo -n -u postgres psql -c "+shellQuote(`CREATE DATABASE "`+strings.ReplaceAll(name, `"`, `""`)+`" OWNER "`+strings.ReplaceAll(user, `"`, `""`)+`"`), 30*time.Second); err2 != nil {
				return fmt.Errorf("membuat database PostgreSQL gagal: %v", err)
			}
		}
		return nil
	}
	if engine == "mysql" {
		if user == "" || pass == "" {
			return fmt.Errorf("username dan password wajib untuk membuat database MySQL")
		}
		q := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`; CREATE USER IF NOT EXISTS '%s'@'localhost' IDENTIFIED BY '%s'; GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost'; FLUSH PRIVILEGES;",
			strings.ReplaceAll(name, "`", "``"), strings.ReplaceAll(user, "'", "''"), strings.ReplaceAll(pass, "'", "''"),
			strings.ReplaceAll(name, "`", "``"), strings.ReplaceAll(user, "'", "''"))
		if _, err := runCmdOutput(context.Background(), "sudo -n mysql -e "+shellQuote(q), 30*time.Second); err != nil {
			return fmt.Errorf("membuat database MySQL gagal (butuh akses root): %w", err)
		}
		return nil
	}
	return nil
}

func (p *Panel) handleDatabase(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	d, password, err := p.getDatabase(id)
	if err != nil {
		httpError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		db, _, err := openDatabase(d, password)
		status := "online"
		detail := ""
		if err != nil {
			status = "offline"
			detail = err.Error()
		} else {
			db.Close()
		}
		writeJSON(w, http.StatusOK, map[string]any{"database": d, "status": status, "detail": detail, "backups": p.listDBBackups(id)})
	case http.MethodPut:
		var in struct {
			Name         string `json:"name"`
			Host         string `json:"host"`
			Port         int    `json:"port"`
			Username     string `json:"username"`
			Password     string `json:"password"`
			KeepPassword bool   `json:"keepPassword"`
			AppID        int64  `json:"appId"`
			FilePath     string `json:"filePath"`
		}
		if !decodeJSON(w, r, &in) {
			return
		}
		enc := d.FilePath // placeholder overwritten below
		_ = enc
		passEnc := ""
		if in.KeepPassword {
			_ = p.db.QueryRow(`SELECT password_enc FROM databases WHERE id=?`, id).Scan(&passEnc)
		} else if in.Password != "" {
			passEnc, _ = p.encrypt(in.Password)
		}
		filePath := d.FilePath
		if d.Engine == "sqlite" && in.FilePath != "" {
			filePath = in.FilePath
		}
		if _, err := p.db.Exec(`UPDATE databases SET name=?,host=?,port=?,username=?,password_enc=?,file_path=?,app_id=? WHERE id=?`,
			defaultStr(in.Name, d.Name), defaultStr(in.Host, d.Host), func() int {
				if in.Port > 0 {
					return in.Port
				}
				return d.Port
			}(), defaultStr(in.Username, d.Username), passEnc, filePath, nullableID(in.AppID), id); err != nil {
			httpError(w, err)
			return
		}
		p.audit("database.update", "database", fmt.Sprint(id), "Database "+d.Name+" diubah")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case http.MethodDelete:
		confirm := r.URL.Query().Get("confirm")
		if confirm != d.Name {
			fail(w, http.StatusBadRequest, "Ketik nama database untuk konfirmasi penghapusan")
			return
		}
		_, _ = p.db.Exec(`DELETE FROM databases WHERE id=?`, id)
		if r.URL.Query().Get("deleteFile") == "1" && d.Engine == "sqlite" && d.FilePath != "" && strings.HasPrefix(d.FilePath, p.dbPathDir()) {
			_ = os.Remove(d.FilePath)
		}
		p.audit("database.delete", "database", fmt.Sprint(id), "Database "+d.Name+" dihapus")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
	}
}

func (p *Panel) handleDatabaseTest(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	d, password, err := p.getDatabase(id)
	if err != nil {
		httpError(w, err)
		return
	}
	start := time.Now()
	db, driver, err := openDatabase(d, password)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "durationMs": time.Since(start).Milliseconds()})
		return
	}
	defer db.Close()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "driver": driver, "durationMs": time.Since(start).Milliseconds()})
}

func (p *Panel) handleDatabaseTables(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	d, password, err := p.getDatabase(id)
	if err != nil {
		httpError(w, err)
		return
	}
	db, _, err := openDatabase(d, password)
	if err != nil {
		failDetail(w, http.StatusBadGateway, "Tidak dapat terhubung", err.Error())
		return
	}
	defer db.Close()
	tables, err := p.introspect(db, d)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tables": tables, "engine": d.Engine})
}

func (p *Panel) introspect(db *sql.DB, d *Database) ([]DBTable, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out := []DBTable{}
	switch d.Engine {
	case "sqlite":
		rows, err := db.QueryContext(ctx, `SELECT name,type FROM sqlite_master WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%' ORDER BY name`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var t DBTable
			if rows.Scan(&t.Name, &t.Type) == nil {
				t.Schema = "main"
				t.Columns = p.sqliteColumns(ctx, db, t.Name)
				_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "`+strings.ReplaceAll(t.Name, `"`, `""`)+`"`).Scan(&t.Rows)
				out = append(out, t)
			}
		}
	case "postgresql":
		rows, err := db.QueryContext(ctx, `SELECT table_schema,table_name,table_type FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog','information_schema') ORDER BY table_schema,table_name`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var t DBTable
			var typ string
			if rows.Scan(&t.Schema, &t.Name, &typ) == nil {
				t.Type = "table"
				if strings.Contains(strings.ToLower(typ), "view") {
					t.Type = "view"
				}
				t.Columns = p.pgColumns(ctx, db, t.Schema, t.Name)
				out = append(out, t)
			}
		}
	case "mysql":
		rows, err := db.QueryContext(ctx, `SELECT table_schema,table_name,table_type FROM information_schema.tables WHERE table_schema NOT IN ('mysql','information_schema','performance_schema','sys') ORDER BY table_schema,table_name`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var t DBTable
			var typ, schema string
			if rows.Scan(&schema, &t.Name, &typ) == nil {
				t.Schema = schema
				t.Type = "table"
				if strings.Contains(strings.ToLower(typ), "view") {
					t.Type = "view"
				}
				t.Columns = p.mysqlColumns(ctx, db, schema, t.Name)
				out = append(out, t)
			}
		}
	}
	return out, nil
}

func (p *Panel) sqliteColumns(ctx context.Context, db *sql.DB, table string) []DBColumn {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info("`+strings.ReplaceAll(table, `"`, `""`)+`")`)
	if err != nil {
		return []DBColumn{}
	}
	defer rows.Close()
	cols := []DBColumn{}
	for rows.Next() {
		var cid int
		var c DBColumn
		var notnull, pk int
		var def sql.NullString
		if rows.Scan(&cid, &c.Name, &c.Type, &notnull, &def, &pk) == nil {
			c.Nullable = notnull == 0
			c.PK = pk > 0
			c.Default = def.String
			cols = append(cols, c)
		}
	}
	return cols
}

func (p *Panel) pgColumns(ctx context.Context, db *sql.DB, schema, table string) []DBColumn {
	rows, err := db.QueryContext(ctx, `SELECT c.column_name,c.data_type,c.is_nullable,COALESCE(c.column_default,''),
		CASE WHEN EXISTS (SELECT 1 FROM information_schema.table_constraints tc JOIN information_schema.key_column_usage kcu ON tc.constraint_name=kcu.constraint_name AND tc.table_schema=kcu.table_schema WHERE tc.constraint_type='PRIMARY KEY' AND tc.table_schema=c.table_schema AND tc.table_name=c.table_name AND kcu.column_name=c.column_name) THEN true ELSE false END
		FROM information_schema.columns c WHERE c.table_schema=$1 AND c.table_name=$2 ORDER BY c.ordinal_position`, schema, table)
	if err != nil {
		return []DBColumn{}
	}
	defer rows.Close()
	cols := []DBColumn{}
	for rows.Next() {
		var c DBColumn
		var nullable string
		if rows.Scan(&c.Name, &c.Type, &nullable, &c.Default, &c.PK) == nil {
			c.Nullable = nullable == "YES"
			cols = append(cols, c)
		}
	}
	return cols
}

func (p *Panel) mysqlColumns(ctx context.Context, db *sql.DB, schema, table string) []DBColumn {
	rows, err := db.QueryContext(ctx, `SELECT column_name,column_type,is_nullable,COALESCE(column_default,''),column_key='PRI' FROM information_schema.columns WHERE table_schema=? AND table_name=? ORDER BY ordinal_position`, schema, table)
	if err != nil {
		return []DBColumn{}
	}
	defer rows.Close()
	cols := []DBColumn{}
	for rows.Next() {
		var c DBColumn
		var nullable string
		if rows.Scan(&c.Name, &c.Type, &nullable, &c.Default, &c.PK) == nil {
			c.Nullable = nullable == "YES"
			cols = append(cols, c)
		}
	}
	return cols
}

func quoteIdent(engine, name string) string {
	parts := strings.Split(name, ".")
	for i, part := range parts {
		if engine == "mysql" {
			parts[i] = "`" + strings.ReplaceAll(part, "`", "``") + "`"
		} else {
			parts[i] = `"` + strings.ReplaceAll(part, `"`, `""`) + `"`
		}
	}
	return strings.Join(parts, ".")
}

func normalizeValue(v any) any {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case time.Time:
		return x.UTC().Format(time.RFC3339)
	default:
		return x
	}
}

func (p *Panel) queryDB(ctx context.Context, db *sql.DB, query string, args ...any) (*DBResult, error) {
	start := time.Now()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	result := &DBResult{Columns: cols, Rows: [][]any{}, DurationMs: time.Since(start).Milliseconds()}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		for i := range vals {
			vals[i] = normalizeValue(vals[i])
		}
		result.Rows = append(result.Rows, vals)
		if len(result.Rows) >= 5000 {
			result.Truncated = true
			break
		}
	}
	result.RowCount = len(result.Rows)
	result.DurationMs = time.Since(start).Milliseconds()
	return result, rows.Err()
}

type DBResult struct {
	Columns    []string `json:"columns"`
	Rows       [][]any  `json:"rows"`
	RowCount   int      `json:"rowCount"`
	DurationMs int64    `json:"durationMs"`
	Message    string   `json:"message"`
	Truncated  bool     `json:"truncated"`
	Affected   int64    `json:"affected"`
}

func (p *Panel) handleDatabaseQuery(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	d, password, err := p.getDatabase(id)
	if err != nil {
		httpError(w, err)
		return
	}
	var in struct {
		SQL     string `json:"sql"`
		Confirm string `json:"confirm"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	query := strings.TrimSpace(in.SQL)
	if query == "" {
		fail(w, http.StatusBadRequest, "SQL kosong")
		return
	}
	upper := strings.ToUpper(query)
	if (strings.HasPrefix(upper, "DELETE") || strings.HasPrefix(upper, "UPDATE")) && !strings.Contains(upper, " WHERE ") && in.Confirm != "HAPUS" {
		fail(w, http.StatusBadRequest, "Query DELETE/UPDATE tanpa WHERE berisiko. Ketik HAPUS untuk konfirmasi.")
		return
	}
	db, _, err := openDatabase(d, password)
	if err != nil {
		failDetail(w, http.StatusBadGateway, "Tidak dapat terhubung", err.Error())
		return
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	isSelect := strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH") || strings.HasPrefix(upper, "PRAGMA") || strings.HasPrefix(upper, "SHOW") || strings.HasPrefix(upper, "EXPLAIN") || strings.HasPrefix(upper, "DESCRIBE")
	start := time.Now()
	if isSelect {
		result, err := p.queryDB(ctx, db, query)
		if err != nil {
			_, _ = p.db.Exec(`INSERT INTO query_history(database_id,sql_text,status,duration_ms,created_at) VALUES(?,?,?,?,?)`, id, truncate(query, 2000), "gagal", time.Since(start).Milliseconds(), nowUTC())
			failDetail(w, http.StatusBadRequest, "Query gagal", err.Error())
			return
		}
		_, _ = p.db.Exec(`INSERT INTO query_history(database_id,sql_text,status,duration_ms,created_at) VALUES(?,?,?,?,?)`, id, truncate(query, 2000), "berhasil", result.DurationMs, nowUTC())
		writeJSON(w, http.StatusOK, map[string]any{"result": result})
		return
	}
	res, err := db.ExecContext(ctx, query)
	if err != nil {
		_, _ = p.db.Exec(`INSERT INTO query_history(database_id,sql_text,status,duration_ms,created_at) VALUES(?,?,?,?,?)`, id, truncate(query, 2000), "gagal", time.Since(start).Milliseconds(), nowUTC())
		failDetail(w, http.StatusBadRequest, "Query gagal", err.Error())
		return
	}
	affected, _ := res.RowsAffected()
	result := &DBResult{Columns: []string{}, Rows: [][]any{}, Affected: affected, DurationMs: time.Since(start).Milliseconds(), Message: fmt.Sprintf("%d baris terpengaruh", affected)}
	_, _ = p.db.Exec(`INSERT INTO query_history(database_id,sql_text,status,duration_ms,created_at) VALUES(?,?,?,?,?)`, id, truncate(query, 2000), "berhasil", result.DurationMs, nowUTC())
	p.audit("database.query", "database", fmt.Sprint(id), "Query non-SELECT dijalankan pada "+d.Name)
	writeJSON(w, http.StatusOK, map[string]any{"result": result})
}

func (p *Panel) handleDatabaseTable(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	d, password, err := p.getDatabase(id)
	if err != nil {
		httpError(w, err)
		return
	}
	table := r.URL.Query().Get("table")
	if table == "" {
		fail(w, http.StatusBadRequest, "Tabel wajib diisi")
		return
	}
	limit := qInt(r, "limit", 50)
	if limit < 1 || limit > 500 {
		limit = 50
	}
	offset := qInt(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	sortCol := r.URL.Query().Get("sort")
	dir := strings.ToUpper(r.URL.Query().Get("dir"))
	if dir != "DESC" {
		dir = "ASC"
	}
	filter := strings.TrimSpace(r.URL.Query().Get("filter"))
	db, _, err := openDatabase(d, password)
	if err != nil {
		failDetail(w, http.StatusBadGateway, "Tidak dapat terhubung", err.Error())
		return
	}
	defer db.Close()
	tables, err := p.introspect(db, d)
	if err != nil {
		httpError(w, err)
		return
	}
	var target *DBTable
	for i := range tables {
		if tables[i].Name == table || tables[i].Schema+"."+tables[i].Name == table {
			target = &tables[i]
			break
		}
	}
	if target == nil {
		fail(w, http.StatusNotFound, "Tabel tidak ditemukan")
		return
	}
	q := `SELECT * FROM ` + quoteIdent(d.Engine, table)
	args := []any{}
	if filter != "" {
		conds := []string{}
		for _, c := range target.Columns {
			ph := "?"
			if d.Engine == "postgresql" {
				ph = "$" + strconv.Itoa(len(args)+1)
			}
			args = append(args, "%"+filter+"%")
			cast := quoteIdent(d.Engine, c.Name)
			if d.Engine == "postgresql" {
				cast += "::text"
			}
			conds = append(conds, cast+" LIKE "+ph)
		}
		if len(conds) > 0 {
			q += " WHERE " + strings.Join(conds, " OR ")
		}
	}
	if sortCol != "" {
		valid := false
		for _, c := range target.Columns {
			if c.Name == sortCol {
				valid = true
				break
			}
		}
		if valid {
			q += " ORDER BY " + quoteIdent(d.Engine, sortCol) + " " + dir
		}
	}
	q += " LIMIT " + strconv.Itoa(limit) + " OFFSET " + strconv.Itoa(offset)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := p.queryDB(ctx, db, q, args...)
	if err != nil {
		failDetail(w, http.StatusBadRequest, "Gagal membaca tabel", err.Error())
		return
	}
	countQ := `SELECT COUNT(*) FROM ` + quoteIdent(d.Engine, table)
	total := target.Rows
	if filter == "" {
		_ = db.QueryRowContext(ctx, countQ).Scan(&total)
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": result, "table": target, "total": total, "limit": limit, "offset": offset, "engine": d.Engine})
}

func (p *Panel) handleDatabaseRows(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	d, password, err := p.getDatabase(id)
	if err != nil {
		httpError(w, err)
		return
	}
	var in struct {
		Table   string         `json:"table"`
		Values  map[string]any `json:"values"`
		PK      map[string]any `json:"pk"`
		Confirm string         `json:"confirm"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Table == "" || len(in.Values) == 0 {
		fail(w, http.StatusBadRequest, "Tabel dan nilai wajib diisi")
		return
	}
	db, _, err := openDatabase(d, password)
	if err != nil {
		failDetail(w, http.StatusBadGateway, "Tidak dapat terhubung", err.Error())
		return
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var query string
	args := []any{}
	switch r.Method {
	case http.MethodPost:
		cols := []string{}
		ph := []string{}
		for k, v := range in.Values {
			cols = append(cols, quoteIdent(d.Engine, k))
			if d.Engine == "postgresql" {
				ph = append(ph, "$"+strconv.Itoa(len(args)+1))
			} else {
				ph = append(ph, "?")
			}
			args = append(args, v)
		}
		query = "INSERT INTO " + quoteIdent(d.Engine, in.Table) + " (" + strings.Join(cols, ",") + ") VALUES (" + strings.Join(ph, ",") + ")"
	case http.MethodPut:
		if len(in.PK) == 0 {
			fail(w, http.StatusBadRequest, "Primary key wajib untuk update")
			return
		}
		sets := []string{}
		wheres := []string{}
		for k, v := range in.Values {
			if d.Engine == "postgresql" {
				sets = append(sets, quoteIdent(d.Engine, k)+"=$"+strconv.Itoa(len(args)+1))
			} else {
				sets = append(sets, quoteIdent(d.Engine, k)+"=?")
			}
			args = append(args, v)
		}
		for k, v := range in.PK {
			if d.Engine == "postgresql" {
				wheres = append(wheres, quoteIdent(d.Engine, k)+"=$"+strconv.Itoa(len(args)+1))
			} else {
				wheres = append(wheres, quoteIdent(d.Engine, k)+"=?")
			}
			args = append(args, v)
		}
		query = "UPDATE " + quoteIdent(d.Engine, in.Table) + " SET " + strings.Join(sets, ",") + " WHERE " + strings.Join(wheres, " AND ")
	case http.MethodDelete:
		if len(in.PK) == 0 {
			fail(w, http.StatusBadRequest, "Primary key wajib untuk menghapus baris")
			return
		}
		if in.Confirm != "HAPUS" {
			fail(w, http.StatusBadRequest, "Ketik HAPUS untuk menghapus baris")
			return
		}
		wheres := []string{}
		for k, v := range in.PK {
			if d.Engine == "postgresql" {
				wheres = append(wheres, quoteIdent(d.Engine, k)+"=$"+strconv.Itoa(len(args)+1))
			} else {
				wheres = append(wheres, quoteIdent(d.Engine, k)+"=?")
			}
			args = append(args, v)
		}
		query = "DELETE FROM " + quoteIdent(d.Engine, in.Table) + " WHERE " + strings.Join(wheres, " AND ")
	default:
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
		return
	}
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		failDetail(w, http.StatusBadRequest, "Operasi baris gagal", err.Error())
		return
	}
	n, _ := res.RowsAffected()
	p.audit("database.row_"+strings.ToLower(r.Method), "database", fmt.Sprint(id), fmt.Sprintf("Baris pada tabel %s diubah (%d baris)", in.Table, n))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "affected": n})
}

func (p *Panel) handleDatabaseImport(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	d, password, err := p.getDatabase(id)
	if err != nil {
		httpError(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 200<<20)
	file, header, err := r.FormFile("file")
	if err != nil {
		fail(w, http.StatusBadRequest, "Berkas tidak ditemukan")
		return
	}
	defer file.Close()
	table := r.FormValue("table")
	format := strings.ToLower(filepath.Ext(header.Filename))
	db, _, err := openDatabase(d, password)
	if err != nil {
		failDetail(w, http.StatusBadGateway, "Tidak dapat terhubung", err.Error())
		return
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	inserted := 0
	if format == ".csv" {
		if table == "" {
			fail(w, http.StatusBadRequest, "Nama tabel wajib untuk impor CSV")
			return
		}
		reader := csv.NewReader(file)
		reader.FieldsPerRecord = -1
		rows, err := reader.ReadAll()
		if err != nil || len(rows) < 2 {
			fail(w, http.StatusBadRequest, "CSV tidak valid atau kosong")
			return
		}
		headerRow := rows[0]
		tx, _ := db.BeginTx(ctx, nil)
		placeholders := []string{}
		for range headerRow {
			if d.Engine == "postgresql" {
				placeholders = append(placeholders, "$"+strconv.Itoa(len(placeholders)+1))
			} else {
				placeholders = append(placeholders, "?")
			}
		}
		stmt := "INSERT INTO " + quoteIdent(d.Engine, table) + " (" + joinQuoted(d.Engine, headerRow) + ") VALUES (" + strings.Join(placeholders, ",") + ")"
		for _, row := range rows[1:] {
			if len(row) != len(headerRow) {
				continue
			}
			args := make([]any, len(row))
			for i, v := range row {
				if v == "" {
					args[i] = nil
				} else {
					args[i] = v
				}
			}
			if _, err := tx.ExecContext(ctx, stmt, args...); err == nil {
				inserted++
			}
		}
		_ = tx.Commit()
	} else if format == ".sql" {
		b, _ := io.ReadAll(file)
		for _, stmt := range splitSQL(string(b)) {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				failDetail(w, http.StatusBadRequest, "SQL gagal di baris import", err.Error())
				return
			}
			inserted++
		}
	} else {
		fail(w, http.StatusBadRequest, "Format harus .csv atau .sql")
		return
	}
	p.audit("database.import", "database", fmt.Sprint(id), fmt.Sprintf("Impor %s ke %s (%d baris)", header.Filename, d.Name, inserted))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "inserted": inserted})
}

func joinQuoted(engine string, names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = quoteIdent(engine, n)
	}
	return strings.Join(out, ",")
}

func splitSQL(s string) []string {
	var out []string
	var b strings.Builder
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case ';':
			if !inSingle && !inDouble {
				out = append(out, b.String())
				b.Reset()
				continue
			}
		}
		b.WriteByte(ch)
	}
	if strings.TrimSpace(b.String()) != "" {
		out = append(out, b.String())
	}
	return out
}

func (p *Panel) handleDatabaseExport(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	d, password, err := p.getDatabase(id)
	if err != nil {
		httpError(w, err)
		return
	}
	table := r.URL.Query().Get("table")
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "" {
		format = "csv"
	}
	db, _, err := openDatabase(d, password)
	if err != nil {
		failDetail(w, http.StatusBadGateway, "Tidak dapat terhubung", err.Error())
		return
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	q := "SELECT * FROM " + quoteIdent(d.Engine, table)
	result, err := p.queryDB(ctx, db, q)
	if err != nil {
		failDetail(w, http.StatusBadRequest, "Ekspor gagal", err.Error())
		return
	}
	p.audit("database.export", "database", fmt.Sprint(id), "Ekspor tabel "+table+" ("+format+")")
	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", table+".json"))
		_ = json.NewEncoder(w).Encode(map[string]any{"table": table, "columns": result.Columns, "rows": result.Rows})
		return
	}
	if format == "sql" {
		w.Header().Set("Content-Type", "application/sql")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", table+".sql"))
		fmt.Fprintf(w, "-- Ekspor SatriaPanel %s\n", table)
		for _, row := range result.Rows {
			vals := make([]string, len(row))
			for i, v := range row {
				vals[i] = sqlLiteral(v)
			}
			fmt.Fprintf(w, "INSERT INTO %s (%s) VALUES (%s);\n", quoteIdent(d.Engine, table), joinQuoted(d.Engine, result.Columns), strings.Join(vals, ","))
		}
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", table+".csv"))
	cw := csv.NewWriter(w)
	_ = cw.Write(result.Columns)
	for _, row := range result.Rows {
		rec := make([]string, len(row))
		for i, v := range row {
			if v == nil {
				rec[i] = ""
			} else {
				rec[i] = fmt.Sprint(v)
			}
		}
		_ = cw.Write(rec)
	}
	cw.Flush()
}

func sqlLiteral(v any) string {
	if v == nil {
		return "NULL"
	}
	if n, ok := v.(float64); ok {
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	s := fmt.Sprint(v)
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func (p *Panel) handleDatabaseBackup(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	d, password, err := p.getDatabase(id)
	if err != nil {
		httpError(w, err)
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"backups": p.listDBBackups(id)})
		return
	}
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
		return
	}
	path, err := p.backupDatabase(d, password)
	if err != nil {
		failDetail(w, http.StatusBadRequest, "Backup database gagal", err.Error())
		return
	}
	info, _ := os.Stat(path)
	var size int64
	if info != nil {
		size = info.Size()
	}
	res, _ := p.db.Exec(`INSERT INTO db_backups(database_id,file_path,size,created_at) VALUES(?,?,?,?)`, id, path, size, nowUTC())
	bid, _ := res.LastInsertId()
	p.audit("database.backup", "database", fmt.Sprint(id), "Backup database "+d.Name+" dibuat")
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "id": bid, "path": path, "size": size})
}

func (p *Panel) backupDatabase(d *Database, password string) (string, error) {
	dir := filepath.Join(p.backupDir(), "db")
	_ = os.MkdirAll(dir, 0o750)
	name := slugify(d.Name) + "-" + time.Now().UTC().Format("20060102-150405")
	switch d.Engine {
	case "sqlite":
		target := filepath.Join(dir, name+".db")
		db, _, err := openDatabase(d, password)
		if err != nil {
			return "", err
		}
		defer db.Close()
		_, err = db.Exec(`VACUUM INTO ` + sqlLiteral(target))
		if err != nil {
			// fallback copy
			src, err2 := os.Open(d.FilePath)
			if err2 != nil {
				return "", err
			}
			defer src.Close()
			dst, err3 := os.Create(target)
			if err3 != nil {
				return "", err3
			}
			defer dst.Close()
			_, err3 = io.Copy(dst, src)
			if err3 != nil {
				return "", err3
			}
		}
		return target, nil
	case "postgresql":
		target := filepath.Join(dir, name+".sql")
		cmd := fmt.Sprintf("PGPASSWORD=%s pg_dump -h %s -p %d -U %s -d %s -f %s", shellQuote(password), shellQuote(d.Host), d.Port, shellQuote(d.Username), shellQuote(d.Name), shellQuote(target))
		out, err := runCmdOutput(context.Background(), cmd, 5*time.Minute)
		if err != nil {
			return "", fmt.Errorf("%v: %s", err, truncate(out, 300))
		}
		return target, nil
	case "mysql":
		target := filepath.Join(dir, name+".sql")
		cmd := fmt.Sprintf("MYSQL_PWD=%s mysqldump -h %s -P %d -u %s --single-transaction %s > %s", shellQuote(password), shellQuote(d.Host), d.Port, shellQuote(d.Username), shellQuote(d.Name), shellQuote(target))
		out, err := runCmdOutput(context.Background(), cmd, 5*time.Minute)
		if err != nil {
			return "", fmt.Errorf("%v: %s", err, truncate(out, 300))
		}
		return target, nil
	}
	return "", fmt.Errorf("engine tidak didukung")
}

func (p *Panel) listDBBackups(dbID int64) []map[string]any {
	rows, err := p.db.Query(`SELECT id,file_path,size,created_at FROM db_backups WHERE database_id=? ORDER BY id DESC LIMIT 30`, dbID)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, size int64
		var path, created string
		if rows.Scan(&id, &path, &size, &created) == nil {
			out = append(out, map[string]any{"id": id, "file": filepath.Base(path), "size": size, "createdAt": created})
		}
	}
	return out
}

func (p *Panel) handleDBBackupDownload(w http.ResponseWriter, r *http.Request) {
	bid := pathID(r)
	var path string
	var dbID int64
	if err := p.db.QueryRow(`SELECT file_path,database_id FROM db_backups WHERE id=?`, bid).Scan(&path, &dbID); err != nil {
		httpError(w, errNotFound)
		return
	}
	if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(p.backupDir())) {
		fail(w, http.StatusForbidden, "Path backup tidak sah")
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(path)))
	http.ServeFile(w, r, path)
}

func (p *Panel) handleDatabaseRestore(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	d, password, err := p.getDatabase(id)
	if err != nil {
		httpError(w, err)
		return
	}
	var in struct {
		BackupID int64  `json:"backupId"`
		Confirm  string `json:"confirm"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Confirm != d.Name {
		fail(w, http.StatusBadRequest, "Ketik nama database untuk konfirmasi restore")
		return
	}
	var path string
	if err := p.db.QueryRow(`SELECT file_path FROM db_backups WHERE id=? AND database_id=?`, in.BackupID, id).Scan(&path); err != nil {
		fail(w, http.StatusNotFound, "Backup tidak ditemukan")
		return
	}
	// safety backup dulu
	_, _ = p.backupDatabase(d, password)
	switch d.Engine {
	case "sqlite":
		src, err := os.Open(path)
		if err != nil {
			failDetail(w, http.StatusBadRequest, "Gagal membuka backup", err.Error())
			return
		}
		defer src.Close()
		dst, err := os.OpenFile(d.FilePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
		if err != nil {
			failDetail(w, http.StatusBadRequest, "Gagal menulis database", err.Error())
			return
		}
		_, err = io.Copy(dst, src)
		dst.Close()
		if err != nil {
			fail(w, http.StatusInternalServerError, "Restore gagal")
			return
		}
	case "postgresql":
		cmd := fmt.Sprintf("PGPASSWORD=%s psql -h %s -p %d -U %s -d %s -f %s", shellQuote(password), shellQuote(d.Host), d.Port, shellQuote(d.Username), shellQuote(d.Name), shellQuote(path))
		if out, err := runCmdOutput(context.Background(), cmd, 10*time.Minute); err != nil {
			failDetail(w, http.StatusBadRequest, "Restore PostgreSQL gagal", truncate(out, 400))
			return
		}
	case "mysql":
		cmd := fmt.Sprintf("MYSQL_PWD=%s mysql -h %s -P %d -u %s %s < %s", shellQuote(password), shellQuote(d.Host), d.Port, shellQuote(d.Username), shellQuote(d.Name), shellQuote(path))
		if out, err := runCmdOutput(context.Background(), cmd, 10*time.Minute); err != nil {
			failDetail(w, http.StatusBadRequest, "Restore MySQL gagal", truncate(out, 400))
			return
		}
	}
	p.audit("database.restore", "database", fmt.Sprint(id), "Database "+d.Name+" direstore dari backup #"+strconv.FormatInt(in.BackupID, 10))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (p *Panel) handleDatabaseHistory(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	rows, err := p.db.Query(`SELECT id,sql_text,status,duration_ms,created_at FROM query_history WHERE database_id=? ORDER BY id DESC LIMIT 50`, id)
	if err != nil {
		httpError(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var qid, dur int64
		var sqlText, status, created string
		if rows.Scan(&qid, &sqlText, &status, &dur, &created) == nil {
			out = append(out, map[string]any{"id": qid, "sql": sqlText, "status": status, "durationMs": dur, "createdAt": created})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": out})
}
