package panel

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Backup struct {
	ID              int64  `json:"id"`
	Trigger         string `json:"trigger"`
	Status          string `json:"status"`
	DriveFolder     string `json:"driveFolder"`
	DriveURL        string `json:"driveUrl"`
	TotalSize       int64  `json:"totalSize"`
	ManifestSummary string `json:"manifestSummary"`
	LocalPath       string `json:"localPath"`
	StartedAt       string `json:"startedAt"`
	FinishedAt      string `json:"finishedAt"`
	Error           string `json:"error"`
	ItemCount       int    `json:"itemCount"`
}

type backupItem struct {
	Path   string `json:"path"`
	Type   string `json:"type"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type backupManifest struct {
	Version   string       `json:"version"`
	Server    string       `json:"server"`
	CreatedAt string       `json:"createdAt"`
	Items     []backupItem `json:"items"`
}

func (p *Panel) runBackup(ctx context.Context, trigger string) (*Backup, error) {
	if !p.backupMu.TryLock() {
		return nil, fmt.Errorf("backup lain sedang berjalan")
	}
	defer p.backupMu.Unlock()

	started := time.Now().UTC()
	res, err := p.db.Exec(`INSERT INTO backups(trigger,status,started_at) VALUES(?,?,?)`, trigger, "berjalan", started.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	p.publish("backup.start", map[string]any{"id": id, "trigger": trigger})
	finish := func(b *Backup, err error) (*Backup, error) {
		status, msg := "berhasil", ""
		if err != nil {
			status, msg = "gagal", truncate(err.Error(), 800)
		}
		fin := nowUTC()
		b.FinishedAt = fin
		_, _ = p.db.Exec(`UPDATE backups SET status=?,finished_at=?,error=?,drive_folder=?,total_size=?,manifest_summary=?,local_path=? WHERE id=?`,
			status, fin, msg, b.DriveFolder, b.TotalSize, b.ManifestSummary, b.LocalPath, id)
		b.Status = status
		b.Error = msg
		if err != nil {
			p.notify("backup", "Backup gagal", msg, nil)
			p.audit("backup.failed", "backup", fmt.Sprint(id), "Backup "+trigger+" gagal: "+truncate(msg, 180))
			p.publish("backup.end", map[string]any{"id": id, "status": "gagal"})
		} else {
			p.notify("backup", "Backup berhasil", b.ManifestSummary+" · "+fmtBytes(b.TotalSize), nil)
			p.audit("backup.success", "backup", fmt.Sprint(id), "Backup "+trigger+" berhasil ("+fmtBytes(b.TotalSize)+")")
			p.publish("backup.end", map[string]any{"id": id, "status": "berhasil"})
		}
		return b, err
	}

	staging := filepath.Join(p.cfg.DataDir, "tmp", fmt.Sprintf("backup-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(staging, 0o750); err != nil {
		b := &Backup{ID: id, Trigger: trigger, StartedAt: started.Format(time.RFC3339)}
		return finish(b, err)
	}
	defer os.RemoveAll(staging)

	manifest := backupManifest{Version: p.version, Server: p.setting("server_name", "home-server"), CreatedAt: started.Format(time.RFC3339)}
	addItem := func(src, rel, typ string) error {
		dst := filepath.Join(staging, filepath.FromSlash(rel))
		if filepath.Clean(src) == filepath.Clean(dst) {
			f, err := os.Open(src)
			if err != nil {
				return err
			}
			h := sha256.New()
			n, err := io.Copy(h, f)
			f.Close()
			if err != nil {
				return err
			}
			manifest.Items = append(manifest.Items, backupItem{Path: rel, Type: typ, SHA256: hex.EncodeToString(h.Sum(nil)), Size: n})
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return err
		}
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(out, h), in)
		out.Close()
		if err != nil {
			return err
		}
		manifest.Items = append(manifest.Items, backupItem{Path: rel, Type: typ, SHA256: hex.EncodeToString(h.Sum(nil)), Size: n})
		return nil
	}

	// panel.db konsisten via VACUUM INTO
	panelCopy := filepath.Join(staging, "panel", "panel.db")
	_ = os.MkdirAll(filepath.Dir(panelCopy), 0o750)
	if _, err := p.db.Exec(`VACUUM INTO ` + sqlLiteral(panelCopy)); err != nil {
		b := &Backup{ID: id, Trigger: trigger, StartedAt: started.Format(time.RFC3339)}
		return finish(b, fmt.Errorf("salinan panel.db: %w", err))
	}
	if err := addItem(panelCopy, "panel/panel.db", "panel-db"); err != nil {
		b := &Backup{ID: id, Trigger: trigger, StartedAt: started.Format(time.RFC3339)}
		return finish(b, err)
	}
	if _, err := os.Stat(filepath.Join(p.cfg.DataDir, "secret.key")); err == nil {
		_ = addItem(filepath.Join(p.cfg.DataDir, "secret.key"), "panel/secret.key", "encryption-key")
	}

	// konfigurasi cloudflared
	cfPath := p.setting("cloudflared_config", "/etc/cloudflared/config.yml")
	if _, err := os.Stat(cfPath); err == nil {
		_ = addItem(cfPath, "cloudflared/config.yml", "cloudflared")
	}

	// aplikasi: metadata, env, dan folder persistent
	apps, _ := p.listApps()
	for _, app := range apps {
		meta := map[string]any{"app": app, "env": p.rawEnv(app.ID)}
		b, _ := json.MarshalIndent(meta, "", "  ")
		tmp := filepath.Join(staging, "apps", app.Slug, "metadata.json")
		_ = os.MkdirAll(filepath.Dir(tmp), 0o750)
		_ = os.WriteFile(tmp, b, 0o600)
		if err := addItem(tmp, filepath.ToSlash(filepath.Join("apps", app.Slug, "metadata.json")), "app-metadata"); err != nil {
			p.logf("backup metadata %s: %v", app.Slug, err)
		}
		for _, d := range app.PersistentDirs {
			src := filepath.Join(app.Workdir, "persistent", filepath.FromSlash(d))
			if info, err := os.Stat(src); err == nil {
				if info.IsDir() {
					_ = filepath.Walk(src, func(path string, fi os.FileInfo, err error) error {
						if err != nil || fi.IsDir() {
							return nil
						}
						rel, _ := filepath.Rel(app.Workdir, path)
						return addItem(path, filepath.ToSlash(filepath.Join("apps", app.Slug, rel)), "app-data")
					})
				}
			}
		}
	}

	// database terkelola
	dbs := p.listDatabases()
	for _, d := range dbs {
		full, password, err := p.getDatabase(d.ID)
		if err != nil {
			continue
		}
		path, err := p.backupDatabase(full, password)
		if err != nil {
			p.logf("backup database %s: %v", d.Name, err)
			continue
		}
		ext := filepath.Ext(path)
		if err := addItem(path, filepath.ToSlash(filepath.Join("databases", slugify(d.Name)+ext)), "database-"+d.Engine); err != nil {
			p.logf("backup database item %s: %v", d.Name, err)
		}
	}

	// Redis opsional
	if p.boolSetting("backup_redis", false) {
		ctx2, cancel := context.WithTimeout(ctx, 15*time.Second)
		c := p.redisAdminClient()
		if err := c.Do(ctx2, "BGSAVE").Err(); err == nil {
			for i := 0; i < 15; i++ {
				time.Sleep(time.Second)
				dirMap, err := c.ConfigGet(ctx2, "dir").Result()
				if err == nil {
					fileMap, _ := c.ConfigGet(ctx2, "dbfilename").Result()
					rdb := filepath.Join(dirMap["dir"], fileMap["dbfilename"])
					if _, err := os.Stat(rdb); err == nil {
						_ = addItem(rdb, "redis/dump.rdb", "redis")
						break
					}
				}
			}
		}
		cancel()
		c.Close()
	}

	mb, _ := json.MarshalIndent(manifest, "", "  ")
	_ = os.WriteFile(filepath.Join(staging, "manifest.json"), mb, 0o600)
	_ = os.WriteFile(filepath.Join(staging, "README.txt"), []byte("Backup SatriaPanel\nServer: "+manifest.Server+"\nDibuat: "+manifest.CreatedAt+"\n\nJangan ubah isi arsip. Simpan secret.key bersama panel.db.\n"), 0o600)

	zipPath := filepath.Join(p.backupDir(), fmt.Sprintf("satriapanel-%s-%s.zip", slugify(manifest.Server), started.Format("20060102-150405")))
	if err := zipDir(staging, zipPath); err != nil {
		return finish(&Backup{ID: id, Trigger: trigger, StartedAt: started.Format(time.RFC3339)}, err)
	}
	info, _ := os.Stat(zipPath)
	var total int64
	if info != nil {
		total = info.Size()
	}
	summary := fmt.Sprintf("%d item · %d aplikasi · %d database", len(manifest.Items), len(apps), len(dbs))
	b := &Backup{ID: id, Trigger: trigger, StartedAt: started.Format(time.RFC3339), TotalSize: total, ManifestSummary: summary, LocalPath: zipPath, ItemCount: len(manifest.Items), Status: "berjalan"}

	driveFolder, driveURL, driveErr := p.uploadDriveBackup(zipPath, manifest.Server, started)
	if driveFolder != "" {
		b.DriveFolder = driveFolder
		b.DriveURL = driveURL
	}
	if p.driveConfigured() {
		if driveErr != nil {
			p.logf("unggah Drive: %v", driveErr)
			b.DriveFolder = "gagal: " + truncate(driveErr.Error(), 150)
		}
	}
	go p.cleanupLocalBackups()
	return finish(b, nil)
}

func (p *Panel) rawEnv(appID int64) []map[string]any {
	rows, err := p.db.Query(`SELECT key,value,is_secret FROM app_env WHERE app_id=? ORDER BY key`, appID)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var k, v string
		var s int
		if rows.Scan(&k, &v, &s) == nil {
			out = append(out, map[string]any{"key": k, "value": v, "isSecret": s == 1})
		}
	}
	return out
}

func zipDir(root, target string) error {
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	_ = os.Chmod(target, 0o600)
	zw := zip.NewWriter(out)
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		h, _ := zip.FileInfoHeader(info)
		h.Name = filepath.ToSlash(rel)
		h.Method = zip.Deflate
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(target)
	}
	return err
}

func (p *Panel) cleanupLocalBackups() {
	entries, _ := os.ReadDir(p.backupDir())
	files := []string{}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".zip") {
			files = append(files, filepath.Join(p.backupDir(), e.Name()))
		}
	}
	sort.Strings(files)
	keep := 20
	if len(files) > keep {
		for _, f := range files[:len(files)-keep] {
			_ = os.Remove(f)
		}
	}
}

func (p *Panel) listBackups() []Backup {
	rows, err := p.db.Query(`SELECT id,trigger,status,drive_folder,total_size,manifest_summary,local_path,started_at,COALESCE(finished_at,''),error FROM backups ORDER BY id DESC LIMIT 100`)
	if err != nil {
		return []Backup{}
	}
	defer rows.Close()
	out := []Backup{}
	for rows.Next() {
		var b Backup
		if rows.Scan(&b.ID, &b.Trigger, &b.Status, &b.DriveFolder, &b.TotalSize, &b.ManifestSummary, &b.LocalPath, &b.StartedAt, &b.FinishedAt, &b.Error) == nil {
			if strings.Count(b.ManifestSummary, "item") > 0 {
				fmt.Sscanf(b.ManifestSummary, "%d", &b.ItemCount)
			}
			out = append(out, b)
		}
	}
	return out
}

func (p *Panel) getBackup(id int64) (*Backup, error) {
	for _, b := range p.listBackups() {
		if b.ID == id {
			return &b, nil
		}
	}
	return nil, errNotFound
}

func (p *Panel) handleBackups(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{
			"backups": p.listBackups(),
			"config": map[string]any{
				"enabled": p.boolSetting("backup_enabled", false), "schedule": p.setting("backup_schedule", "0 2 * * *"),
				"retentionDaily": p.intSetting("backup_retention_d", 7), "retentionWeekly": p.intSetting("backup_retention_w", 4),
				"includeRedis": p.boolSetting("backup_redis", false), "drive": p.driveStatus(),
			},
		})
		return
	}
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
		return
	}
	b, err := p.runBackup(r.Context(), "manual")
	if err != nil {
		failDetail(w, http.StatusInternalServerError, "Backup gagal", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"backup": b})
}

func (p *Panel) handleBackup(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	b, err := p.getBackup(id)
	if err != nil {
		httpError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"backup": b, "exists": fileExists(b.LocalPath)})
	case http.MethodDelete:
		if b.LocalPath != "" && strings.HasPrefix(filepath.Clean(b.LocalPath), filepath.Clean(p.backupDir())) {
			_ = os.Remove(b.LocalPath)
		}
		_, _ = p.db.Exec(`DELETE FROM backups WHERE id=?`, id)
		p.audit("backup.delete", "backup", fmt.Sprint(id), "Riwayat backup #"+fmt.Sprint(id)+" dihapus")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		fail(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan")
	}
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func (p *Panel) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	b, err := p.getBackup(id)
	if err != nil || b.LocalPath == "" {
		fail(w, http.StatusNotFound, "Backup tidak ditemukan")
		return
	}
	if !strings.HasPrefix(filepath.Clean(b.LocalPath), filepath.Clean(p.backupDir())) {
		fail(w, http.StatusForbidden, "Path backup tidak sah")
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(b.LocalPath)))
	http.ServeFile(w, r, b.LocalPath)
}

func (p *Panel) handleBackupContents(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	b, err := p.getBackup(id)
	if err != nil {
		httpError(w, err)
		return
	}
	zr, err := zip.OpenReader(b.LocalPath)
	if err != nil {
		failDetail(w, http.StatusBadRequest, "Arsip tidak dapat dibaca", err.Error())
		return
	}
	defer zr.Close()
	items := []map[string]any{}
	var manifest backupManifest
	for _, f := range zr.File {
		items = append(items, map[string]any{"name": f.Name, "size": f.UncompressedSize64})
		if f.Name == "manifest.json" {
			rc, err := f.Open()
			if err == nil {
				_ = json.NewDecoder(rc).Decode(&manifest)
				rc.Close()
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "manifest": manifest, "backup": b})
}

func (p *Panel) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	b, err := p.getBackup(id)
	if err != nil {
		httpError(w, err)
		return
	}
	var in struct {
		Confirm string `json:"confirm"`
		Scope   string `json:"scope"`
		AppID   int64  `json:"appId"`
		DBID    int64  `json:"databaseId"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Confirm != "PULIHKAN" {
		fail(w, http.StatusBadRequest, "Ketik PULIHKAN untuk konfirmasi restore")
		return
	}
	zr, err := zip.OpenReader(b.LocalPath)
	if err != nil {
		failDetail(w, http.StatusBadRequest, "Arsip tidak dapat dibuka", err.Error())
		return
	}
	defer zr.Close()
	var manifest backupManifest
	for _, f := range zr.File {
		if f.Name == "manifest.json" {
			rc, _ := f.Open()
			_ = json.NewDecoder(rc).Decode(&manifest)
			rc.Close()
			break
		}
	}
	hashes := map[string]string{}
	for _, it := range manifest.Items {
		hashes[it.Path] = it.SHA256
	}
	verified := 0
	extract := func(prefix, destRoot string) error {
		for _, f := range zr.File {
			if f.FileInfo().IsDir() || !strings.HasPrefix(f.Name, prefix) {
				continue
			}
			if want, ok := hashes[f.Name]; ok {
				rc, err := f.Open()
				if err != nil {
					return err
				}
				h := sha256.New()
				data, err := io.ReadAll(io.TeeReader(rc, h))
				rc.Close()
				if err != nil {
					return err
				}
				if hex.EncodeToString(h.Sum(nil)) != want {
					return fmt.Errorf("checksum tidak cocok untuk %s", f.Name)
				}
				verified++
				rel := strings.TrimPrefix(f.Name, prefix)
				if f.Name == prefix {
					rel = filepath.Base(prefix)
				}
				dest := filepath.Join(destRoot, filepath.FromSlash(rel))
				rootClean := filepath.Clean(destRoot)
				if filepath.Clean(dest) != rootClean && !strings.HasPrefix(filepath.Clean(dest), rootClean+string(os.PathSeparator)) {
					return fmt.Errorf("path tidak aman di arsip")
				}
				_ = os.MkdirAll(filepath.Dir(dest), 0o750)
				if err := os.WriteFile(dest, data, 0o600); err != nil {
					return err
				}
			}
		}
		return nil
	}
	result := map[string]any{}
	switch true {
	case strings.HasPrefix(in.Scope, "app:"):
		appID, _ := strconvParse(strings.TrimPrefix(in.Scope, "app:"))
		app, err := p.getApp(appID)
		if err != nil {
			fail(w, http.StatusBadRequest, "Aplikasi tidak ditemukan")
			return
		}
		dest := filepath.Join(app.Workdir, "persistent")
		if err := extract("apps/"+app.Slug+"/persistent/", dest); err != nil {
			failDetail(w, http.StatusBadRequest, "Restore aplikasi gagal", err.Error())
			return
		}
		result["message"] = fmt.Sprintf("Data persistent aplikasi %s dipulihkan (%d berkas diverifikasi).", app.Name, verified)
	case in.Scope == "panel":
		dest := filepath.Join(p.cfg.DataDir, "restore")
		if err := extract("panel/", dest); err != nil {
			failDetail(w, http.StatusBadRequest, "Restore panel gagal", err.Error())
			return
		}
		result["message"] = fmt.Sprintf("panel.db dan secret.key diverifikasi di %s (%d berkas). Hentikan panel, ganti panel.db, lalu jalankan kembali.", dest, verified)
	case strings.HasPrefix(in.Scope, "database:"):
		dbID, _ := strconvParse(strings.TrimPrefix(in.Scope, "database:"))
		db, password, err := p.getDatabase(dbID)
		if err != nil {
			fail(w, http.StatusBadRequest, "Database tidak ditemukan")
			return
		}
		if db.Engine != "sqlite" {
			fail(w, http.StatusBadRequest, "Restore database server gunakan tombol restore pada backup database di editor")
			return
		}
		dest := filepath.Join(p.cfg.DataDir, "restore")
		if err := extract("databases/"+slugify(db.Name)+".db", dest); err != nil {
			failDetail(w, http.StatusBadRequest, "Restore database gagal", err.Error())
			return
		}
		restored := filepath.Join(dest, slugify(db.Name)+".db")
		if _, err := p.backupDatabase(db, password); err != nil {
			p.logf("safety backup: %v", err)
		}
		in2, err := os.Open(restored)
		if err != nil {
			fail(w, http.StatusInternalServerError, "Berkas restore tidak ditemukan")
			return
		}
		out, err := os.OpenFile(db.FilePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
		if err == nil {
			_, err = io.Copy(out, in2)
			out.Close()
		}
		in2.Close()
		if err != nil {
			failDetail(w, http.StatusInternalServerError, "Gagal menyalin database", err.Error())
			return
		}
		result["message"] = fmt.Sprintf("Database SQLite %s dipulihkan (%d berkas diverifikasi).", db.Name, verified)
	default:
		fail(w, http.StatusBadRequest, "Scope restore tidak dikenal")
		return
	}
	p.audit("backup.restore", "backup", fmt.Sprint(id), "Restore dari backup #"+fmt.Sprint(id)+" scope "+in.Scope)
	result["ok"] = true
	result["verified"] = verified
	writeJSON(w, http.StatusOK, result)
}

func strconvParse(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscan(strings.TrimSpace(s), &n)
	return n, err
}

func (p *Panel) handleBackupConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": p.boolSetting("backup_enabled", false), "schedule": p.setting("backup_schedule", "0 2 * * *"),
			"retentionDaily": p.intSetting("backup_retention_d", 7), "retentionWeekly": p.intSetting("backup_retention_w", 4),
			"includeRedis": p.boolSetting("backup_redis", false), "drive": p.driveStatus(),
		})
		return
	}
	var in struct {
		Enabled         bool   `json:"enabled"`
		Schedule        string `json:"schedule"`
		RetentionDaily  int    `json:"retentionDaily"`
		RetentionWeekly int    `json:"retentionWeekly"`
		IncludeRedis    bool   `json:"includeRedis"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if _, err := cronParse(in.Schedule); err != nil {
		fail(w, http.StatusBadRequest, "Jadwal backup tidak valid")
		return
	}
	_ = p.setSetting("backup_enabled", fmt.Sprint(in.Enabled))
	_ = p.setSetting("backup_schedule", in.Schedule)
	_ = p.setSetting("backup_retention_d", fmt.Sprint(maxInt(in.RetentionDaily, 1)))
	_ = p.setSetting("backup_retention_w", fmt.Sprint(maxInt(in.RetentionWeekly, 0)))
	_ = p.setSetting("backup_redis", fmt.Sprint(in.IncludeRedis))
	_, _ = p.db.Exec(`UPDATE cron_jobs SET schedule_expr=? WHERE builtin_key='backup'`, in.Schedule)
	if p.sched != nil {
		p.sched.reload()
	}
	p.audit("backup.config", "backup", "", "Konfigurasi backup diubah")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
